package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// SaveCheckpoint appends one checkpoint row with the next monotonic seq for run_id.
func (s *Store) SaveCheckpoint(ctx context.Context, cp state.RunCheckpoint) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite: nil store")
	}
	ctxJ := cp.ContextJSON
	if ctxJ == "" {
		ctxJ = "{}"
	}
	created := cp.CreatedAt.UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT IFNULL(MAX(seq), 0) + 1 FROM run_checkpoints WHERE run_id = ?`, cp.RunID).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO run_checkpoints (run_id, seq, step_index, step_id, context_json, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, cp.RunID, seq, cp.StepIndex, cp.StepID, ctxJ, cp.Status, created); err != nil {
		return err
	}
	return tx.Commit()
}

func scanCheckpointRow(sc rowScanner) (*state.RunCheckpoint, error) {
	var cp state.RunCheckpoint
	var created string
	if err := sc.Scan(&cp.RunID, &cp.Seq, &cp.StepIndex, &cp.StepID, &cp.ContextJSON, &cp.Status, &created); err != nil {
		return nil, err
	}
	t, err := parseSQLiteTime(created)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}
	cp.CreatedAt = t
	return &cp, nil
}

// ListCheckpointsByRunID returns all checkpoints for run_id ordered by seq ascending.
func (s *Store) ListCheckpointsByRunID(ctx context.Context, runID string) ([]state.RunCheckpoint, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, seq, step_index, step_id, context_json, status, created_at
FROM run_checkpoints
WHERE run_id = ?
ORDER BY seq ASC
`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []state.RunCheckpoint
	for rows.Next() {
		cp, err := scanCheckpointRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *cp)
	}
	return out, rows.Err()
}

// GetLatestCheckpoint returns the newest checkpoint for run_id or sql.ErrNoRows.
func (s *Store) GetLatestCheckpoint(ctx context.Context, runID string) (*state.RunCheckpoint, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqlite: nil store")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, seq, step_index, step_id, context_json, status, created_at
FROM run_checkpoints
WHERE run_id = ?
ORDER BY seq DESC
LIMIT 1
`, runID)
	return scanCheckpointRow(row)
}

// UpdateRunStatus sets runs.status without updating finished_at or output.
func (s *Store) UpdateRunStatus(ctx context.Context, runID, status string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite: nil store")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status = ? WHERE run_id = ?`, status, runID)
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
