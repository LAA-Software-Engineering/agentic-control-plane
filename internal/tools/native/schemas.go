package native

import "encoding/json"

// operationInputSchemas maps a native operation to the JSON Schema for its input.
// The agent tool-calling loop advertises these to the model so an agent knows a
// tool's required arguments — without them the model is handed an empty parameter
// schema and cannot pass e.g. `owner` to pull_request.get or `path` to read_file,
// so native tool calls fail on real providers (the mock model does not validate).
// Operations absent from this map advertise the permissive default (any object).
var operationInputSchemas = map[string]json.RawMessage{
	// workspace adapter
	"read_file":  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Workspace-relative file path."}},"required":["path"],"additionalProperties":false}`),
	"write_file": json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Workspace-relative file path."},"content":{"type":"string","description":"Full new contents of the file."}},"required":["path","content"],"additionalProperties":false}`),
	"run_tests":  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false,"description":"Runs the operator-configured test command; takes no arguments."}`),

	// github adapter
	"pull_request.get":          githubTripletSchema,
	"pull_request.diff":         githubTripletSchema,
	"issues.get":                githubTripletSchema,
	"issues.comment":            json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer"},"body":{"type":"string","description":"Comment body (Markdown)."}},"required":["owner","repo","number","body"]}`),
	"pull_request.post_comment": json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer"},"body":{"type":"string","description":"Comment body (Markdown)."}},"required":["owner","repo","number","body"]}`),
	"check_runs.list":           json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"ref":{"type":"string","description":"Commit SHA or ref."}},"required":["owner","repo","ref"]}`),

	// git adapter
	"create_branch": json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"New branch name."}},"required":["name"],"additionalProperties":false}`),
	"push_branch":   json.RawMessage(`{"type":"object","properties":{"branch":{"type":"string","description":"Branch to push to the configured remote."}},"required":["branch"],"additionalProperties":false}`),
}

// githubTripletSchema is the owner/repo/number input shared by several github ops.
var githubTripletSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer","description":"Issue or PR number."}},"required":["owner","repo","number"]}`)

// OperationInputSchema returns the input JSON Schema for a native operation and
// whether one is defined. A caller that gets ok=false should advertise a
// permissive default so the operation stays callable.
func OperationInputSchema(operation string) (json.RawMessage, bool) {
	s, ok := operationInputSchemas[operation]
	return s, ok
}
