// Package project loads the root project.yaml, expands spec.imports, merges resources
// into a spec.ProjectGraph. Reference checks use [ResolveReferences] (delegates to spec);
// full MVP validation is [spec.ValidateProjectGraph].
package project
