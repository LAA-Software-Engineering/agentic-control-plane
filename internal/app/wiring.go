package app

import (
	_ "github.com/Terfyn/terfyn/internal/runtime/claudecode"
	_ "github.com/Terfyn/terfyn/internal/runtime/gemini"
	_ "github.com/Terfyn/terfyn/internal/runtime/local"

	"github.com/Terfyn/terfyn/internal/cli"
)

func runCLI() int {
	return cli.Main()
}
