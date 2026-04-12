// Package local implements the MVP disk-backed workflow runtime (issue #23, design doc section 16).
//
// Use [NewRuntime] with a project root directory and [state.RuntimeStore], then [Runtime.ExecuteWorkflow].
package local
