package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

var allowedHosts = []string{"api.openai.com", "auth.openai.com", "chatgpt.com", "github.com"}

func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(lifecycle); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "integration management egress proxy failed: %v\n", err)
		os.Exit(1)
	}
}

func run(lifecycle context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	proxy := &handler{resolver: net.DefaultResolver, dialer: &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}, semaphore: make(chan struct{}, 64), logger: logger}
	server := &http.Server{Addr: ":8080", Handler: proxy, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case <-lifecycle.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(lifecycle), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type handler struct {
	resolver  *net.Resolver
	dialer    *net.Dialer
	semaphore chan struct{}
	logger    *slog.Logger
}

func (handler *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/livez" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/readyz" {
		readyCtx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := handler.checkAllowlist(readyCtx); err != nil {
			http.Error(response, "egress allowlist is unavailable", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodConnect {
		http.Error(response, "CONNECT is required", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(request.Host)
	if err != nil || port != "443" || !slices.Contains(allowedHosts, strings.ToLower(host)) {
		http.Error(response, "egress target rejected", http.StatusForbidden)
		return
	}
	select {
	case handler.semaphore <- struct{}{}:
		defer func() { <-handler.semaphore }()
	default:
		http.Error(response, "egress capacity exceeded", http.StatusServiceUnavailable)
		return
	}
	addresses, err := handler.resolver.LookupNetIP(request.Context(), "ip", host)
	if err != nil || len(addresses) == 0 {
		http.Error(response, "egress target unavailable", http.StatusBadGateway)
		return
	}
	selected := publicAddress(addresses)
	if selected == nil {
		http.Error(response, "egress address rejected", http.StatusForbidden)
		return
	}
	upstream, err := handler.dialer.DialContext(request.Context(), "tcp", net.JoinHostPort(selected.String(), port))
	if err != nil {
		http.Error(response, "egress connection unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	hijacker, ok := response.(http.Hijacker)
	if !ok {
		http.Error(response, "egress transport unavailable", http.StatusInternalServerError)
		return
	}
	downstream, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer downstream.Close()
	if _, err = downstream.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	_ = upstream.SetDeadline(time.Now().Add(20 * time.Minute))
	_ = downstream.SetDeadline(time.Now().Add(20 * time.Minute))
	done := make(chan struct{}, 2)
	go copyTunnel(upstream, downstream, done)
	go copyTunnel(downstream, upstream, done)
	<-done
	handler.logger.Info("management egress tunnel completed", "target", host)
}

func (handler *handler) checkAllowlist(ctx context.Context) error {
	for _, host := range allowedHosts {
		addresses, err := handler.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || publicAddress(addresses) == nil {
			return errors.New("allowlisted egress target is unavailable")
		}
	}
	return nil
}

func publicAddress(addresses []netip.Addr) net.IP {
	for _, address := range addresses {
		value := net.IP(address.AsSlice())
		if !value.IsPrivate() && !value.IsLoopback() && !value.IsLinkLocalUnicast() && !value.IsUnspecified() {
			return value
		}
	}
	return nil
}

func copyTunnel(destination io.Writer, source io.Reader, done chan<- struct{}) {
	_, _ = io.Copy(destination, source)
	done <- struct{}{}
}
