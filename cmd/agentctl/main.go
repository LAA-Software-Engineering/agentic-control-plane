package main

import (
	"fmt"
	"os"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/app"
)

func main() {
	if err := app.New().Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
