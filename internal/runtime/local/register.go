package local

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
)

func init() {
	runtime.Register(runtime.NameLocal, NewFromDeps)
}
