// Package httptransport owns the direct purpose-specific HTTPS upload boundary; private material never traverses the public control API or a generic secret endpoint.
// Package httptransport владеет direct purpose-specific HTTPS upload boundary; private material не проходит public control API либо generic secret endpoint.
package httptransport

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
)

const (
	// sshUploadPath fixes the single private-key purpose route; neither a generic object path nor a caller-selected destination is accepted.
	// sshUploadPath фиксирует единственный private-key purpose route; generic object path и caller-selected destination запрещены.
	sshUploadPath = "/v1/ssh-identities/"
	// sshUploadRejected is the fixed transport diagnostic; request bytes, permits and dependency errors are never interpolated into it.
	// sshUploadRejected задаёт fixed transport diagnostic без request bytes, permits либо dependency errors.
	sshUploadRejected = `{"outcome":"REJECTED"}`
	// sshUploadUnavailable preserves an unreported owner completion without granting retry or observation credit to the browser.
	// sshUploadUnavailable сохраняет unreported owner completion без выдачи browser retry либо observation credit.
	sshUploadUnavailable = `{"outcome":"UNAVAILABLE","reason":"OWNER_READ_REQUIRED"}`
	// sshUploadMaximumBytes matches the owner's content precondition, broker parser and ephemeral UI input bound.
	// sshUploadMaximumBytes совпадает с owner content precondition, broker parser и ephemeral UI input bound.
	sshUploadMaximumBytes = 16384
)

// SSHIdentityUpload serves one exact TLS origin and one configured browser origin, while CLI callers still require the same opaque owner-issued permit and current domain admission.
// SSHIdentityUpload обслуживает один exact TLS origin и один configured browser origin; CLI caller также требует opaque owner-issued permit и current domain admission.
type SSHIdentityUpload struct {
	service                         *sshidentity.Service
	endpoint                        managedssh.IdentityUploadEndpoint
	host, browserOrigin, serverName string
	logger                          *slog.Logger
}

// NewSSHIdentityUpload validates trusted route metadata only; readiness and permission still depend on the existing owner Resolve/Claim/Complete path for every request.
// NewSSHIdentityUpload проверяет лишь trusted route metadata; readiness и permission зависят от existing owner Resolve/Claim/Complete path каждого request.
func NewSSHIdentityUpload(service *sshidentity.Service, endpoint managedssh.IdentityUploadEndpoint, browserOrigin string, logger *slog.Logger) (*SSHIdentityUpload, error) {
	origin, err := url.Parse(browserOrigin)
	if service == nil || logger == nil || !endpoint.Valid() || err != nil || !(managedssh.IdentityUploadEndpoint{Origin: browserOrigin, TLSServerName: origin.Hostname()}).Valid() {
		return nil, managedssh.ErrIdentityUploadEndpointInvalid
	}
	upload, _ := url.Parse(endpoint.Origin)
	serverName := endpoint.TLSServerName
	if net.ParseIP(serverName) != nil {
		serverName = ""
	} // TLS для literal IP не передаёт расширение SNI.
	return &SSHIdentityUpload{service: service, endpoint: endpoint, host: upload.Host, browserOrigin: browserOrigin, serverName: serverName, logger: logger}, nil
}

