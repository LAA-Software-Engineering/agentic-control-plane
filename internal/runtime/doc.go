// Package runtime defines the execution adapter contract between the control plane
// (declare / validate / plan / apply / inspect) and workflow runtimes.
//
// The control plane resolves configuration into an immutable [config.ResolvedConfig]
// snapshot and selects a registered runtime via [Lookup]. Runtimes execute workflows
// and report status; they must not reload project YAML/TOML themselves.
//
// The MVP local disk-backed implementation lives in
// [github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local].
package runtime
