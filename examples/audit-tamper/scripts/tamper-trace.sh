#!/usr/bin/env bash
# Plant an edit in one trace_events.data_json row without updating hash (issue #169).
# Prefers python3 (stdlib sqlite3). Falls back to the sqlite3 CLI.
set -euo pipefail

usage() {
  echo "usage: tamper-trace.sh --state <sqlite-db> --run <run-id>" >&2
  exit 2
}

STATE=""
RUN=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --state)
      STATE="${2:-}"
      shift 2
      ;;
    --run)
      RUN="${2:-}"
      shift 2
      ;;
    -h | --help)
      usage
      ;;
    *)
      usage
      ;;
  esac
done

if [[ -z "$STATE" || -z "$RUN" ]]; then
  usage
fi
if [[ ! -f "$STATE" ]]; then
  echo "tamper-trace: state db not found: $STATE" >&2
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  python3 - "$STATE" "$RUN" <<'PY'
import sqlite3
import sys

state, run_id = sys.argv[1], sys.argv[2]
con = sqlite3.connect(state)
try:
    cur = con.cursor()
    cur.execute(
        "SELECT seq FROM trace_events WHERE run_id = ? ORDER BY seq ASC LIMIT 1",
        (run_id,),
    )
    row = cur.fetchone()
    if row is None:
        sys.stderr.write(f"tamper-trace: no trace_events for run {run_id!r}\n")
        sys.exit(1)
    seq = row[0]
    cur.execute(
        "UPDATE trace_events SET data_json = ? WHERE run_id = ? AND seq = ?",
        ('{"tampered":true}', run_id, seq),
    )
    if cur.rowcount != 1:
        sys.stderr.write(f"tamper-trace: expected 1 row updated, got {cur.rowcount}\n")
        sys.exit(1)
    con.commit()
    print(f"tampered run {run_id} seq {seq} (data_json only; hash unchanged)")
finally:
    con.close()
PY
  exit 0
fi

if command -v sqlite3 >/dev/null 2>&1; then
  seq="$(sqlite3 "$STATE" "SELECT seq FROM trace_events WHERE run_id = '${RUN//\'/\'\'}' ORDER BY seq ASC LIMIT 1")"
  if [[ -z "$seq" ]]; then
    echo "tamper-trace: no trace_events for run $RUN" >&2
    exit 1
  fi
  sqlite3 "$STATE" "UPDATE trace_events SET data_json = '{\"tampered\":true}' WHERE run_id = '${RUN//\'/\'\'}' AND seq = ${seq}"
  echo "tampered run $RUN seq $seq (data_json only; hash unchanged)"
  exit 0
fi

echo "tamper-trace: need python3 or sqlite3 on PATH" >&2
exit 1
