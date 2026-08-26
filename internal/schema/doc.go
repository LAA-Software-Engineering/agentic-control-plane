// Package schema loads JSON Schema files and validates JSON instances (design doc §9.1–§9.2, §13.3).
// Use ResolveSchemaPath with the project root, then Validate(absPath, jsonBytes).
// LoadDocument compiles the same files onto the project graph for static wiring checks (issue #193).
package schema
