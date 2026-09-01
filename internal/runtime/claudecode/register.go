package claudecode

import "github.com/Terfyn/terfyn/internal/runtime"

func init() {
	runtime.Register(Name, NewFromDeps)
}
