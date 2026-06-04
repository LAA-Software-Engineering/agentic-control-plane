package inspect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

// DefaultPort is the default TCP port for the read-only inspector (issue #109).
const DefaultPort = 8787

// Config holds inspector server settings.
type Config struct {
	// Addr is the listen address (e.g. "127.0.0.1:8787"). When empty, localhost:Port is used.
	Addr string
	// Port is used when Addr is empty.
	Port int
	// StatePath is the absolute SQLite path shown in API payloads.
	StatePath string
	// Env scopes deployment state listing (applied_resources).
	Env string
	// ProjectName, when set, includes applied_projects in /api/state when present.
	ProjectName string
	// TraceUIBaseURL, when set, enables deep links for runs that carry an OTel trace_id (issue #108).
	TraceUIBaseURL string
}

// Server serves read-only JSON and static UI over HTTP.
type Server struct {
	store *sqlite.Store
	cfg   Config
	mux   *http.ServeMux
}

// NewServer wires handlers for a read-only SQLite store opened via [sqlite.OpenReadOnly].
func NewServer(st *sqlite.Store, cfg Config) (*Server, error) {
	if st == nil {
		return nil, errors.New("inspect: nil store")
	}
	if strings.TrimSpace(cfg.StatePath) == "" {
		return nil, errors.New("inspect: empty state path")
	}
	if cfg.Port <= 0 {
		cfg.Port = DefaultPort
	}
	s := &Server{store: st, cfg: cfg, mux: http.NewServeMux()}
	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/runs", s.handleListRuns)
	s.mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /api/checkpoints", s.handleCheckpoints)
	s.mux.Handle("GET /{$}", s.staticHandler("index.html"))
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticFS()))
}

// Handler returns the root HTTP handler (read-only routes only).
func (s *Server) Handler() http.Handler {
	return s.mux
}

// ListenAddr returns the address this server will bind when [Server.ListenAndServe] is called.
func (s *Server) ListenAddr() string {
	if a := strings.TrimSpace(s.cfg.Addr); a != "" {
		return a
	}
	return fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
}

// ListenAndServe blocks until the server stops or ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.ListenAddr())
	if err != nil {
		return fmt.Errorf("inspect: listen %s: %w", s.ListenAddr(), err)
	}

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflow := strings.TrimSpace(r.URL.Query().Get("workflow"))
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	var runs []state.Run
	var err error
	if workflow != "" {
		runs, err = s.store.ListRunsByWorkflow(ctx, workflow, limit)
	} else {
		runs, err = s.store.ListRecentRuns(ctx, limit)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"statePath": s.cfg.StatePath,
		"workflow":  workflow,
		"runs":      runsToRecords(runs),
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := strings.TrimSpace(r.PathValue("id"))
	if runID == "" {
		writeAPIError(w, http.StatusBadRequest, errors.New("missing run id"))
		return
	}

	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, fmt.Errorf("unknown run %q", runID))
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	steps, err := s.store.ListRunStepsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	events, err := s.store.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}

	payload := map[string]any{
		"statePath": s.cfg.StatePath,
		"run":       runToRecord(*run),
		"steps":     stepsToRecords(steps),
		"events":    traceEventsToRecords(events),
	}
	if base := strings.TrimSpace(s.cfg.TraceUIBaseURL); base != "" {
		if tid := traceIDFromEvents(events); tid != "" {
			payload["traceLink"] = strings.TrimRight(base, "/") + "/" + tid
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env := strings.TrimSpace(s.cfg.Env)
	if env == "" {
		env = "local"
	}
	rows, err := s.store.ListAppliedResourcesByEnv(ctx, env)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	var appliedProject any
	pname := strings.TrimSpace(s.cfg.ProjectName)
	if pname == "" {
		pname = strings.TrimSpace(r.URL.Query().Get("project"))
	}
	if pname != "" {
		p, err := s.store.GetAppliedProject(ctx, env, pname)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		if err == nil {
			appliedProject = map[string]any{
				"projectName": p.ProjectName,
				"env":         p.Env,
				"version":     p.Version,
				"appliedAt":   p.AppliedAt.UTC().Format(time.RFC3339Nano),
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"environment":    env,
		"statePath":      s.cfg.StatePath,
		"resources":      appliedResourcesToRecords(rows),
		"appliedProject": appliedProject,
	})
}

func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := strings.TrimSpace(r.URL.Query().Get("run"))
	if runID == "" {
		writeAPIError(w, http.StatusBadRequest, errors.New("query parameter run is required"))
		return
	}
	cps, err := s.store.ListCheckpointsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"statePath":   s.cfg.StatePath,
		"runId":       runID,
		"checkpoints": checkpointsToRecords(cps),
	})
}

func parseLimit(raw string, defaultLimit int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > 500 {
		return 500
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// RejectMutation is an http.Handler that answers non-GET requests with 405.
// Wrap the inspector handler in tests to assert no accidental mutation routes exist.
func RejectMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
