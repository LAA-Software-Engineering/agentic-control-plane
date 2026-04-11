// Package plan computes desired vs current state diffs and risk summaries.
//
// Deployment comparison uses canonical JSON from encoding/json and spec_hash = SHA-256(hex)
// of those bytes (design doc §14.1, issue #12).
package plan
