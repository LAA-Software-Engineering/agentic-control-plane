-- Fixed-width timestamp normalization (issue #385): runs.started_at is ORDER BY'd
-- and range-compared (retention) as text, but stored as RFC3339Nano, which trims
-- trailing fractional zeros. A whole-second value (…:00Z) then sorts AFTER a
-- fractional one in the same second (…:00.5Z) because 'Z' (0x5A) > '.' (0x2E).
-- Rewrite existing rows to a fixed nine-digit fraction so text order == time order,
-- matching formatSQLiteTime for all rows written from now on. All stored values are
-- UTC (they end in 'Z'); rows already at nine digits are rewritten to themselves.

UPDATE runs
SET started_at = CASE
    WHEN instr(started_at, '.') > 0 THEN
        substr(started_at, 1, instr(started_at, '.') - 1)
        || '.'
        || substr(
             substr(started_at, instr(started_at, '.') + 1,
                    length(started_at) - instr(started_at, '.') - 1)
             || '000000000', 1, 9)
        || 'Z'
    WHEN substr(started_at, length(started_at), 1) = 'Z' THEN
        substr(started_at, 1, length(started_at) - 1) || '.000000000Z'
    ELSE started_at
END
WHERE started_at IS NOT NULL;

UPDATE runs
SET finished_at = CASE
    WHEN instr(finished_at, '.') > 0 THEN
        substr(finished_at, 1, instr(finished_at, '.') - 1)
        || '.'
        || substr(
             substr(finished_at, instr(finished_at, '.') + 1,
                    length(finished_at) - instr(finished_at, '.') - 1)
             || '000000000', 1, 9)
        || 'Z'
    WHEN substr(finished_at, length(finished_at), 1) = 'Z' THEN
        substr(finished_at, 1, length(finished_at) - 1) || '.000000000Z'
    ELSE finished_at
END
WHERE finished_at IS NOT NULL;
