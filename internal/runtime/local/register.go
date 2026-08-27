package local

import (
	"github.com/LAA-Software-Engineering/terfyn/internal/runtime"
)

func init() {
	runtime.Register(runtime.NameLocal, NewFromDeps)
}
