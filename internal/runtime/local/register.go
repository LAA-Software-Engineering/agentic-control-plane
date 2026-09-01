package local

import (
	"github.com/Terfyn/terfyn/internal/runtime"
)

func init() {
	runtime.Register(runtime.NameLocal, NewFromDeps)
}
