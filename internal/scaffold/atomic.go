package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/util"
)

// stagedWrite tracks a temp file pending rename to its final path.
type stagedWrite struct {
	finalPath   string
	tempPath    string
	hadOriginal bool
	original    []byte
}

// committer applies staged file edits atomically (temp write + rename) with rollback on failure.
type committer struct {
	root string
	// failAfter commits successfully for the first N renames, then returns errFailInjected.
	failAfter int
	committed int
}

// errFailInjected simulates a mid-commit failure for rollback tests.
var errFailInjected = fmt.Errorf("scaffold: injected commit failure")

// commitFiles writes every edit via temp+rename. A partial failure rolls back completed renames.
func (c *committer) commitFiles(edits []fileEdit) (err error) {
	if len(edits) == 0 {
		return nil
	}
	var staged []stagedWrite
	defer func() {
		if err != nil {
			rollbackWrites(staged)
		}
	}()

	for _, e := range edits {
		sw, err := c.stageWrite(e)
		if err != nil {
			return err
		}
		staged = append(staged, sw)
	}

	for i, sw := range staged {
		if c.failAfter >= 0 && c.committed >= c.failAfter {
			return errFailInjected
		}
		if err := os.Rename(sw.tempPath, sw.finalPath); err != nil {
			return fmt.Errorf("scaffold: rename %q: %w", sw.finalPath, err)
		}
		staged[i].tempPath = ""
		c.committed++
	}
	return nil
}

type fileEdit struct {
	path    string
	content []byte
}

func (c *committer) stageWrite(e fileEdit) (stagedWrite, error) {
	finalPath, err := c.resolveUnderRoot(e.path)
	if err != nil {
		return stagedWrite{}, err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return stagedWrite{}, fmt.Errorf("scaffold: mkdir %q: %w", filepath.Dir(finalPath), err)
	}

	sw := stagedWrite{finalPath: finalPath}
	if data, err := os.ReadFile(finalPath); err == nil {
		sw.hadOriginal = true
		sw.original = data
	} else if !os.IsNotExist(err) {
		return stagedWrite{}, fmt.Errorf("scaffold: read %q: %w", finalPath, err)
	}

	tempPath, err := tempFileInDir(filepath.Dir(finalPath), filepath.Base(finalPath))
	if err != nil {
		return stagedWrite{}, err
	}
	sw.tempPath = tempPath

	if err := os.WriteFile(tempPath, e.content, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return stagedWrite{}, fmt.Errorf("scaffold: write temp %q: %w", tempPath, err)
	}
	return sw, nil
}

func (c *committer) resolveUnderRoot(path string) (string, error) {
	if filepath.IsAbs(path) {
		abs := filepath.Clean(path)
		if !util.IsUnderRoot(c.root, abs) {
			return "", fmt.Errorf("scaffold: path %q is outside project root", path)
		}
		return abs, nil
	}
	abs := filepath.Join(c.root, filepath.FromSlash(path))
	abs = filepath.Clean(abs)
	if !util.IsUnderRoot(c.root, abs) {
		return "", fmt.Errorf("scaffold: path %q resolves outside project root", path)
	}
	return abs, nil
}

func tempFileInDir(dir, base string) (string, error) {
	f, err := os.CreateTemp(dir, "."+base+".agentctl-tmp-*")
	if err != nil {
		return "", fmt.Errorf("scaffold: create temp in %q: %w", dir, err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("scaffold: close temp: %w", err)
	}
	return name, nil
}

func rollbackWrites(staged []stagedWrite) {
	for _, sw := range staged {
		if sw.tempPath != "" {
			_ = os.Remove(sw.tempPath)
		}
	}
	for i := len(staged) - 1; i >= 0; i-- {
		sw := staged[i]
		if sw.tempPath != "" {
			continue
		}
		if sw.hadOriginal {
			_ = os.WriteFile(sw.finalPath, sw.original, 0o644)
			continue
		}
		_ = os.Remove(sw.finalPath)
	}
}
