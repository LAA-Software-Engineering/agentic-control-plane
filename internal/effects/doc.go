// Package effects computes transitive effect bounds over a resolved ProjectGraph
// (issue #189, ADR 002).
//
// [Compute] is pure: no I/O, no SQLite, and no Environment overlay application.
// The caller passes an already-resolved desired graph. Bounds are over declared
// spec.operations and advertised uses on that graph; MCP tools/list is not called
// (#204 pin is not shipped). Runtime CheckToolCall is unchanged (#190 / #191 are
// not implemented here).
//
// Two edge kinds are preserved on witness hops:
//
//   - static — workflow step uses: naming a tool operation
//   - autonomous — agent.spec.tools grants; every advertised operation’s declared
//     effects join the bound even when no uses: names them
//
// Grants resolve through concrete operations (tool.<name>.<operation>), never
// effect classes. A reachable operation with no declared effects is an explicit
// unknown (fail-closed), not an empty/allow set. Declared effects on operations
// not reachable from a root are reported as [Unreachable], not omitted.
//
// Cyclic graphs terminate via a visiting set (least fixed point). Production YAML
// has no subworkflows (#194); tests inject synthetic workflow→workflow edges.
package effects
