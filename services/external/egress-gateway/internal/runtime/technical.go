package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"golang.org/x/net/netutil"
)

type technicalServer struct {
	server   *http.Server
	listener net.Listener
}

func newTechnicalServer(address string, state *state, metrics *observability.Metrics) *technicalServer {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(writer http.ResponseWriter, _ *http.Request) {
		setTechnicalHeaders(writer, "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		setTechnicalHeaders(writer, "text/plain; charset=utf-8")
		ready := state.ready()
		metrics.SetReady(ready)
		if !ready {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /policy", func(writer http.ResponseWriter, request *http.Request) {
		setTechnicalHeaders(writer, "application/json")
		if request.URL.RawQuery != "" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(writer).Encode(state.readback()); err != nil {
			return
		}
	})
	mux.Handle("GET /metrics", metrics.Handler())
	return &technicalServer{server: &http.Server{
		Addr: address, Handler: http.MaxBytesHandler(mux, 1<<20), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}}
}

func (server *technicalServer) listen() error {
	listener, err := net.Listen("tcp", server.server.Addr)
	if err != nil {
		return errors.New("listen technical server")
	}
	server.listener = netutil.LimitListener(listener, 64)
	return nil
}

func (server *technicalServer) serve() error {
	if server.listener == nil {
		return errors.New("technical server is not listening")
	}
	err := server.server.Serve(server.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *technicalServer) shutdown(ctx context.Context) error {
	return server.server.Shutdown(ctx)
}

func setTechnicalHeaders(writer http.ResponseWriter, contentType string) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}
