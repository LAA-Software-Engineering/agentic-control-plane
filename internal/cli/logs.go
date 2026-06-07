package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var runID string
	var workflow string
	var tenantID string
	var threadID string
	var actorID string
	var eventTypes []string

	cmd := &cobra.Command{
		Use:          "logs",
		Short:        "Show workflow runs and trace events from SQLite",
		SilenceUsage: true,
		Long: `Inspect execution history stored in the SQLite state database.

Without filters, lists recent runs (newest first). Use --run to print trace events for one run
(ordered by seq), --workflow to print events for recent runs of a workflow name, or
--tenant-id / --thread-id / --actor-id to filter the run list (combinable).
Use --event to filter trace events by closed event type (repeatable; issue #115).

Examples:
  agentctl logs
  agentctl logs --run <run-id>
  agentctl logs --run <run-id> --event tool_execution
  agentctl logs --workflow pr-review
  agentctl logs --tenant-id acme --thread-id prod-session-1

Exit codes (section 11.2):
  0 — success
  1 — generic failure (e.g. cannot open SQLite)
  2 — validation failure (unknown run id, invalid flags)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runLogs(cmd, runID, workflow, tenantID, threadID, actorID, eventTypes)
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "show trace events for this run id")
	cmd.Flags().StringVar(&workflow, "workflow", "", "show trace events for recent runs of this workflow")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "filter runs by tenant id")
	cmd.Flags().StringVar(&threadID, "thread-id", "", "filter runs by thread id")
	cmd.Flags().StringVar(&actorID, "actor-id", "", "filter runs by actor id")
	cmd.Flags().StringArrayVar(&eventTypes, "event", nil, "filter trace events by event type (repeatable)")
	return cmd
}

func runLogs(cmd *cobra.Command, runID, workflow, tenantID, threadID, actorID string, eventTypes []string) error {
	ctx := context.Background()
	g := Globals()

	runID = strings.TrimSpace(runID)
	workflow = strings.TrimSpace(workflow)
	tenantID = strings.TrimSpace(tenantID)
	threadID = strings.TrimSpace(threadID)
	actorID = strings.TrimSpace(actorID)
	if runID != "" && workflow != "" {
		return NewExitErrorf(ExitValidationError, "logs: use only one of --run or --workflow")
	}
	if runID != "" && (tenantID != "" || threadID != "" || actorID != "") {
		return NewExitErrorf(ExitValidationError, "logs: --run cannot be combined with tenant/thread/actor filters")
	}
	eventFilter, err := parseLogsEventFilter(eventTypes)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	graph, root, err := prepareProjectGraph(g)
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

	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -n)
		if _, err := st.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return fmt.Errorf("logs: prune trace runs: %w", err)
		}
	}

	filter := state.RunListFilter{
		TenantID:     tenantID,
		ThreadID:     threadID,
		ActorID:      actorID,
		WorkflowName: workflow,
	}

	switch {
	case runID != "":
		return writeLogsForRun(cmd, ctx, st, dsn, runID, eventFilter, g)
	case workflow != "" || tenantID != "" || threadID != "" || actorID != "":
		return writeLogsFiltered(cmd, ctx, st, dsn, filter, eventFilter, g)
	default:
		return writeLogsRunList(cmd, ctx, st, dsn, g)
	}
}

func writeLogsForRun(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn, runID string, eventFilter map[string]struct{}, g *Global) error {
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
	events = trace.NormalizeEvents(events)
	events = filterTraceEvents(events, eventFilter)
	return writeLogsEventsOutput(cmd, dsn, runID, "", events, g)
}

func writeLogsFiltered(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn string, filter state.RunListFilter, eventFilter map[string]struct{}, g *Global) error {
	filter.Limit = state.DefaultRunListLimit
	runs, err := st.ListRunsFiltered(ctx, filter)
	if err != nil {
		return fmt.Errorf("logs: list runs: %w", err)
	}
	workflow := strings.TrimSpace(filter.WorkflowName)

	if g.Output != render.FormatTable {
		type runEntry struct {
			RunID    string                       `json:"runId"`
			Status   string                       `json:"status"`
			Workflow string                       `json:"workflow"`
			Events   []statejson.TraceEventRecord `json:"events"`
		}
		entries := make([]runEntry, 0, len(runs))
		for _, r := range runs {
			ev, err := st.ListTraceEventsByRunID(ctx, r.RunID)
			if err != nil {
				return fmt.Errorf("logs: list trace events: %w", err)
			}
			ev = trace.NormalizeEvents(ev)
			ev = filterTraceEvents(ev, eventFilter)
			entries = append(entries, runEntry{
				RunID:    r.RunID,
				Status:   r.Status,
				Workflow: r.WorkflowName,
				Events:   statejson.TraceEvents(ev),
			})
		}
		payload := struct {
			StatePath string     `json:"statePath"`
			Workflow  string     `json:"workflow"`
			Runs      []runEntry `json:"runs"`
		}{StatePath: dsn, Workflow: workflow, Runs: entries}
		out := cmd.OutOrStdout()
		if g.Output == render.FormatJSON {
			return render.WriteJSON(out, payload)
		}
		return render.WriteYAML(out, payload)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "State: %s\n", dsn)
	writeLogsFilterHeader(&b, filter)
	b.WriteString("\n")
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
		ev = trace.NormalizeEvents(ev)
		ev = filterTraceEvents(ev, eventFilter)
		fmt.Fprintf(&b, "=== Run %s (%s, %s) ===\n", r.RunID, r.WorkflowName, r.Status)
		b.WriteString(formatTraceTable(ev))
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

func writeLogsFilterHeader(b *strings.Builder, filter state.RunListFilter) {
	if w := strings.TrimSpace(filter.WorkflowName); w != "" {
		fmt.Fprintf(b, "Workflow filter: %s\n", w)
	}
	if t := strings.TrimSpace(filter.TenantID); t != "" {
		fmt.Fprintf(b, "Tenant filter: %s\n", t)
	}
	if th := strings.TrimSpace(filter.ThreadID); th != "" {
		fmt.Fprintf(b, "Thread filter: %s\n", th)
	}
	if a := strings.TrimSpace(filter.ActorID); a != "" {
		fmt.Fprintf(b, "Actor filter: %s\n", a)
	}
}

func writeLogsRunList(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, dsn string, g *Global) error {
	runs, err := st.ListRecentRuns(ctx, state.DefaultRunListLimit)
	if err != nil {
		return fmt.Errorf("logs: list runs: %w", err)
	}
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, statejson.RunListPayload{StatePath: dsn, Runs: statejson.Runs(runs)})
	case render.FormatYAML:
		return render.WriteYAML(out, statejson.RunListPayload{StatePath: dsn, Runs: statejson.Runs(runs)})
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "State: %s\n\n", dsn)
		if len(runs) == 0 {
			fmt.Fprintf(&b, "No runs found.\n")
			_, err := fmt.Fprint(out, b.String())
			return err
		}
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "RUN ID\tWORKFLOW\tENV\tSTATUS\tTENANT\tTHREAD\tACTOR\tSTARTED")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.RunID, r.WorkflowName, r.Env, r.Status, r.TenantID, r.ThreadID, r.ActorID,
				r.StartedAt.UTC().Format(time.RFC3339),
			)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		_, err := fmt.Fprint(out, b.String())
		return err
	}
}

func writeLogsEventsOutput(cmd *cobra.Command, dsn, runID, workflow string, events []state.TraceEvent, g *Global) error {
	out := cmd.OutOrStdout()
	payload := statejson.RunEventsPayload{
		StatePath: dsn,
		RunID:     runID,
		Workflow:  workflow,
		Events:    statejson.TraceEvents(events),
	}
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
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
	fmt.Fprintln(w, "SEQ\tTIME\tTYPE\tACTOR\tSTEP\tDATA")
	for _, e := range events {
		step := e.StepID
		if step == "" {
			step = "-"
		}
		actor := e.ActorType
		if actor == "" {
			actor = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			e.Seq,
			e.Timestamp.UTC().Format(time.RFC3339),
			e.Type,
			actor,
			step,
			clipJSONForTable(e.DataJSON, 96),
		)
	}
	_ = w.Flush()
	return b.String()
}

func parseLogsEventFilter(raw []string) (map[string]struct{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		et, known := trace.ParseEventType(s)
		if !known {
			return nil, fmt.Errorf("logs: unknown event type %q (known: %s)", s, strings.Join(trace.AllEventTypeStrings(), ", "))
		}
		out[et.String()] = struct{}{}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func filterTraceEvents(events []state.TraceEvent, filter map[string]struct{}) []state.TraceEvent {
	if len(filter) == 0 {
		return events
	}
	out := make([]state.TraceEvent, 0, len(events))
	for _, e := range events {
		if _, ok := filter[trace.NormalizeStoredEventType(e.Type)]; ok {
			out = append(out, e)
		}
	}
	return out
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
