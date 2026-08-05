package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	expectedClientCN = "mattercodex-buildkit-staging-push"
	serverError      = "registry write authorizer failed"
)

func main() {
	ca, err := os.ReadFile("/identity/client-ca.pem")
	if err != nil {
		log.Fatal(serverError)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		log.Fatal(serverError)
	}
	backend, _ := url.Parse("http://127.0.0.1:5005")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("redirect rejected")
	}}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) != 1 ||
			request.TLS.PeerCertificates[0].Subject.CommonName != expectedClientCN ||
			!allowedRegistryWrite(request.Method, request.URL.Path) {
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		target := *backend
		target.Path, target.RawQuery = request.URL.Path, request.URL.RawQuery
		upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), request.Body)
		if err != nil {
			http.Error(writer, "upstream request rejected", http.StatusBadGateway)
			return
		}
		upstream.Header = request.Header.Clone()
		response, err := client.Do(upstream)
		if err != nil {
			http.Error(writer, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, io.LimitReader(response.Body, 1<<30))
	})
	server := &http.Server{Addr: ":5001", Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}} //nolint:gosec // exact client CA and TLS 1.3.
	if err := server.ListenAndServeTLS("/identity/tls.crt", "/identity/tls.key"); err != nil {
		log.Fatal(serverError)
	}
}

func allowedRegistryWrite(method, path string) bool {
	if method == http.MethodGet && path == "/v2/" {
		return true
	}
	if method == http.MethodDelete || !strings.HasPrefix(path, "/v2/") || strings.ContainsAny(path, "\r\n\\") {
		return false
	}
	repository := ""
	for _, candidate := range []string{"staging/role-images", "staging/readiness"} {
		prefix := "/v2/" + candidate + "/"
		if strings.HasPrefix(path, prefix) {
			repository = candidate
			break
		}
	}
	if repository == "" {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodPut:
		return strings.Contains(path, "/blobs/") || strings.Contains(path, "/manifests/")
	default:
		return false
	}
}
