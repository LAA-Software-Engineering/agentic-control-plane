package schema

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// newCompiler returns a compiler with a pinned default draft so behavior stays stable
// as the library's implicit default changes (see jsonschema.Compiler.DefaultDraft).
func newCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	return c
}
