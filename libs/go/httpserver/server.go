// Package httpserver owns bounded technical HTTP listeners, standard health and
// metrics routes, and exact service-owned read routes; it contains no business API.
// Пакет httpserver управляет ограниченными техническими HTTP-listener, маршрутами
// здоровья/метрик и точными служебными read-маршрутами, не реализуя бизнесовый API.
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

// Config sets the listen address and bounded header/read/write/idle, header-size
// and connection budgets. New validates all limits before reserving a listener.
// Config задаёт адрес и пределы чтения заголовков/запроса, записи, простоя,
// размера заголовков и числа соединений. New проверяет их до создания listener.
type Config struct {
	Address            string
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	MaximumHeaderBytes int
	MaximumConnections int
}

// StandardProcessConfig returns the identical bounded technical-listener budgets shared by
// small gateway and worker processes; callers still choose the address and own Listen/Serve/Shutdown.
// RU: StandardProcessConfig возвращает одинаковые ограниченные бюджеты технического listener для
// небольших gateway и workers; адрес и жизненный цикл Listen/Serve/Shutdown остаются у вызывающего.
func StandardProcessConfig(address string) Config {
	return Config{
		Address: address, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaximumHeaderBytes: 32 << 10, MaximumConnections: 128,
	}
}

// Readiness exposes a precomputed local snapshot; a Kubernetes probe must not
// trigger external I/O or recompute dependency health through this interface.
// Readiness предоставляет готовый локальный снимок; Kubernetes probe не должен
// запускать внешнее I/O или заново вычислять здоровье зависимостей через этот порт.
type Readiness interface {
	// Ready returns admission readiness and a diagnostic reason from that snapshot;
	// the HTTP response uses only the boolean and does not expose the reason.
	// Ready возвращает готовность и диагностическую причину из снимка; HTTP-ответ
	// использует только boolean и не раскрывает причину клиенту.
	Ready() (bool, string)
}

// ExactGETRoute describes a unique exact-path GET handler owned by one service.
// New rejects collisions and requests with query, body or transfer encoding;
// the handler remains responsible for its own application authorization.
// ExactGETRoute описывает уникальный точный GET-маршрут конкретного сервиса.
// New запрещает коллизии и query/body/transfer encoding; сам handler по-прежнему
// отвечает за необходимую прикладную авторизацию.
type ExactGETRoute struct {
	Path        string
	ContentType string
	Handler     http.Handler
}

// Server owns the listener and http.Server without starting hidden goroutines.
// Callers sequence Listen, their startup barrier, Serve and bounded Shutdown.
// Server владеет listener и http.Server, не создавая скрытых goroutine.
// Вызывающий код управляет Listen, startup barrier, Serve и bounded Shutdown.
type Server struct {
	server             *http.Server
	listener           net.Listener
	maximumConnections int
}

// New validates budgets and dependencies, registers health/live/ready/metrics and
// unique exact GET routes, and wraps the mux with a one-MiB body limit. It opens
// no socket; Listen reserves the address separately before the startup barrier.
// New проверяет бюджеты и зависимости, регистрирует health/live/ready/metrics
// и уникальные точные GET-маршруты, ограничивая тело одним МиБ. Сокет не открыт:
// Listen отдельно резервирует адрес перед startup barrier.
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
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		setHeaders(writer)
		writer.WriteHeader(http.StatusNoContent)
	})
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
	registered := map[string]struct{}{"/healthz": {}, "/livez": {}, "/readyz": {}, "/metrics": {}}
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

// validExactGETRoute requires a handler, content type and bounded exact path with
// no wildcard/query/fragment/traversal syntax or collision with a registered route.
// validExactGETRoute требует handler, content type и ограниченный точный путь
// без wildcard/query/fragment/traversal и без коллизии с зарегистрированным путём.
func validExactGETRoute(route ExactGETRoute, registered map[string]struct{}) bool {
	if route.Handler == nil || route.ContentType == "" || len(route.Path) < 2 || len(route.Path) > 128 ||
		!strings.HasPrefix(route.Path, "/") || strings.HasSuffix(route.Path, "/") ||
		strings.ContainsAny(route.Path, "?#{} \\") || strings.Contains(route.Path, "..") {
		return false
	}
	_, exists := registered[route.Path]
	return !exists
}

// Listen reserves a TCP listener once and limits concurrent connections. Invalid
// lifecycle or bind failure returns a safe error without starting Serve.
// Listen один раз резервирует TCP-listener и ограничивает число соединений.
// Неверный lifecycle или ошибка bind возвращает безопасную ошибку без Serve.
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

// Serve blocks on the existing listener, treating ordinary http.Server shutdown
// as success while preserving other server errors for its caller.
// Serve блокируется на существующем listener, считая штатное закрытие http.Server
// успехом и возвращая остальные серверные ошибки вызывающему коду.
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

// Shutdown drains the underlying server within the caller's context; a nil or
// uninitialized receiver is a no-op suitable for partial-startup cleanup.
// Shutdown завершает работу сервера в пределах контекста вызывающего кода;
// nil или неинициализированный получатель допускает очистку частичного startup.
func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil || server.server == nil {
		return nil
	}
	return server.server.Shutdown(ctx)
}

// setHeaders disables storage and MIME sniffing and establishes the default
// plain-text content type, which an exact service route may explicitly replace.
// setHeaders запрещает кеширование и MIME sniffing и задаёт text/plain;
// точный служебный маршрут может явно заменить этот Content-Type.
func setHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}
