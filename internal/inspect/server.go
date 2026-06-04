package inspect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/statejson"
)

// DefaultPort is the default TCP port for the read-only inspector (issue #109).
const DefaultPort = 8787

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 5 * time.Second
)

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
	store     *sqlite.Store
	cfg       Config
	mux       *http.ServeMux
	mu        sync.RWMutex
	boundAddr string // set after Listen; use [Server.BoundAddr] when Port is 0
}

// NewServer wires handlers for a read-only SQLite store opened via [sqlite.OpenReadOnly].
func NewServer(st *sqlite.Store, cfg Config) (*Server, error) {
	if st == nil {
		return nil, errors.New("inspect: nil store")
	}
	if strings.TrimSpace(cfg.StatePath) == "" {
		return nil, errors.New("inspect: empty state path")
	}
	if cfg.Port < 0 {
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

// Handler returns the root HTTP handler (GET/HEAD only, security headers on responses).
func (s *Server) Handler() http.Handler {
	return securityHeaders(RejectMutation(s.mux))
}

// ListenReady reports whether [Server.ListenAndServe] has bound a TCP listener.
func (s *Server) ListenReady() bool {
	s.mu.RLock()
	ok := s.boundAddr != ""
	s.mu.RUnlock()
	return ok
}

// BoundAddr returns the address the server is listening on after [Server.ListenAndServe] starts.
// When Port is 0, this is the kernel-assigned port (127.0.0.1:NNNN).
func (s *Server) BoundAddr() string {
	s.mu.RLock()
	ba := s.boundAddr
	s.mu.RUnlock()
	if ba != "" {
		return ba
	}
	return s.ListenAddr()
}

// ListenAddr returns the address this server will bind when [Server.ListenAndServe] is called.
func (s *Server) ListenAddr() string {
	if a := strings.TrimSpace(s.cfg.Addr); a != "" {
		return a
	}
	if s.cfg.Port == 0 {
		return "127.0.0.1:0"
	}
	return fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
}

// ListenAndServe blocks until the server stops or ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.ListenAddr())
	if err != nil {
		return fmt.Errorf("inspect: listen %s: %w", s.ListenAddr(), err)
	}
	s.mu.Lock()
	s.boundAddr = ln.Addr().String()
	s.mu.Unlock()

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
	limit := statejson.ParseRunListLimit(r.URL.Query().Get("limit"))

	var runs []state.Run
	var err error
	if workflow != "" {
		runs, err = s.store.ListRunsByWorkflow(ctx, workflow, limit)
	} else {
		runs, err = s.store.ListRecentRuns(ctx, limit)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to list runs")
		return
	}
	writeJSON(w, http.StatusOK, ListRunsResponse{
		StatePath: s.cfg.StatePath,
		Workflow:  workflow,
		Runs:      statejson.Runs(runs),
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := strings.TrimSpace(r.PathValue("id"))
	if runID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "missing run id")
		return
	}

	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "not_found", "run not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to load run")
		return
	}
	steps, err := s.store.ListRunStepsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to load run steps")
		return
	}
	events, err := s.store.ListTraceEventsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to load trace events")
		return
	}

	resp := RunDetailResponse{
		StatePath: s.cfg.StatePath,
		Run:       statejson.Run(*run),
		Steps:     stepsToRecords(steps),
		Events:    statejson.TraceEvents(events),
	}
	if base := strings.TrimSpace(s.cfg.TraceUIBaseURL); base != "" {
		if tid := statejson.TraceIDFromEvents(events); tid != "" {
			resp.TraceLink = base + "/" + tid
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env := strings.TrimSpace(s.cfg.Env)
	if env == "" {
		env = state.DefaultEnvironment
	}
	rows, err := s.store.ListAppliedResourcesByEnv(ctx, env)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to list applied resources")
		return
	}
	var appliedProject *statejson.AppliedProjectRecord
	pname := strings.TrimSpace(s.cfg.ProjectName)
	if pname == "" {
		pname = strings.TrimSpace(r.URL.Query().Get("project"))
	}
	if pname != "" {
		p, err := s.store.GetAppliedProject(ctx, env, pname)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to load applied project")
			return
		}
		if err == nil {
			appliedProject = statejson.AppliedProject(p)
		}
	}
	writeJSON(w, http.StatusOK, StateResponse{
		Environment:    env,
		StatePath:      s.cfg.StatePath,
		Resources:      statejson.AppliedResources(rows),
		AppliedProject: appliedProject,
	})
}

func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := strings.TrimSpace(r.URL.Query().Get("run"))
	if runID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "query parameter run is required")
		return
	}
	cps, err := s.store.ListCheckpointsByRunID(ctx, runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to list checkpoints")
		return
	}
	writeJSON(w, http.StatusOK, CheckpointsResponse{
		StatePath:   s.cfg.StatePath,
		RunID:       runID,
		Checkpoints: checkpointsToRecords(cps),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg, Code: code})
}

// RejectMutation is an http.Handler that answers non-GET/HEAD requests with 405.
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
