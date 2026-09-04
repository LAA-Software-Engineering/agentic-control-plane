package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Legacy fields removed from the canonical model (ADR 007 step 1) that `terfyn migrate --to-agent` must
// still tolerate in legacy YAML: it accepts them, warns once per field per resource, and omits them from
// the generated .agent (the strict loader would otherwise reject them as unknown keys). Keyed by the
// resource Kind that could carry each field.
var legacyRemovedFieldsByKind = map[string][]string{
	"Tool":   {"permissions"},
	"Policy": {"security"},
	"Agent":  {"memory", "runtime"},
}

// legacyFieldWarning is the deprecation notice for a removed field. `permissions` gets distinct wording
// because it did affect plan hints (a superseded heuristic); the others were never semantically consumed.
func legacyFieldWarning(kind, name, field string) string {
	if kind == "Tool" && field == "permissions" {
		return fmt.Sprintf("Tool/%s: spec.permissions is deprecated; its plan-only write heuristic has been superseded by operations/effects capability analysis and is omitted from generated .agent", name)
	}
	if kind == "Project" && field == "providers.tools" {
		return fmt.Sprintf("Project/%s: spec.providers.tools is no longer part of the canonical model (its mcp.enabled flag was always a no-op) and has no runtime semantics; omitted from generated .agent", name)
	}
	return fmt.Sprintf("%s/%s: spec.%s is no longer part of the canonical model and has no runtime semantics; omitted from generated .agent", kind, name, field)
}

// prepareMigrationRoot returns a project root the strict YAML loader can ingest. If no YAML resource
// under root carries a removed legacy field, it returns root unchanged with a no-op cleanup. Otherwise
// it copies the tree to a temp directory, strips the legacy fields from each YAML resource (collecting
// one warning per field per resource), and returns the temp root plus a cleanup to remove it.
func prepareMigrationRoot(root string) (migRoot string, warnings []string, cleanup func(), err error) {
	noop := func() {}
	found, err := treeHasLegacyFields(root)
	if err != nil {
		return "", nil, noop, err
	}
	if !found {
		return root, nil, noop, nil
	}
	tmp, err := os.MkdirTemp("", "terfyn-migrate-*")
	if err != nil {
		return "", nil, noop, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	warnings, err = copyTreeStrippingLegacy(root, tmp)
	if err != nil {
		cleanup()
		return "", nil, noop, err
	}
	return tmp, warnings, cleanup, nil
}

// treeHasLegacyFields reports whether any YAML resource under root carries a removed legacy field.
func treeHasLegacyFields(root string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found || !isYAMLFile(path) {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if w, _ := stripLegacyYAMLDoc(data); len(w) > 0 {
			found = true
		}
		return nil
	})
	return found, err
}

// copyTreeStrippingLegacy mirrors src into dst, stripping removed legacy fields from every YAML resource
// file (other files are copied verbatim), and returns the collected deprecation warnings (sorted).
func copyTreeStrippingLegacy(src, dst string) ([]string, error) {
	var warnings []string
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out := data
		if isYAMLFile(path) {
			w, cleaned := stripLegacyYAMLDoc(data)
			if len(w) > 0 {
				warnings = append(warnings, w...)
				out = cleaned
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, out, 0o644)
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(warnings)
	return warnings, nil
}

// stripLegacyYAMLDoc removes removed legacy fields (per the doc's kind) from a single YAML resource
// document, returning one warning per removed field and the cleaned bytes. A doc that is not a single
// resource mapping, or carries no legacy field, yields no warnings and the input is left to the loader.
func stripLegacyYAMLDoc(data []byte) (warnings []string, cleaned []byte) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, data
	}
	kind, _ := doc["kind"].(string)
	fields := legacyRemovedFieldsByKind[kind]
	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		return nil, data
	}
	name := ""
	if md, ok := doc["metadata"].(map[string]any); ok {
		name, _ = md["name"].(string)
	}
	changed := false
	for _, f := range fields {
		if _, present := spec[f]; present {
			delete(spec, f)
			warnings = append(warnings, legacyFieldWarning(kind, name, f))
			changed = true
		}
	}
	// The one nested removed field: Project spec.providers.tools (its mcp.enabled flag was always a
	// no-op). The strict loader would reject it as an unknown key, so strip it here too. Providers is
	// kept (its models are canonical); only the tools sub-map is removed.
	if kind == "Project" {
		if providers, ok := spec["providers"].(map[string]any); ok {
			if _, present := providers["tools"]; present {
				delete(providers, "tools")
				warnings = append(warnings, legacyFieldWarning(kind, name, "providers.tools"))
				changed = true
			}
		}
	}
	if !changed {
		return nil, data
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, data
	}
	return warnings, out
}

func isYAMLFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}
