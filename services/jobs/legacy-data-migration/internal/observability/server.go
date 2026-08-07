// Package observability предоставляет bounded one-shot health и metrics boundary.
package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	listener net.Listener
	server   *http.Server
	mu       sync.RWMutex
	ready    bool
	mode     string
	outcome  string
}

func Start(address, mode string) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, errors.New("listen technical endpoint")
	}
	state := &Server{listener: listener, mode: mode, outcome: "running"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", state.live)
	mux.HandleFunc("GET /health/ready", state.readiness)
	mux.HandleFunc("GET /metrics", state.metrics)
	state.server = &http.Server{
		Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second,
	}
	return state, nil
}

func (server *Server) Serve() error {
	err := server.server.Serve(server.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *Server) SetReady(value bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.ready = value
}

func (server *Server) SetOutcome(value string) {
	if value != "success" && value != "blocked" && value != "error" {
		value = "error"
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	server.outcome = value
}

func (server *Server) Shutdown(ctx context.Context) error { return server.server.Shutdown(ctx) }

func (server *Server) live(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) readiness(writer http.ResponseWriter, _ *http.Request) {
	server.mu.RLock()
	ready := server.ready
	server.mu.RUnlock()
	if !ready {
		http.Error(writer, "not ready", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) metrics(writer http.ResponseWriter, _ *http.Request) {
	server.mu.RLock()
	ready, mode, outcome := server.ready, server.mode, server.outcome
	server.mu.RUnlock()
	readyValue := 0
	if ready {
		readyValue = 1
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(writer,
		"# HELP mattercodex_legacy_data_migration_ready Whether the exact migration working path passed startup checks.\n"+
			"# TYPE mattercodex_legacy_data_migration_ready gauge\n"+
			"mattercodex_legacy_data_migration_ready %d\n"+
			"# HELP mattercodex_legacy_data_migration_runs_total Completed migration runs by bounded mode and outcome.\n"+
			"# TYPE mattercodex_legacy_data_migration_runs_total counter\n",
		readyValue,
	)
	for _, candidate := range []string{"success", "blocked", "error"} {
		value := 0
		if outcome == candidate {
			value = 1
		}
		_, _ = fmt.Fprintf(writer,
			"mattercodex_legacy_data_migration_runs_total{mode=%q,outcome=%q} %d\n",
			mode, candidate, value)
	}
}
