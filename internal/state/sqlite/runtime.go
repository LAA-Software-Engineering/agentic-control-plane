package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

const runSelectColumns = `run_id, workflow_name, env, status, started_at, finished_at, input_json, output_json, error_text, total_cost_usd, workflow_spec_hash, environment_name, tenant_id, thread_id, actor_id, parent_run_id, request_id, idempotency_key, source`

// StartRun inserts a new row in runs (design doc §14.2).
func (s *Store) StartRun(ctx context.Context, r state.Run) error {
	in := r.InputJSON
	if in == "" {
		in = "{}"
	}
	attr := state.RunAttribution{
		TenantID:       r.TenantID,
		ThreadID:       r.ThreadID,
		ActorID:        r.ActorID,
		ParentRunID:    r.ParentRunID,
		RequestID:      r.RequestID,
		IdempotencyKey: r.IdempotencyKey,
		Source:         r.Source,
	}
	state.NormalizeAttribution(&attr)
	if attr.RequestID == "" {
		attr.RequestID = r.RunID
	}
	at := r.StartedAt.UTC().Format(time.RFC3339Nano)
	var parent, idem any
	if attr.ParentRunID != "" {
		parent = attr.ParentRunID
	}
	if attr.IdempotencyKey != "" {
		idem = attr.IdempotencyKey
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO runs (run_id, workflow_name, env, status, started_at, input_json, total_cost_usd, workflow_spec_hash, environment_name, tenant_id, thread_id, actor_id, parent_run_id, request_id, idempotency_key, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, r.RunID, r.WorkflowName, r.Env, r.Status, at, in, r.TotalCostUSD, r.WorkflowSpecHash, r.EnvironmentName,
		attr.TenantID, attr.ThreadID, attr.ActorID, parent, attr.RequestID, idem, attr.Source)
	return err
}

// FinishRun updates status, finished_at, output_json, error_text, and total_cost_usd.
func (s *Store) FinishRun(ctx context.Context, runID, status string, finishedAt time.Time, outputJSON, errorText string, totalCostUSD float64) error {
	fin := finishedAt.UTC().Format(time.RFC3339Nano)
	var out, et any
	if outputJSON != "" {
		out = outputJSON
	}
	if errorText != "" {
		et = errorText
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE runs SET status = ?, finished_at = ?, output_json = ?, error_text = ?, total_cost_usd = ?
WHERE run_id = ?
`, status, fin, out, et, totalCostUSD, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpsertRunStep inserts or updates a step row for (run_id, step_id).
func (s *Store) UpsertRunStep(ctx context.Context, st state.RunStep) error {
	var started, finished any
	if st.StartedAt != nil {
		started = st.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if st.FinishedAt != nil {
		finished = st.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	var inJ, outJ, errT any
	if st.InputJSON != "" {
		inJ = st.InputJSON
	}
	if st.OutputJSON != "" {
		outJ = st.OutputJSON
	}
	if st.ErrorText != "" {
		errT = st.ErrorText
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_steps (run_id, step_id, status, started_at, finished_at, input_json, output_json, error_text, cost_usd)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, step_id) DO UPDATE SET
  status = excluded.status,
  started_at = excluded.started_at,
  finished_at = excluded.finished_at,
  input_json = excluded.input_json,
  output_json = excluded.output_json,
  error_text = excluded.error_text,
  cost_usd = excluded.cost_usd
`, st.RunID, st.StepID, st.Status, started, finished, inJ, outJ, errT, st.CostUSD)
	return err
}

// AppendTraceEvent appends one trace row with the next monotonic seq for run_id.
func (s *Store) AppendTraceEvent(ctx context.Context, runID string, ts time.Time, eventType string, stepID string, dataJSON string) (seq int64, err error) {
	dj := dataJSON
	if dj == "" {
		dj = "{}"
	}
	var sid any
	if stepID != "" {
		sid = stepID
	}
	tss := ts.UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.QueryRowContext(ctx, `SELECT IFNULL(MAX(seq), 0) + 1 FROM trace_events WHERE run_id = ?`, runID).Scan(&seq); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO trace_events (run_id, seq, timestamp, type, step_id, data_json, tenant_id, thread_id, actor_id)
SELECT ?, ?, ?, ?, ?, ?, tenant_id, thread_id, actor_id FROM runs WHERE run_id = ?
`, runID, seq, tss, eventType, sid, dj, runID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunRow(sc rowScanner) (*state.Run, error) {
	var r state.Run
	var started, finished sql.NullString
	var outJ, errT, parent, idem sql.NullString
	if err := sc.Scan(
		&r.RunID, &r.WorkflowName, &r.Env, &r.Status, &started, &finished,
		&r.InputJSON, &outJ, &errT, &r.TotalCostUSD, &r.WorkflowSpecHash, &r.EnvironmentName,
		&r.TenantID, &r.ThreadID, &r.ActorID, &parent, &r.RequestID, &idem, &r.Source,
	); err != nil {
		return nil, err
	}
	if parent.Valid {
		r.ParentRunID = parent.String
	}
	if idem.Valid {
		r.IdempotencyKey = idem.String
	}
	st, err := parseSQLiteTime(started.String)
	if err != nil {
		return nil, fmt.Errorf("started_at: %w", err)
	}
	r.StartedAt = st
	if finished.Valid && finished.String != "" {
		ft, err := parseSQLiteTime(finished.String)
		if err != nil {
			return nil, fmt.Errorf("finished_at: %w", err)
		}
		r.FinishedAt = &ft
	}
	if outJ.Valid {
		r.OutputJSON = outJ.String
	}
	if errT.Valid {
		r.ErrorText = errT.String
	}
	return &r, nil
}

// GetRun returns the run row or sql.ErrNoRows.
func (s *Store) GetRun(ctx context.Context, runID string) (*state.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+runSelectColumns+`
FROM runs
WHERE run_id = ?
`, runID)
	return scanRunRow(row)
}

func clampRunListLimit(limit int) int {
	return state.ClampRunListLimit(limit)
}

// ListRecentRuns returns runs ordered by started_at descending.
func (s *Store) ListRecentRuns(ctx context.Context, limit int) ([]state.Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	limit = clampRunListLimit(limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+runSelectColumns+`
FROM runs
ORDER BY started_at DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []state.Run
	for rows.Next() {
		r, err := scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListRunsByWorkflow returns runs for workflow_name ordered by started_at descending.
func (s *Store) ListRunsByWorkflow(ctx context.Context, workflowName string, limit int) ([]state.Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	limit = clampRunListLimit(limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+runSelectColumns+`
FROM runs
WHERE workflow_name = ?
ORDER BY started_at DESC
LIMIT ?
`, workflowName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []state.Run
	for rows.Next() {
		r, err := scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListRunStepsByRunID returns run_steps for run_id ordered by step_id ascending.
func (s *Store) ListRunStepsByRunID(ctx context.Context, runID string) ([]state.RunStep, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, step_id, status, started_at, finished_at, input_json, output_json, error_text, cost_usd
FROM run_steps
WHERE run_id = ?
ORDER BY step_id ASC
`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []state.RunStep
	for rows.Next() {
		st, err := scanRunStepRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

func scanRunStepRow(sc rowScanner) (*state.RunStep, error) {
	var st state.RunStep
	var started, finished sql.NullString
	var inJ, outJ, errT sql.NullString
	if err := sc.Scan(&st.RunID, &st.StepID, &st.Status, &started, &finished, &inJ, &outJ, &errT, &st.CostUSD); err != nil {
		return nil, err
	}
	if started.Valid && started.String != "" {
		t, err := parseSQLiteTime(started.String)
		if err != nil {
			return nil, fmt.Errorf("started_at: %w", err)
		}
		st.StartedAt = &t
	}
	if finished.Valid && finished.String != "" {
		t, err := parseSQLiteTime(finished.String)
		if err != nil {
			return nil, fmt.Errorf("finished_at: %w", err)
		}
		st.FinishedAt = &t
	}
	if inJ.Valid {
		st.InputJSON = inJ.String
	}
	if outJ.Valid {
		st.OutputJSON = outJ.String
	}
	if errT.Valid {
		st.ErrorText = errT.String
	}
	return &st, nil
}

// ListRunsFiltered returns runs matching optional tenant/thread/actor/workflow filters.
func (s *Store) ListRunsFiltered(ctx context.Context, filter state.RunListFilter) ([]state.Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	limit := clampRunListLimit(filter.Limit)
	q := `SELECT ` + runSelectColumns + ` FROM runs WHERE 1=1`
	var args []any
	if t := strings.TrimSpace(filter.TenantID); t != "" {
		q += ` AND tenant_id = ?`
		args = append(args, t)
	}
	if th := strings.TrimSpace(filter.ThreadID); th != "" {
		q += ` AND thread_id = ?`
		args = append(args, th)
	}
	if a := strings.TrimSpace(filter.ActorID); a != "" {
		q += ` AND actor_id = ?`
		args = append(args, a)
	}
	if w := strings.TrimSpace(filter.WorkflowName); w != "" {
		q += ` AND workflow_name = ?`
		args = append(args, w)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []state.Run
	for rows.Next() {
		r, err := scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListTraceEventsByRunID returns trace rows for run_id ordered by seq ascending.
func (s *Store) ListTraceEventsByRunID(ctx context.Context, runID string) ([]state.TraceEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, seq, timestamp, type, step_id, data_json, tenant_id, thread_id, actor_id
FROM trace_events
WHERE run_id = ?
ORDER BY seq ASC
`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []state.TraceEvent
	for rows.Next() {
		var e state.TraceEvent
		var ts string
		var step sql.NullString
		if err := rows.Scan(&e.RunID, &e.Seq, &ts, &e.Type, &step, &e.DataJSON, &e.TenantID, &e.ThreadID, &e.ActorID); err != nil {
			return nil, err
		}
		t, err := parseSQLiteTime(ts)
		if err != nil {
			return nil, fmt.Errorf("timestamp: %w", err)
		}
		e.Timestamp = t
		if step.Valid {
			e.StepID = step.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteRunsStartedBefore deletes runs older than cutoff (by runs.started_at, RFC3339Nano text compare).
// Foreign keys cascade to run_steps and trace_events (design doc 14.2, issue #75).
func (s *Store) DeleteRunsStartedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("sqlite: nil store")
	}
	cut := cutoff.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE started_at < ?`, cut)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
