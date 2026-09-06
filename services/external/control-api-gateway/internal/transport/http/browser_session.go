package httptransport

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
)

func (server *Server) GetOwnerSession(w http.ResponseWriter, r *http.Request) {
	metadata, err := server.boundary.SessionMetadata(r.Context())
	if err != nil {
		writeSessionProblem(w, err)
		return
	}
	body, valid := sessionMetadataMap(metadata)
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", metadata.SessionRevision))
	writeJSON(w, http.StatusOK, body)
}

func (server *Server) BeginOwnerAuthorization(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeOptionalJSON[generated.OwnerAuthorizationInput](w, r)
	if !ok {
		return
	}
	var purpose *boundary.SessionPurpose
	fresh := false
	if body != nil {
		fresh = body.FreshAuthentication != nil && *body.FreshAuthentication
		purpose = sessionPurposeMap(body.Purpose)
	}
	authorization, cookie, err := server.boundary.BeginAuthorization(r.Context(), purpose, fresh)
	if err != nil {
		writeSessionProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{Name: boundary.LoginCookieName, Value: cookie, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: 300})
	writeJSON(w, http.StatusOK, struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}{authorization})
}

func (server *Server) CompleteOwnerAuthorization(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[generated.CompleteOwnerAuthorizationJSONRequestBody](w, r)
	if !ok {
		return
	}
	cookie, err := r.Cookie(boundary.LoginCookieName)
	if err != nil {
		writeLocalProblem(w, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	claims, encoded, csrf, metadata, err := server.boundary.CompleteAuthorization(r.Context(), cookie.Value, body.State, body.Code)
	if err != nil {
		writeSessionProblem(w, err)
		return
	}
	mapped, valid := sessionMetadataMap(metadata)
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	boundary.SetOwnerSessionCookies(w, claims, encoded, csrf)
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", metadata.SessionRevision))
	writeJSON(w, http.StatusOK, mapped)
}

func sessionPurposeMap(source *generated.OwnerSessionPurpose) *boundary.SessionPurpose {
	if source == nil {
		return nil
	}
	purpose := &boundary.SessionPurpose{Kind: string(source.Kind), ProjectRef: stringValue(source.ProjectRef), SecretRef: stringValue(source.SecretRef),
		ReceiptRef: stringValue(source.ReceiptRef), ReceiptDigest: stringValue(source.ReceiptDigest)}
	if source.ReceiptVersion != nil {
		purpose.ReceiptVersion = *source.ReceiptVersion
	}
	return purpose
}

func sessionMetadataMap(source boundary.SessionMetadata) (generated.OwnerSessionMetadata, bool) {
	generation, err := uuid.Parse(source.Generation)
	if err != nil || source.Version < 1 || source.Version > 1<<53-1 || source.SessionRevision < 1 || source.SessionRevision > 1<<53-1 ||
		source.ServerTime.IsZero() || source.AccessExpiresAt.IsZero() || source.AbsoluteExpiresAt.IsZero() || source.ExpiresAt.IsZero() || source.RenewAfter.IsZero() ||
		!source.ExpiresAt.After(source.ServerTime) || source.ExpiresAt.After(source.AbsoluteExpiresAt) || source.RenewAfter.After(source.ExpiresAt) {
		return generated.OwnerSessionMetadata{}, false
	}
	mode := generated.OwnerSessionMetadataRenewalMode("REAUTHENTICATION")
	if source.BackendRefresh {
		mode = generated.OwnerSessionMetadataRenewalMode("BACKEND_REFRESH")
	}
	return generated.OwnerSessionMetadata{Generation: generation, Version: int64(source.Version), SessionRevision: int64(source.SessionRevision),
		ServerTime: source.ServerTime, ExpiresAt: source.ExpiresAt, AccessExpiresAt: source.AccessExpiresAt,
		AbsoluteExpiresAt: source.AbsoluteExpiresAt, RenewAfter: source.RenewAfter, RenewalMode: mode}, true
}

func writeSessionProblem(w http.ResponseWriter, err error) {
	if errors.Is(err, boundary.ErrSessionValidationUnavailable) || errors.Is(err, browserstate.ErrUnavailable) ||
		errors.Is(err, browserstate.ErrConflict) || errors.Is(err, session.ErrRenewalPending) {
		w.Header().Set("Retry-After", "1")
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	if errors.Is(err, boundary.ErrRateLimited) {
		w.Header().Set("Retry-After", "1")
		writeLocalProblem(w, http.StatusTooManyRequests, "RATE_LIMITED", true)
		return
	}
	writeLocalProblem(w, http.StatusUnauthorized, "UNAUTHENTICATED", false)
}
