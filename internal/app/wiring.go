package app

import (
	_ "github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/cli"
)

func runCLI() int {
	return cli.Main()
}
