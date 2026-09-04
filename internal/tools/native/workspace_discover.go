package native

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Discovery operations for the native workspace adapter (issue #452): list_dir / glob / grep.
//
// On an unfamiliar repo an agent that can only read and write files has no way to ENUMERATE what
// exists, so "read the relevant code" collapses to "guess a path". These read-only ops let an agent
// find the files it is already allowed to read. Each resolves through the same os.Root sandbox as
// read_file — a `..`/symlink escape is refused at the OS level.
//
// Like every operation in this package these are concrete capabilities. The effect classes they
// produce (workspace.read) are declared on the Tool resource's operations manifest (see doc.go),
// NOT here, and enforced there: an author must both declare each op and grant it before an agent
// can call it. A tool that lists only read_file does not gain list_dir/glob/grep for free, and a
// closed-world program must add them to `operations` and `grants` to use them.
const (
	// maxWorkspaceGlobMatches / maxWorkspaceGrepMatches bound a discovery result so a broad pattern
	// over a large tree degrades to truncated=true rather than an unbounded result.
	maxWorkspaceGlobMatches   = 1000
	maxWorkspaceGrepMatches   = 500
	maxWorkspaceGrepFileBytes = maxWorkspaceReadBytes // per-file read cap for grep (1 MiB)
	// grepBinarySniffBytes is how far into a file we look for a NUL before treating it as binary
	// and skipping it (grep is a source-search tool, not a binary scanner).
	grepBinarySniffBytes = 8 << 10
	// maxGrepLineRunes truncates a single matching line in the result.
	maxGrepLineRunes = 240
)

// grepSkipDirs are directory names grep prunes by default: VCS metadata and vendored/dependency
// trees that are large and rarely the search target. A caller can still search one by scoping grep
// directly at it (the `path` arg), so pruning applies only below the scope root.
var grepSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// workspaceScopeRel normalizes an optional directory/scope arg to an fs.FS-valid, root-relative
// path. An empty or "." value means the sandbox root itself. A `..` escape is refused by os.Root
// when the path is later opened/walked, but reject an obviously-invalid fs path early for a clear
// error.
func workspaceScopeRel(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return ".", nil
	}
	rel := strings.TrimLeft(filepath.ToSlash(raw), "/")
	if rel == "" {
		return ".", nil
	}
	if !fs.ValidPath(rel) {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	return rel, nil
}

func dispatchWorkspaceListDir(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := openWorkspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()

	// path is optional: absent/"." lists the sandbox root.
	rawPath, _, err := optionalStringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: list_dir: %w", err)
	}
	rel, err := workspaceScopeRel(rawPath)
	if err != nil {
		return nil, meta, fmt.Errorf("native: list_dir: %w", err)
	}

	f, err := root.Open(rel)
	if err != nil {
		return nil, meta, fmt.Errorf("native: list_dir %q: %w", rel, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, meta, fmt.Errorf("native: list_dir %q: %w", rel, err)
	}
	if !info.IsDir() {
		return nil, meta, fmt.Errorf("native: list_dir %q: not a directory", rel)
	}
	names, truncated, err := readWorkspaceDirEntries(f)
	if err != nil {
		return nil, meta, fmt.Errorf("native: list_dir %q: %w", rel, err)
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	out := map[string]any{"path": rel, "entries": names}
	if truncated {
		out["truncated"] = true
	}
	return out, meta, nil
}

func dispatchWorkspaceGlob(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := openWorkspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()

	pattern, err := stringFromWith(with, "pattern")
	if err != nil {
		return nil, meta, fmt.Errorf("native: glob: %w", err)
	}
	pattern = strings.TrimLeft(filepath.ToSlash(pattern), "/")
	segs, err := compileGlob(pattern)
	if err != nil {
		// A malformed pattern is a bad-input error surfaced to the caller.
		return nil, meta, fmt.Errorf("native: glob %q: %w", pattern, err)
	}

	// Walk the sandbox FS and match root-relative paths ourselves so `**` is a real recursive
	// wildcard (io/fs.Glob is path.Match: `*` never crosses `/` and `**` is not globstar, which
	// silently returns a one-level subset for the `**/*.go` a coding agent will send).
	fsys := root.FS()
	matches := make([]string, 0, 16)
	truncated := false
	walkErr := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if p == "." {
			return nil
		}
		if globMatch(segs, strings.Split(p, "/")) {
			matches = append(matches, p)
			if len(matches) >= maxWorkspaceGlobMatches {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, meta, fmt.Errorf("native: glob %q: %w", pattern, walkErr)
	}
	sort.Strings(matches)
	meta.DurationMs = time.Since(start).Milliseconds()
	out := map[string]any{"pattern": pattern, "matches": matches}
	if truncated {
		out["truncated"] = true
	}
	return out, meta, nil
}

// compileGlob splits a slash-separated glob into segments and validates each. "**" is a segment
// that matches zero or more path segments; within any other segment `*`/`?`/`[…]` follow path.Match
// (they never cross `/`). A `..` component (which os.Root would refuse anyway) and a syntactically
// invalid segment are reported as a bad-input error up front, so a bad pattern fails the same way
// whether or not any file happens to reach it during the walk.
func compileGlob(pattern string) ([]string, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	segs := strings.Split(pattern, "/")
	for _, s := range segs {
		if s == ".." {
			return nil, fmt.Errorf("pattern escapes workspace root")
		}
		if s == "**" {
			continue
		}
		if _, err := path.Match(s, ""); err != nil {
			return nil, err
		}
	}
	return segs, nil
}

// globMatch reports whether the name segments match the compiled glob segments, treating "**" as a
// zero-or-more-segment wildcard.
func globMatch(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		// "**" consumes 0..len(name) leading segments; try each split.
		for i := 0; i <= len(name); i++ {
			if globMatch(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	// Segment already validated by compileGlob, so a bad-pattern error cannot arise here.
	if ok, _ := path.Match(pat[0], name[0]); !ok {
		return false
	}
	return globMatch(pat[1:], name[1:])
}

func dispatchWorkspaceGrep(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	root, err := openWorkspaceRoot(ctx)
	if err != nil {
		return nil, meta, err
	}
	defer root.Close()

	pattern, err := stringFromWith(with, "pattern")
	if err != nil {
		return nil, meta, fmt.Errorf("native: grep: %w", err)
	}
	// A malformed regexp is a bad-input error surfaced to the caller.
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, meta, fmt.Errorf("native: grep %q: %w", pattern, err)
	}
	rawScope, _, err := optionalStringFromWith(with, "path")
	if err != nil {
		return nil, meta, fmt.Errorf("native: grep: %w", err)
	}
	scope, err := workspaceScopeRel(rawScope)
	if err != nil {
		return nil, meta, fmt.Errorf("native: grep: %w", err)
	}

	fsys := root.FS()
	hits := make([]map[string]any, 0, 16)
	truncated := false
	walkErr := fs.WalkDir(fsys, scope, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Honor cancellation/timeout on every entry so a long search over a big tree actually
		// stops when the run's ctx is done (matching run_tests' CommandContext behavior).
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			// Prune VCS metadata and vendored/dependency trees (large, rarely the target) unless
			// the caller scoped the search directly at one, so grep stays a bounded code search.
			if p != scope && grepSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fileHits, fileTrunc, stop, ferr := grepFile(fsys, p, re, maxWorkspaceGrepMatches-len(hits))
		if ferr != nil {
			return nil // an unreadable file is skipped, not fatal for the whole search
		}
		hits = append(hits, fileHits...)
		if fileTrunc {
			// A file exceeded the per-file scan cap: a match past the cap may have been missed,
			// so flag the whole result as incomplete (as read_file does for an oversized file).
			truncated = true
		}
		if stop {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, meta, fmt.Errorf("native: grep %q under %q: %w", pattern, scope, walkErr)
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	out := map[string]any{"pattern": pattern, "path": scope, "matches": hits, "count": len(hits)}
	if truncated {
		out["truncated"] = true
	}
	return out, meta, nil
}

// grepFile scans one file for re, appending up to budget hits. truncated is true when the file was
// larger than the per-file scan cap (so a match past the cap may have been missed); stop is true
// when the budget was reached (the caller should stop walking). A binary file (NUL in the sniff
// window) is skipped.
func grepFile(fsys fs.FS, p string, re *regexp.Regexp, budget int) (hits []map[string]any, truncated, stop bool, err error) {
	if budget <= 0 {
		return nil, false, true, nil
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, false, false, err
	}
	defer f.Close()
	// Read one byte past the cap so an oversized file is reported (like read_file) rather than
	// silently yielding a clean miss for a match that lives just past the cap.
	data, err := io.ReadAll(io.LimitReader(f, maxWorkspaceGrepFileBytes+1))
	if err != nil {
		return nil, false, false, err
	}
	if len(data) > maxWorkspaceGrepFileBytes {
		data = data[:maxWorkspaceGrepFileBytes]
		truncated = true
	}
	sniff := data
	if len(sniff) > grepBinarySniffBytes {
		sniff = sniff[:grepBinarySniffBytes]
	}
	if bytesIndexNUL(sniff) >= 0 {
		return nil, false, false, nil // binary file — skip
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		hits = append(hits, map[string]any{"file": p, "line": i + 1, "text": truncateRunes(line, maxGrepLineRunes)})
		if len(hits) >= budget {
			return hits, truncated, true, nil
		}
	}
	return hits, truncated, false, nil
}

func bytesIndexNUL(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// optionalStringFromWith reads an optional string arg. Absent/nil returns ("", false, nil) — the
// caller uses its default. A present but non-scalar value (an array or object, a common JSON-schema
// slip) is a bad-input error, NOT a silent fall-through to the default, so a mistyped `path` does
// not quietly widen a scoped listing/search to the whole workspace.
func optionalStringFromWith(with map[string]any, key string) (string, bool, error) {
	v, ok := with[key]
	if !ok || v == nil {
		return "", false, nil
	}
	s, err := scalarToString(v)
	if err != nil {
		return "", false, fmt.Errorf("field %q: %w", key, err)
	}
	return strings.TrimSpace(s), true, nil
}
