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

// GetOwnerSession reads boundary-verified session metadata, validates the public representation
// and returns no-store JSON with the authoritative session-revision ETag. It never exposes bearer
// or refresh credentials.
// GetOwnerSession читает проверенные boundary метаданные сессии, валидирует публичное
// представление и возвращает no-store JSON с ETag авторитетной session revision. Bearer и refresh
// credentials не выдаются.
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

// CreateOwnerSessionTicket delegates WebSocket ticket issuance to the session boundary and returns
// the short-lived ticket with expiry under no-store. Boundary errors use the shared safe session
// error mapping.
// CreateOwnerSessionTicket поручает выдачу WebSocket ticket границе сессии и возвращает
// краткоживущий ticket со сроком под no-store. Ошибки boundary проходят общее безопасное
// отображение ошибок сессии.
func (server *Server) CreateOwnerSessionTicket(w http.ResponseWriter, r *http.Request, _ generated.CreateOwnerSessionTicketParams) {
	ticket, expires, err := server.boundary.IssueWebSocketTicket(r.Context())
	if err != nil {
		writeSessionProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, generated.OwnerSessionTicket{Ticket: ticket, ExpiresAt: expires})
}

// BeginOwnerAuthorization maps optional purpose/fresh-authentication input and starts
// boundary-owned authorization. It sets the secure HttpOnly strict-same-site login cookie for five
// minutes and returns only the authorization URL under no-store.
// BeginOwnerAuthorization преобразует необязательные purpose/fresh-authentication и начинает
// авторизацию под управлением boundary. Устанавливается защищённый HttpOnly strict-same-site login
// cookie на пять минут и выдаётся только authorization URL под no-store.
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

// CompleteOwnerAuthorization requires the login cookie and delegates state/code validation to the
// boundary, then validates returned metadata before setting owner session/CSRF cookies. The
// response ETag follows the resulting session revision; raw boundary failures remain private.
// CompleteOwnerAuthorization требует login cookie и поручает проверку state/code границе, затем
// проверяет метаданные до установки owner session/CSRF cookies. ETag соответствует итоговой
// session revision; сырые ошибки boundary остаются приватными.
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

// sessionPurposeMap copies optional purpose, resource/scope coordinates and receipt pins into the
// boundary input without authorizing those requested resources or manufacturing a missing receipt
// version.
// sessionPurposeMap копирует необязательные purpose, координаты ресурса/области и receipt pins во
// вход boundary без авторизации запрошенных ресурсов и без назначения отсутствующей версии
// receipt.
func sessionPurposeMap(source *generated.OwnerSessionPurpose) *boundary.SessionPurpose {
	if source == nil {
		return nil
	}
	scopeKind := ""
	if source.ScopeKind != nil {
		scopeKind = string(*source.ScopeKind)
	}
	purpose := &boundary.SessionPurpose{Kind: string(source.Kind), ProjectRef: stringValue(source.ProjectRef), SecretRef: stringValue(source.SecretRef),
		ScopeKind: scopeKind, OrganizationRef: stringValue(source.OrganizationRef),
		ReceiptRef: stringValue(source.ReceiptRef), ReceiptDigest: stringValue(source.ReceiptDigest)}
	if source.ReceiptVersion != nil {
		purpose.ReceiptVersion = *source.ReceiptVersion
	}
	return purpose
}

// sessionMetadataMap validates UUID generation, safe version counters and nonzero/coherent session
// deadlines, then selects backend-refresh versus reauthentication metadata. It projects timing
// facts without extending any server-owned session lifetime.
// sessionMetadataMap проверяет UUID поколения, безопасные счётчики версий и ненулевые
// согласованные сроки сессии, затем выбирает метаданные backend-refresh либо reauthentication.
// Проецируются факты времени без продления серверного срока сессии.
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

// writeSessionProblem maps availability/conflict/renewal-pending errors to retryable 503 and rate
// limiting to 429 with Retry-After one second; other errors become nonretryable 401 without
// internal details.
// writeSessionProblem преобразует ошибки availability/conflict/renewal-pending в повторяемый 503,
// rate limit в 429 с Retry-After одна секунда; прочие ошибки становятся неповторяемым 401 без
// внутренних подробностей.
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
