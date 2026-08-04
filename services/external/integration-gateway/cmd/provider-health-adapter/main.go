package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	maximumRequestBytes  = 64 << 10
	expectedProxyDNSName = "integration-egress-proxy-client.mattercodex-system.svc.cluster.local"
)

type configuration struct {
	listen          string
	certificateFile string
	privateKeyFile  string
	clientCAFile    string
	credentialFile  string
}

func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cleanupBase := context.Background()
	if err := run(lifecycle, cleanupBase, configuration{
		listen: envOr("PROVIDER_HEALTH_ADAPTER_LISTEN", ":8443"),
		certificateFile: envOr("PROVIDER_HEALTH_ADAPTER_TLS_CERTIFICATE_FILE",
			"/var/run/secrets/mattercodex/provider-health-adapter/tls/tls.crt"),
		privateKeyFile: envOr("PROVIDER_HEALTH_ADAPTER_TLS_PRIVATE_KEY_FILE",
			"/var/run/secrets/mattercodex/provider-health-adapter/tls/tls.key"),
		clientCAFile: envOr("PROVIDER_HEALTH_ADAPTER_CLIENT_CA_FILE",
			"/var/run/config/mattercodex/provider-health-adapter/client-ca/ca.pem"),
		credentialFile: envOr("PROVIDER_HEALTH_ADAPTER_CREDENTIAL_FILE",
			"/var/run/secrets/mattercodex/provider-health-adapter/credentials/token"),
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "provider health adapter failed: %v\n", err)
		os.Exit(1)
	}
}

func run(lifecycle, cleanupBase context.Context, config configuration) error {
	if _, _, err := net.SplitHostPort(config.listen); err != nil {
		return errors.New("provider health adapter listen address is invalid")
	}
	for _, path := range []string{config.certificateFile, config.privateKeyFile, config.clientCAFile, config.credentialFile} {
		if !filepath.IsAbs(path) {
			return errors.New("provider health adapter path is invalid")
		}
	}
	certificate, err := tls.LoadX509KeyPair(config.certificateFile, config.privateKeyFile)
	if err != nil {
		return errors.New("load provider health adapter certificate")
	}
	clientCARaw, err := os.ReadFile(config.clientCAFile)
	if err != nil || len(clientCARaw) == 0 || len(clientCARaw) > 1<<20 {
		return errors.New("read provider health adapter client CA")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCARaw) {
		return errors.New("parse provider health adapter client CA")
	}
	listener, err := net.Listen("tcp", config.listen)
	if err != nil {
		return errors.New("bind provider health adapter listener")
	}
	defer listener.Close()
	tlsListener := tls.NewListener(listener, &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || state.PeerCertificates[0].VerifyHostname(expectedProxyDNSName) != nil {
				return errors.New("provider health adapter client identity is invalid")
			}
			return nil
		},
	})
	handler := &adapter{credentialFile: config.credentialFile}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", handler.ready)
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /validate", handler.validate)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(tlsListener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-lifecycle.Done():
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(cleanupBase), 5*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(cleanup)
		serveErr := <-serveResult
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(shutdownErr, serveErr)
	}
}

type adapter struct{ credentialFile string }

func (adapter *adapter) ready(response http.ResponseWriter, _ *http.Request) {
	if _, err := adapter.credential(); err != nil {
		http.Error(response, "provider credential is unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (adapter *adapter) health(response http.ResponseWriter, request *http.Request) {
	credential, err := adapter.credential()
	if err != nil {
		http.Error(response, "provider credential is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !sameSecret(request.Header.Get("X-Provider-Token"), credential) {
		response.Header().Set("X-MatterCodex-Effect-Outcome", "NO_EFFECT")
		http.Error(response, "provider credential is invalid", http.StatusUnauthorized)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(response, "{\"status\":\"ok\"}\n")
}

func (adapter *adapter) validate(response http.ResponseWriter, request *http.Request) {
	body := http.MaxBytesReader(response, request.Body, maximumRequestBytes)
	defer body.Close()
	var value struct {
		Credentials map[string]string `json:"credentials"`
	}
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	var trailing any
	credential, credentialErr := adapter.credential()
	if decoder.Decode(&value) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) || credentialErr != nil ||
		len(value.Credentials) != 1 || !sameSecret(value.Credentials["provider-health"], credential) {
		response.Header().Set("X-MatterCodex-Effect-Outcome", "NO_EFFECT")
		http.Error(response, "provider credential is invalid", http.StatusUnauthorized)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (adapter *adapter) credential() (string, error) {
	info, err := os.Stat(adapter.credentialFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > maximumRequestBytes || info.Mode().Perm()&0o007 != 0 {
		return "", errors.New("provider credential file is unsafe")
	}
	raw, err := os.ReadFile(adapter.credentialFile)
	value := string(raw)
	if err != nil || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("provider credential is invalid")
	}
	return value, nil
}

func sameSecret(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
