package app

import (
	_ "github.com/LAA-Software-Engineering/terfyn/internal/runtime/local"

	"github.com/LAA-Software-Engineering/terfyn/internal/cli"
)

func runCLI() int {
	return cli.Main()
}
