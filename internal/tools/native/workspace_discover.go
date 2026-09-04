package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// find the files it is already allowed to read. They carry the same workspace.read effect and the
// same os.Root sandbox as read_file — a `..`/symlink escape is refused at the OS level — so they add
// no capability surface.
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
	rawPath, _ := optionalStringFromWith(with, "path")
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
	ents, rderr := f.ReadDir(maxWorkspaceDirEntries + 1)
	if rderr != nil && !errors.Is(rderr, io.EOF) {
		return nil, meta, fmt.Errorf("native: list_dir %q: %w", rel, rderr)
	}
	truncated := false
	if len(ents) > maxWorkspaceDirEntries {
		ents = ents[:maxWorkspaceDirEntries]
		truncated = true
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
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
	// fs.Glob resolves against the sandbox FS, so a match can never name a path outside the root.
	// A malformed pattern is a recoverable tool error (bad input), not a run fault.
	matches, err := fs.Glob(root.FS(), pattern)
	if err != nil {
		return nil, meta, fmt.Errorf("native: glob %q: %w", pattern, err)
	}
	truncated := false
	if len(matches) > maxWorkspaceGlobMatches {
		matches = matches[:maxWorkspaceGlobMatches]
		truncated = true
	}
	sort.Strings(matches)
	meta.DurationMs = time.Since(start).Milliseconds()
	out := map[string]any{"pattern": pattern, "matches": matches}
	if truncated {
		out["truncated"] = true
	}
	return out, meta, nil
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
	// A malformed regexp is a recoverable tool error (bad input), not a run fault.
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, meta, fmt.Errorf("native: grep %q: %w", pattern, err)
	}
	rawScope, _ := optionalStringFromWith(with, "path")
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
		if d.IsDir() {
			// .git is VCS metadata (large, never source); skip it so grep stays a code search.
			if d.Name() == ".git" && p != scope {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fileHits, stop, ferr := grepFile(fsys, p, re, maxWorkspaceGrepMatches-len(hits))
		if ferr != nil {
			return nil // an unreadable file is skipped, not fatal for the whole search
		}
		hits = append(hits, fileHits...)
		if stop {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return nil, meta, fmt.Errorf("native: grep %q under %q: %w", pattern, scope, walkErr)
	}
	meta.DurationMs = time.Since(start).Milliseconds()
	out := map[string]any{"pattern": pattern, "path": scope, "matches": hits, "count": len(hits)}
	if truncated {
		out["truncated"] = true
	}
	return out, meta, nil
}

// grepFile scans one file for re, appending up to budget hits. stop is true when the budget was
// reached (the caller should stop walking). A binary file (NUL in the sniff window) is skipped.
func grepFile(fsys fs.FS, p string, re *regexp.Regexp, budget int) (hits []map[string]any, stop bool, err error) {
	if budget <= 0 {
		return nil, true, nil
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxWorkspaceGrepFileBytes))
	if err != nil {
		return nil, false, err
	}
	sniff := data
	if len(sniff) > grepBinarySniffBytes {
		sniff = sniff[:grepBinarySniffBytes]
	}
	if bytesIndexNUL(sniff) >= 0 {
		return nil, false, nil // binary file — skip
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		hits = append(hits, map[string]any{"file": p, "line": i + 1, "text": truncateRunes(line, maxGrepLineRunes)})
		if len(hits) >= budget {
			return hits, true, nil
		}
	}
	return hits, false, nil
}

func bytesIndexNUL(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// optionalStringFromWith reads an optional string arg: absent/nil returns ("", false) without error.
func optionalStringFromWith(with map[string]any, key string) (string, bool) {
	v, ok := with[key]
	if !ok || v == nil {
		return "", false
	}
	s, err := scalarToString(v)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(s), true
}