// ServeHTTP rejects transport aliases and cross-origin access before owner I/O, snapshots at most sixteen KiB of raw key bytes, clears its buffer after the joined service call and never retries a material effect.
// ServeHTTP отклоняет transport aliases и cross-origin access до owner I/O, фиксирует максимум sixteen KiB raw key bytes, очищает buffer после joined service call и не повторяет material effect.
func (handler *SSHIdentityUpload) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Origin")
	if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || r.TLS.ServerName != handler.serverName || r.Host != handler.host || r.URL.IsAbs() || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Origin")) > 1 || len(r.Header.Values("Cookie")) != 0 {
		sshUploadResponse(w, http.StatusForbidden, sshUploadRejected)
		return
	}
	origin := r.Header.Get("Origin")
	if len(r.Header.Values("Origin")) > 0 && origin == "" || origin != "" && origin != handler.browserOrigin {
		sshUploadResponse(w, http.StatusForbidden, sshUploadRejected)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, sshUploadPath), "/")
	if !strings.HasPrefix(r.URL.Path, sshUploadPath) || len(parts) != 2 || !strings.HasPrefix(parts[0], "ssu_") || !managedssh.OwnerReference(parts[0]) || (parts[1] != "material" && parts[1] != "observe") {
		sshUploadResponse(w, http.StatusNotFound, sshUploadRejected)
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	if r.Method == http.MethodOptions {
		if origin == "" || len(r.Header.Values("Access-Control-Request-Method")) != 1 || r.Header.Get("Access-Control-Request-Method") != http.MethodPost || !sshUploadPreflightHeaders(r.Header.Values("Access-Control-Request-Headers")) || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			sshUploadResponse(w, http.StatusForbidden, sshUploadRejected)
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
		w.Header().Set("Access-Control-Allow-Headers", "authorization, content-type")
		w.Header().Set("Access-Control-Max-Age", "0")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		sshUploadResponse(w, http.StatusMethodNotAllowed, sshUploadRejected)
		return
	}
	if len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("Content-Encoding")) != 0 || len(r.Trailer) != 0 || len(r.Header.Values("Content-Type")) > 1 {
		sshUploadResponse(w, http.StatusBadRequest, sshUploadRejected)
		return
	}
	token := r.Header.Get("Authorization")
	if !strings.HasPrefix(token, "Bearer ") || len(token) != 50 {
		sshUploadResponse(w, http.StatusUnauthorized, sshUploadRejected)
		return
	}
	permit, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, "Bearer "))
	if err != nil || len(permit) != 32 {
		clear(permit)
		sshUploadResponse(w, http.StatusUnauthorized, sshUploadRejected)
		return
	}
	defer clear(permit)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	operation := "materialize"
	if parts[1] == "observe" {
		operation = "observe"
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Type") != "" {
			sshUploadResponse(w, http.StatusBadRequest, sshUploadRejected)
			return
		}
		err = handler.service.Observe(ctx, parts[0], permit)
	} else {
		if r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength == 0 || r.ContentLength > sshUploadMaximumBytes {
			sshUploadResponse(w, http.StatusBadRequest, sshUploadRejected)
			return
		}
		if http.NewResponseController(w).SetReadDeadline(time.Now().Add(10*time.Second)) != nil {
			handler.unavailable(w, ctx, operation)
			return
		}
		body := http.MaxBytesReader(w, r.Body, sshUploadMaximumBytes)
		material, readErr := io.ReadAll(body)
		_ = body.Close()
		defer clear(material)
		if readErr != nil || len(material) == 0 || len(r.Trailer) != 0 {
			sshUploadResponse(w, http.StatusBadRequest, sshUploadRejected)
			return
		}
		err = handler.service.Materialize(ctx, parts[0], permit, material)
	}
	switch {
	case err == nil:
		sshUploadResponse(w, http.StatusOK, `{"outcome":"EVIDENCE_RECORDED","reason":"OWNER_READ_REQUIRED"}`)
	case errors.Is(err, sshrepository.ErrIdentityRejected):
		sshUploadResponse(w, http.StatusForbidden, sshUploadRejected)
	case errors.Is(err, sshrepository.ErrIdentityUnknown):
		sshUploadResponse(w, http.StatusAccepted, `{"outcome":"STORAGE_UNKNOWN","reason":"OWNER_READ_REQUIRED"}`)
	case errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected):
		sshUploadResponse(w, http.StatusConflict, `{"outcome":"ACK_REJECTED","reason":"OWNER_READ_REQUIRED"}`)
	default:
		handler.unavailable(w, ctx, operation)
	}
}

// sshUploadPreflightHeaders accepts only the two purpose headers and rejects duplicate, unknown or oversized CORS declarations without reflecting them.
// sshUploadPreflightHeaders допускает лишь два purpose headers и отклоняет duplicate, unknown либо oversized CORS declarations без их отражения.
func sshUploadPreflightHeaders(values []string) bool {
	if len(values) != 1 || len(values[0]) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range strings.Split(values[0], ",") {
		header := strings.ToLower(strings.TrimSpace(value))
		if (header != "authorization" && header != "content-type") || seen[header] {
			return false
		}
		seen[header] = true
	}
	return seen["authorization"]
}

// sshUploadResponse reports only receipt classification, never current setup state or attachment; authoritative state must be reread from CP.
// sshUploadResponse сообщает лишь receipt classification без current setup state либо attachment; authoritative state повторно читается из CP.
// It emits only fixed closed metadata and no credential, owner locator or provider diagnostics.
// Ответ содержит лишь fixed closed metadata без credential, owner locator либо provider diagnostics.
func sshUploadResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body+"\n")
}

// unavailable logs one closed unexpected-failure class without formatting the underlying dependency error or any request metadata.
// unavailable записывает один closed unexpected-failure class без форматирования underlying dependency error либо request metadata.
func (handler *SSHIdentityUpload) unavailable(w http.ResponseWriter, ctx context.Context, operation string) {
	handler.logger.ErrorContext(ctx, "SSH identity upload failed", "operation", operation, "error_class", "unavailable")
	sshUploadResponse(w, http.StatusServiceUnavailable, sshUploadUnavailable)
}
