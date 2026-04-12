// Package testkit parses and runs YAML fixture workflow tests (design doc §10.2, §17.4, issue #73).
package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Suite is one workflow test file (typically under <project>/tests/).
type Suite struct {
	APIVersion string `yaml:"apiVersion"`
	Workflow   string `yaml:"workflow"`
	Cases      []Case `yaml:"cases"`
}

// Case is a single scenario: workflow input and success or expected-failure assertions.
type Case struct {
	Name        string         `yaml:"name"`
	Input       map[string]any `yaml:"input"`
	Expect      Expect         `yaml:"expect"`
	ExpectError bool           `yaml:"expectError"`
}

// Expect holds assertions for a successful run.
type Expect struct {
	OutputContains []string `yaml:"outputContains"`
}

// ParseSuiteFile reads and parses one YAML suite file.
func ParseSuiteFile(path string) (*Suite, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSuiteBytes(b)
}

// ParseSuiteBytes decodes YAML into a Suite.
func ParseSuiteBytes(data []byte) (*Suite, error) {
	var s Suite
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("testkit: parse suite: %w", err)
	}
	s.Workflow = strings.TrimSpace(s.Workflow)
	if s.Workflow == "" {
		return nil, fmt.Errorf("testkit: suite missing workflow name")
	}
	if len(s.Cases) == 0 {
		return nil, fmt.Errorf("testkit: suite has no cases")
	}
	for i := range s.Cases {
		if strings.TrimSpace(s.Cases[i].Name) == "" {
			return nil, fmt.Errorf("testkit: case %d missing name", i)
		}
	}
	return &s, nil
}

// DiscoverSuitePaths returns sorted YAML paths under root/tests (recursive).
// If tests/ is missing, it returns nil, nil.
func DiscoverSuitePaths(root string) ([]string, error) {
	testsDir := filepath.Join(root, "tests")
	if _, err := os.Stat(testsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(testsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
