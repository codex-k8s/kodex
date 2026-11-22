// Package identityupload exposes only direct owner-local Kubernetes credential setup over TLS.
// No generic Secret, arbitrary URL, shell, proxy or credential read operation is available here.
// Пакет identityupload предоставляет лишь прямой owner-local Kubernetes setup через TLS.
// Generic Secret, arbitrary URL, shell, proxy и credential read здесь отсутствуют.
package identityupload

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// Handler restricts one direct TLS origin and bounded concurrent uploads to the existing owner workflow.
// The semaphore bounds memory only; authority always comes from the permit and fresh owner transaction.
// Handler ограничивает direct TLS origin и bounded concurrent uploads существующим owner workflow.
// Semaphore ограничивает лишь память; authority всегда приходит из permit и fresh owner transaction.
type Handler struct {
	service *kubernetesidentity.Service
	host    string
	slots   chan struct{}
}

// New requires an explicitly configured HTTPS origin and initialized purpose workflow.
// It does not bind a socket or infer an origin from Host, forwarded headers or caller input.
// New требует явно настроенный HTTPS origin и инициализированный purpose workflow.
// Не открывает socket и не выводит origin из Host, forwarded headers либо caller input.
func New(service *kubernetesidentity.Service, origin string) (*Handler, error) {
	endpoint, err := url.Parse(origin)
	if service == nil || err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawPath != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return nil, targetrepo.ErrSetupRejected
	}
	return &Handler{service: service, host: endpoint.Host, slots: make(chan struct{}, 8)}, nil
}

// ServeHTTP accepts one fixed upload/observe route and clears owned material after the joined workflow.
// It never redirects, echoes provider diagnostics, stores cookies or returns private material pins.
// ServeHTTP принимает один fixed upload/observe route и очищает owned material после joined workflow.
// Не делает redirects, не повторяет provider diagnostics, не хранит cookies и не выдаёт private material pins.
func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || r.Host != handler.host || r.Method != http.MethodPost || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Origin") != "" || len(r.Header.Values("Authorization")) != 1 {
		handler.problem(w, http.StatusBadRequest)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/kubernetes-identity-setups/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/v1/kubernetes-identity-setups/") || len(parts) != 2 || !strings.HasPrefix(parts[0], "ksu_") || !managedkubernetes.ValidLocator(parts[0]) || parts[1] != "upload" && parts[1] != "observe" {
		handler.problem(w, http.StatusNotFound)
		return
	}
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) != len("Bearer ")+43 {
		handler.problem(w, http.StatusUnauthorized)
		return
	}
	permit, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(authorization, "Bearer "))
	defer clear(permit)
	if err != nil || len(permit) != 32 {
		handler.problem(w, http.StatusUnauthorized)
		return
	}
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" || len(parameters) != 0 || r.ContentLength > managedkubernetes.MaximumCredentialBytes || r.Body == nil {
		handler.problem(w, http.StatusBadRequest)
		return
	}
	select {
	case handler.slots <- struct{}{}:
		defer func() { <-handler.slots }()
	default:
		handler.problem(w, http.StatusTooManyRequests)
		return
	}
	material, err := io.ReadAll(io.LimitReader(r.Body, managedkubernetes.MaximumCredentialBytes+1))
	defer clear(material)
	if err != nil || len(material) > managedkubernetes.MaximumCredentialBytes || parts[1] == "observe" && len(material) != 0 || parts[1] == "upload" && len(material) == 0 {
		handler.problem(w, http.StatusBadRequest)
		return
	}
	operation, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var result value.KubernetesIdentitySetupView
	if parts[1] == "upload" {
		result, err = handler.service.Materialize(operation, parts[0], permit, material)
	} else {
		result, err = handler.service.Observe(operation, parts[0], permit)
	}
	if err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, targetrepo.ErrSetupRejected) {
			code = http.StatusForbidden
		}
		handler.problem(w, code)
		return
	}
	if !result.Valid() || result.SetupRef != parts[0] {
		handler.problem(w, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

// problem writes one fixed safe diagnostic without upstream status bodies, permits or credential bytes.
// problem выдаёт одну fixed safe diagnostic без upstream status bodies, permits либо credential bytes.
func (*Handler) problem(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "{\"code\":\"KUBERNETES_IDENTITY_SETUP_UNAVAILABLE\"}\n")
}

// NewServer requires explicit TLS configuration before composing bounded HTTP server budgets.
// It starts no goroutine and never falls back to a plaintext listener.
// NewServer требует явный TLS config до сборки bounded HTTP server budgets.
// Не запускает goroutine и не использует fallback к plaintext listener.
func NewServer(address string, handler *Handler, config *tls.Config) (*http.Server, error) {
	if handler == nil || config == nil || config.MinVersion < tls.VersionTLS13 || len(config.Certificates) != 1 || config.InsecureSkipVerify || config.ClientAuth != tls.NoClientCert {
		return nil, targetrepo.ErrSetupRejected
	}
	return &http.Server{Addr: address, Handler: handler, TLSConfig: config.Clone(), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second,
		IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192}, nil
}
