package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/spf13/cobra"
)

const logsRunListDefaultLimit = 50

func newLogsCmd() *cobra.Command {
	var runID string
	var workflow string

	cmd := &cobra.Command{
		Use:          "logs",
		Short:        "Show workflow runs and trace events from SQLite",
		SilenceUsage: true,
		Long: `Inspect execution history stored in the SQLite state database.

Without filters, lists recent runs (newest first). Use --run to print trace events for one run
(ordered by seq), or --workflow to print events for recent runs of a workflow name.

Examples:
  agentctl logs
  agentctl logs --run <run-id>
  agentctl logs --workflow pr-review

Exit codes (section 11.2):
  0 — success
  1 — generic failure (e.g. cannot open SQLite)
  2 — validation failure (unknown run id, invalid flags)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runLogs(cmd, runID, workflow)
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "show trace events for this run id")
	cmd.Flags().StringVar(&workflow, "workflow", "", "show trace events for recent runs of this workflow")
	return cmd
}

func runLogs(cmd *cobra.Command, runID, workflow string) error {
	ctx := context.Background()
	g := Globals()

	runID = strings.TrimSpace(runID)
	workflow = strings.TrimSpace(workflow)
	if runID != "" && workflow != "" {
		return NewExitErrorf(ExitValidationError, "logs: use only one of --run or --workflow")
	}

	graph, root, err := prepareProjectGraph(g.ProjectRoot, g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	dsn, err := resolveStateSQLitePath(root, graph, g.StatePath)
	if err != nil {
		return fmt.Errorf("logs: resolve state path: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("logs: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	switch {
	case runID != "":
		return writeLogsForRun(cmd, ctx, st, dsn, runID, g)
	case workflow != "":
		return writeLogsForWorkflow(cmd, ctx, st, dsn, workflow, g)
	default:
		return writeLogsRunList(cmd, ctx, st, dsn, g)
	}
}

func writeLogsForRun(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn, runID string, g *Global) error {
	if _, err := st.GetRun(ctx, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NewExitErrorf(ExitValidationError, "logs: unknown run %q", runID)
		}
		return fmt.Errorf("logs: get run: %w", err)
	}
	events, err := st.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		return fmt.Errorf("logs: list trace events: %w", err)
	}
	return writeLogsEventsOutput(cmd, dsn, runID, "", events, g)
}

func writeLogsForWorkflow(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn, workflow string, g *Global) error {
	runs, err := st.ListRunsByWorkflow(ctx, workflow, logsRunListDefaultLimit)
	if err != nil {
		return fmt.Errorf("logs: list runs: %w", err)
	}

	if g.Output != render.FormatTable {
		type runEntry struct {
			RunID    string           `json:"runId"`
			Status   string           `json:"status"`
			Workflow string           `json:"workflow"`
			Events   []logEventRecord `json:"events"`
		}
		entries := make([]runEntry, 0, len(runs))
		for _, r := range runs {
			ev, err := st.ListTraceEventsByRunID(ctx, r.RunID)
			if err != nil {
				return fmt.Errorf("logs: list trace events: %w", err)
			}
			entries = append(entries, runEntry{
				RunID:    r.RunID,
				Status:   r.Status,
				Workflow: r.WorkflowName,
				Events:   traceEventsToRecords(ev),
			})
		}
		payload := map[string]any{
			"statePath": dsn,
			"workflow":  workflow,
			"runs":      entries,
		}
		out := cmd.OutOrStdout()
		if g.Output == render.FormatJSON {
			return render.WriteJSON(out, payload)
		}
		return render.WriteYAML(out, payload)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "State: %s\nWorkflow filter: %s\n\n", dsn, workflow)
	if len(runs) == 0 {
		fmt.Fprintln(&b, "No runs found.")
		_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
		return err
	}
	for i, r := range runs {
		if i > 0 {
			b.WriteString("\n")
		}
		ev, err := st.ListTraceEventsByRunID(ctx, r.RunID)
		if err != nil {
			return fmt.Errorf("logs: list trace events: %w", err)
		}
		fmt.Fprintf(&b, "=== Run %s (%s, %s) ===\n", r.RunID, r.WorkflowName, r.Status)
		b.WriteString(formatTraceTable(ev))
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

func writeLogsRunList(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn string, g *Global) error {
	runs, err := st.ListRecentRuns(ctx, logsRunListDefaultLimit)
	if err != nil {
		return fmt.Errorf("logs: list runs: %w", err)
	}
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{
			"statePath": dsn,
			"runs":      runsToJSON(runs),
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"statePath": dsn,
			"runs":      runsToJSON(runs),
		})
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "State: %s\n\n", dsn)
		if len(runs) == 0 {
			fmt.Fprintf(&b, "No runs found.\n")
			_, err := fmt.Fprint(out, b.String())
			return err
		}
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "RUN ID\tWORKFLOW\tENV\tSTATUS\tSTARTED")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				r.RunID, r.WorkflowName, r.Env, r.Status, r.StartedAt.UTC().Format(time.RFC3339),
			)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		_, err := fmt.Fprint(out, b.String())
		return err
	}
}

type logEventRecord struct {
	Seq       int64           `json:"seq"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	StepID    string          `json:"stepId,omitempty"`
	Data      json.RawMessage `json:"data"`
}

func traceEventsToRecords(events []state.TraceEvent) []logEventRecord {
	out := make([]logEventRecord, 0, len(events))
	for _, e := range events {
		rec := logEventRecord{
			Seq:       e.Seq,
			Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
			Type:      e.Type,
			StepID:    e.StepID,
		}
		if e.DataJSON != "" {
			rec.Data = json.RawMessage(e.DataJSON)
		} else {
			rec.Data = json.RawMessage("{}")
		}
		out = append(out, rec)
	}
	return out
}

func writeLogsEventsOutput(cmd *cobra.Command, dsn, runID, workflow string, events []state.TraceEvent, g *Global) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		payload := map[string]any{
			"statePath": dsn,
			"runId":     runID,
			"events":    traceEventsToRecords(events),
		}
		if workflow != "" {
			payload["workflow"] = workflow
		}
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
		payload := map[string]any{
			"statePath": dsn,
			"runId":     runID,
			"events":    traceEventsToRecords(events),
		}
		if workflow != "" {
			payload["workflow"] = workflow
		}
		return render.WriteYAML(out, payload)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "State: %s\nRun ID: %s\n\n", dsn, runID)
		if workflow != "" {
			fmt.Fprintf(&b, "Workflow filter: %s\n\n", workflow)
		}
		b.WriteString(formatTraceTable(events))
		_, err := fmt.Fprint(out, b.String())
		return err
	}
}

