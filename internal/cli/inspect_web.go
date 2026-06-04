package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/inspect"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/spf13/cobra"
)

func runInspectWeb(cmd *cobra.Command, port int, traceUIBase string) error {
	g := Globals()
	graph, root, err := prepareProjectGraph(g.ProjectRoot, g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	env := planEnvironment(g)
	dsn, err := resolveStateSQLitePath(root, graph, g.StatePath)
	if err != nil {
		return fmt.Errorf("inspect: resolve state path: %w", err)
	}
	if _, err := os.Stat(dsn); err != nil {
		if os.IsNotExist(err) {
			return NewExitErrorf(ExitValidationError, "inspect: state database %q does not exist (run plan/apply or a workflow first)", dsn)
		}
		return fmt.Errorf("inspect: stat state %q: %w", dsn, err)
	}

	ctx := context.Background()
	st, err := sqlite.OpenReadOnly(ctx, dsn)
	if err != nil {
		return fmt.Errorf("inspect: open sqlite read-only %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	cfg := inspect.Config{
		Port:           port,
		StatePath:      dsn,
		Env:            env,
		ProjectName:    strings.TrimSpace(graph.Meta.Name),
		TraceUIBaseURL: strings.TrimSpace(traceUIBase),
	}
	srv, err := inspect.NewServer(st, cfg)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}

	addr := srv.ListenAddr()
	url := "http://" + addr + "/"
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Inspector listening on %s (read-only)\nOpen %s\nPress Ctrl+C to stop.\n", addr, url); err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.ListenAndServe(runCtx); err != nil && runCtx.Err() == nil {
		return fmt.Errorf("inspect: server: %w", err)
	}
	return nil
}
