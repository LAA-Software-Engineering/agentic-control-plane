package app

// App is the composition root for agentctl. Subsystems (stores, planner,
// runtime) will be assembled here in later phases.
type App struct{}

// New constructs an application instance with default wiring.
func New() *App {
	return &App{}
}

// Run starts the CLI and blocks until the root command finishes.
func (a *App) Run() error {
	return runCLI()
}