func formatTraceTable(events []state.TraceEvent) string {
	if len(events) == 0 {
		return "No trace events.\n"
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tTIME\tTYPE\tSTEP\tDATA")
	for _, e := range events {
		step := e.StepID
		if step == "" {
			step = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			e.Seq,
			e.Timestamp.UTC().Format(time.RFC3339),
			e.Type,
			step,
			clipJSONForTable(e.DataJSON, 96),
		)
	}
	_ = w.Flush()
	return b.String()
}

func clipJSONForTable(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func runsToJSON(runs []state.Run) []map[string]any {
	out := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		in := r.InputJSON
		if in == "" {
			in = "{}"
		}
		m := map[string]any{
			"runId":        r.RunID,
			"workflow":     r.WorkflowName,
			"env":          r.Env,
			"status":       r.Status,
			"startedAt":    r.StartedAt.UTC().Format(time.RFC3339Nano),
			"totalCostUsd": r.TotalCostUSD,
			"input":        json.RawMessage(in),
		}
		if r.FinishedAt != nil {
			m["finishedAt"] = r.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		if r.OutputJSON != "" {
			m["output"] = json.RawMessage(r.OutputJSON)
		}
		if r.ErrorText != "" {
			m["error"] = r.ErrorText
		}
		out = append(out, m)
	}
	return out
}
