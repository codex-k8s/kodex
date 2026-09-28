package integrationfixture

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress     = ":8080"
	listenAddressEnv         = "KODEX_INTEGRATION_SYNTHETIC_LISTEN_ADDRESS"
	tlsListenAddressEnv      = "KODEX_INTEGRATION_SYNTHETIC_TLS_LISTEN_ADDRESS"
	tlsCertificateFileEnv    = "KODEX_INTEGRATION_SYNTHETIC_TLS_CERTIFICATE_FILE"
	tlsPrivateKeyFileEnv     = "KODEX_INTEGRATION_SYNTHETIC_TLS_PRIVATE_KEY_FILE"
	bearerTokenSHA256Env     = "KODEX_INTEGRATION_SYNTHETIC_BEARER_SHA256"
	fixtureShutdownTimeout   = 5 * time.Second
	fixtureReadHeaderTimeout = 2 * time.Second
)

type tlsServerConfig struct {
	listenAddress, certificateFile, privateKeyFile string
	bearerDigest                                   [sha256.Size]byte
}

func Run(lifecycleCtx, shutdownBaseCtx context.Context, output io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	store := NewStore()
	handler := NewHandler(store)
	listenAddress, err := configuredListenAddress()
	if err != nil {
		return err
	}
	tlsConfig, err := configuredTLSServer()
	if err != nil {
		return err
	}
	server := newFixtureServer(listenAddress, handler, logger)
	listener, err := (&net.ListenConfig{}).Listen(lifecycleCtx, "tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen for synthetic integration requests: %w", err)
	}
	servers := []*http.Server{server}
	listeners := []net.Listener{listener}
	handlers := []*Handler{handler}
	if tlsConfig != nil {
		certificate, loadErr := tls.LoadX509KeyPair(tlsConfig.certificateFile, tlsConfig.privateKeyFile)
		if loadErr != nil {
			_ = listener.Close()
			return fmt.Errorf("load synthetic integration TLS identity: %w", loadErr)
		}
		tlsListener, listenErr := (&net.ListenConfig{}).Listen(lifecycleCtx, "tcp", tlsConfig.listenAddress)
		if listenErr != nil {
			_ = listener.Close()
			return fmt.Errorf("listen for synthetic OpenAPI requests: %w", listenErr)
		}
		tlsHandler := NewBearerHandler(store, tlsConfig.bearerDigest)
		tlsServer := newFixtureServer(tlsConfig.listenAddress, tlsHandler, logger)
		servers = append(servers, tlsServer)
		listeners = append(listeners, tls.NewListener(tlsListener, &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
		}))
		handlers = append(handlers, tlsHandler)
	}
	for _, current := range handlers {
		current.SetReady(true)
	}
	logger.InfoContext(
		lifecycleCtx,
		"integration synthetic fixture started",
		"listeners", len(listeners),
		"address", listener.Addr().String(),
	)
	serveResult := make(chan error, len(servers))
	for index := range servers {
		currentServer, currentListener := servers[index], listeners[index]
		go func() { serveResult <- currentServer.Serve(currentListener) }()
	}

	var serveErr error
	select {
	case <-lifecycleCtx.Done():
	case serveErr = <-serveResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	for _, current := range handlers {
		current.SetReady(false)
	}
	shutdownErr := shutdownFixtureServers(shutdownBaseCtx, servers)
	logger.InfoContext(shutdownBaseCtx, "integration synthetic fixture stopped")
	return errors.Join(serveErr, shutdownErr)
}

func newFixtureServer(address string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: fixtureReadHeaderTimeout,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func shutdownFixtureServers(base context.Context, servers []*http.Server) error {
	shutdownCtx, cancelShutdown := context.WithTimeout(base, fixtureShutdownTimeout)
	defer cancelShutdown()
	errorsByServer := make(chan error, len(servers))
	for _, server := range servers {
		current := server
		go func() {
			err := current.Shutdown(shutdownCtx)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errorsByServer <- err
		}()
	}
	var result error
	for range servers {
		result = errors.Join(result, <-errorsByServer)
	}
	return result
}

func configuredListenAddress() (string, error) {
	address := os.Getenv(listenAddressEnv)
	if address == "" {
		return defaultListenAddress, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return "", errors.New("synthetic integration listen address is invalid")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || (portNumber != 0 && portNumber < 1024) || portNumber > 65535 {
		return "", errors.New("synthetic integration listen port is invalid")
	}
	return address, nil
}

func configuredTLSServer() (*tlsServerConfig, error) {
	values := []string{
		os.Getenv(tlsListenAddressEnv),
		os.Getenv(tlsCertificateFileEnv),
		os.Getenv(tlsPrivateKeyFileEnv),
		os.Getenv(bearerTokenSHA256Env),
	}
	nonEmpty := 0
	for _, value := range values {
		if value != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		return nil, nil
	}
	if nonEmpty != len(values) {
		return nil, errors.New("synthetic integration TLS configuration is incomplete")
	}
	address := values[0]
	host, port, err := net.SplitHostPort(address)
	if err != nil || (host != "" && host != "127.0.0.1") {
		return nil, errors.New("synthetic integration TLS listen address is invalid")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1024 || portNumber > 65535 {
		return nil, errors.New("synthetic integration TLS listen port is invalid")
	}
	for _, path := range values[1:3] {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("synthetic integration TLS path is invalid")
		}
	}
	if values[3] != strings.ToLower(values[3]) || len(values[3]) != sha256.Size*2 {
		return nil, errors.New("synthetic integration bearer digest is invalid")
	}
	decoded, err := hex.DecodeString(values[3])
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("synthetic integration bearer digest is invalid")
	}
	config := &tlsServerConfig{listenAddress: address, certificateFile: values[1], privateKeyFile: values[2]}
	copy(config.bearerDigest[:], decoded)
	return config, nil
}
