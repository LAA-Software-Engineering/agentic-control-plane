// Package local implements the MVP disk-backed workflow runtime (issue #23, design doc section 16).
//
// Construct a runtime via [runtime.Lookup] and [NewFromDeps], then call [Runtime.Invoke] or
// [Runtime.Resume] with a [config.ResolvedConfig] snapshot from the control plane.
package local
