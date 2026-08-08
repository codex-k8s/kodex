// Package httpserver предоставляет общий технический HTTP runtime.
package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/netutil"
)

// Config задаёт закрытую техническую поверхность и сетевые бюджеты.
type Config struct {
	Address            string
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	MaximumHeaderBytes int
	MaximumConnections int
}

// Readiness возвращает текущее состояние без внешнего I/O.
type Readiness interface {
	Ready() (bool, string)
}

// ExactGETRoute добавляет безопасный service-owned read-only endpoint.
type ExactGETRoute struct {
	Path        string
	ContentType string
	Handler     http.Handler
}

// Server владеет listener и стандартным http.Server без фоновых goroutine.
type Server struct {
	server             *http.Server
	listener           net.Listener
	maximumConnections int
}

// New валидирует конфигурацию и связывает стандартные и exact service-owned маршруты.
func New(config Config, readiness Readiness, metrics http.Handler, routes ...ExactGETRoute) (*Server, error) {
	if config.Address == "" || readiness == nil || metrics == nil ||
		config.ReadHeaderTimeout < time.Second || config.ReadHeaderTimeout > 10*time.Second ||
		config.ReadTimeout < config.ReadHeaderTimeout || config.ReadTimeout > 30*time.Second ||
		config.WriteTimeout < time.Second || config.WriteTimeout > 30*time.Second ||
		config.IdleTimeout < time.Second || config.IdleTimeout > 2*time.Minute ||
		config.MaximumHeaderBytes < 16<<10 || config.MaximumHeaderBytes > 256<<10 ||
		config.MaximumConnections < 16 || config.MaximumConnections > 4096 {
		return nil, errors.New("technical HTTP server configuration is invalid")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(writer http.ResponseWriter, _ *http.Request) {
		setHeaders(writer)
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		setHeaders(writer)
		ready, _ := readiness.Ready()
		if !ready {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("GET /metrics", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setHeaders(writer)
		metrics.ServeHTTP(writer, request)
	}))
	registered := map[string]struct{}{"/livez": {}, "/readyz": {}, "/metrics": {}}
	for _, route := range routes {
		if !validExactGETRoute(route, registered) {
			return nil, errors.New("technical HTTP route configuration is invalid")
		}
		registered[route.Path] = struct{}{}
		current := route
		mux.Handle("GET "+current.Path, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			setHeaders(writer)
			writer.Header().Set("Content-Type", current.ContentType)
			if request.URL.RawQuery != "" || request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			current.Handler.ServeHTTP(writer, request)
		}))
	}
	return &Server{maximumConnections: config.MaximumConnections, server: &http.Server{
		Addr:              config.Address,
		Handler:           http.MaxBytesHandler(mux, 1<<20),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaximumHeaderBytes,
	}}, nil
}

func validExactGETRoute(route ExactGETRoute, registered map[string]struct{}) bool {
	if route.Handler == nil || route.ContentType == "" || len(route.Path) < 2 || len(route.Path) > 128 ||
		!strings.HasPrefix(route.Path, "/") || strings.HasSuffix(route.Path, "/") ||
		strings.ContainsAny(route.Path, "?#{} \\") || strings.Contains(route.Path, "..") {
		return false
	}
	_, exists := registered[route.Path]
	return !exists
}

// Listen резервирует listener до startup barrier.
func (server *Server) Listen() error {
	if server == nil || server.server == nil || server.listener != nil {
		return errors.New("technical HTTP server lifecycle is invalid")
	}
	listener, err := net.Listen("tcp", server.server.Addr)
	if err != nil {
		return errors.New("listen technical HTTP server")
	}
	server.listener = netutil.LimitListener(listener, server.maximumConnections)
	return nil
}

// Serve обслуживает уже созданный listener и блокируется до остановки.
func (server *Server) Serve() error {
	if server == nil || server.listener == nil {
		return errors.New("technical HTTP server is not listening")
	}
	err := server.server.Serve(server.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown ограниченно закрывает listener и активные соединения.
func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil || server.server == nil {
		return nil
	}
	return server.server.Shutdown(ctx)
}

func setHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}
