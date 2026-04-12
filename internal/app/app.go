package app

import "os"

// App is the composition root for agentctl. Subsystems (stores, planner,
// runtime) will be assembled here in later phases.
type App struct{}

// New constructs an application instance with default wiring.
func New() *App {
	return &App{}
}

// Run starts the CLI and returns a process exit code (design doc section 11.2).
func (a *App) Run() int {
	return runCLI()
}

// RunAndExit runs the CLI and terminates the process (convenience for main).
func (a *App) RunAndExit() {
	os.Exit(a.Run())
}
