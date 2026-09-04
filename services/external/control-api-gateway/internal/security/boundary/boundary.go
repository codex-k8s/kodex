package boundary

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

const (
	SessionCookieName             = "__Host-kodex-session"
	CSRFCookieName                = "__Host-kodex-csrf"
	ProjectReferenceHeader        = "X-Kodex-Project-ID"
	freshAuthenticationWindow     = 2 * time.Minute
	freshAuthenticationFutureSkew = 30 * time.Second
)

var (
	ErrRateLimited                  = errors.New("owner rate limit exceeded")
	ErrUnauthenticated              = errors.New("owner credential cannot establish session")
	ErrSessionPurposeInvalid        = errors.New("owner session purpose is invalid")
	ErrFreshAuthenticationRequired  = errors.New("fresh owner authentication is required")
	ErrElevationRequired            = errors.New("runtime secret reveal elevation is required")
	ErrElevationConsumed            = errors.New("runtime secret reveal elevation is already consumed")
	ErrElevationUnavailable         = errors.New("runtime secret reveal elevation store is unavailable")
	ErrSessionValidationUnavailable = errors.New("owner session validation store is unavailable")
)

type (
	identityContextKey              struct{}
	verifiedAuthorizationContextKey struct{}
	authenticatedSessionContextKey  struct{}
	projectReferenceContextKey      struct{}
)

type verifiedAuthorization struct {
	principal oidcauth.Principal
	bearer    string
}

type authenticatedSession struct {
	claims       session.Claims
	bearerExpiry time.Time
}

type SessionPurpose struct {
	Kind       string
	ProjectRef string
	SecretRef  string
}

type Identity struct {
	Subject          string
	OrganizationID   string
	SessionID        string
	BrowserSessionID string
	SessionRevision  uint64
	CSRFHash         string
	ExpiresAt        time.Time
	Elevation        *session.Elevation
}

type OIDCVerifier interface {
	VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error)
	VerifyToken(context.Context, string) (oidcauth.Principal, error)
}

type SessionStore interface {
	Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error)
	IssueWithElevation(string, string, string, uint64, string, time.Time, *session.Elevation) (session.Claims, string, string, error)
	Open(string) (session.Claims, error)
	Renew(session.Claims, time.Time) (session.Claims, string, bool, error)
}

type RevocationStore interface {
	Revoke(context.Context, string) error
	Revoked(context.Context, string) (bool, error)
	ConsumeOnce(context.Context, string) (bool, error)
}

type Config struct {
	Origins     []string
	Verifier    OIDCVerifier
	Sessions    SessionStore
	Revocations RevocationStore
	Limiter     *ratelimit.Limiter
	Timeout     time.Duration
}

type Boundary struct {
	origins     map[string]struct{}
	verifier    OIDCVerifier
	sessions    SessionStore
	revocations RevocationStore
	limiter     *ratelimit.Limiter
	timeout     time.Duration
	now         func() time.Time
	stopping    atomic.Bool
}

func New(config Config) (*Boundary, error) {
	if config.Verifier == nil || config.Sessions == nil || config.Revocations == nil || config.Limiter == nil ||
		config.Timeout < time.Second || config.Timeout > time.Minute || len(config.Origins) == 0 || len(config.Origins) > 8 {
		return nil, errors.New("owner security boundary configuration is invalid")
	}
	origins := make(map[string]struct{}, len(config.Origins))
	for _, origin := range config.Origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasSuffix(origin, "/") {
			return nil, errors.New("CORS origin is invalid")
		}
		origins[origin] = struct{}{}
	}
	return &Boundary{origins: origins, verifier: config.Verifier, sessions: config.Sessions, revocations: config.Revocations, limiter: config.Limiter, timeout: config.Timeout, now: time.Now}, nil
}

func (boundary *Boundary) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if boundary.stopping.Load() {
			writeProblem(writer, http.StatusServiceUnavailable, "STOPPING", true)
			return
		}
		origin := request.Header.Get("Origin")
		if origin != "" {
			if !boundary.AllowsOrigin(origin) {
				writeProblem(writer, http.StatusForbidden, "ORIGIN_REJECTED", false)
				return
			}
			writer.Header().Set("Access-Control-Allow-Origin", origin)
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
			writer.Header().Set("Access-Control-Expose-Headers", "ETag, Retry-After")
			writer.Header().Add("Vary", "Origin")
		}
		if request.Method == http.MethodOptions {
			if origin == "" || !allowedPreflight(request) {
				writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
				return
			}
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-Audio-Size, X-CSRF-Token, X-File-Name, X-Kodex-Project-ID")
			writer.Header().Add("Vary", "Access-Control-Request-Method")
			writer.Header().Add("Vary", "Access-Control-Request-Headers")
			writer.Header().Set("Access-Control-Max-Age", "300")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		releasePreAuth, admitted := boundary.limiter.AcquirePreAuth()
		if !admitted {
			writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
			return
		}
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/session" {
			deadline, cancel := context.WithTimeout(request.Context(), boundary.timeout)
			defer cancel()
			principal, bearer, err := func() (oidcauth.Principal, string, error) {
				defer releasePreAuth()
				return boundary.verifier.VerifyAuthorization(deadline, request.Header.Get("Authorization"))
			}()
			if err != nil {
				statusCode, code, retryable := authenticationProblem(err)
				writeProblem(writer, statusCode, code, retryable)
				return
			}
			subjectKey := principal.OrganizationID + ":" + principal.Subject
			if !boundary.limiter.Allow(subjectKey) {
				writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
				return
			}
			release, ok := boundary.limiter.AcquireHTTP(subjectKey)
			if !ok {
				writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
				return
			}
			defer release()
			ctx, err := controlplaneclient.WithApplicationGrant(deadline, bearer)
			if err != nil {
				writeProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
				return
			}
			ctx = context.WithValue(ctx, verifiedAuthorizationContextKey{}, verifiedAuthorization{principal: principal, bearer: bearer})
			next.ServeHTTP(writer, request.WithContext(ctx))
			return
		}
		identity, claims, bearerExpiry, err := func() (Identity, session.Claims, time.Time, error) {
			defer releasePreAuth()
			return boundary.authenticate(request)
		}()
		if err != nil {
			statusCode, code, retryable := authenticationProblem(err)
			writeProblem(writer, statusCode, code, retryable)
			return
		}
		subjectKey := identity.OrganizationID + ":" + identity.Subject
		if !boundary.limiter.Allow(subjectKey) {
			writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
			return
		}
		var release func()
		var ok bool
		if isRealtimePath(request.URL.Path) {
			release, ok = boundary.limiter.AcquireWebSocket(subjectKey)
		} else {
			release, ok = boundary.limiter.AcquireHTTP(subjectKey)
		}
		if !ok {
			writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
			return
		}
		defer release()
		if isMutation(request.Method) {
			if origin == "" || !boundary.verifyCSRF(request, claims) {
				writeProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
				return
			}
		}
		ctx, err := controlplaneclient.WithApplicationGrant(request.Context(), claims.Bearer)
		if err != nil {
			writeProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
			return
		}
		ctx, err = withProjectReference(ctx, request)
		if err != nil {
			writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		if request.Method == http.MethodPut && request.URL.Path == "/api/v1/session" {
			claims, renewed, err := boundary.renewSession(writer, request, claims, bearerExpiry)
			if err != nil {
				writeProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
				return
			}
			if renewed {
				identity.ExpiresAt = time.Unix(claims.ExpiresAt, 0).UTC()
			}
		}
		ctx = context.WithValue(ctx, identityContextKey{}, identity)
		ctx = context.WithValue(ctx, authenticatedSessionContextKey{}, authenticatedSession{claims: claims, bearerExpiry: bearerExpiry})
		if isRealtimePath(request.URL.Path) {
			next.ServeHTTP(writer, request.WithContext(ctx))
			return
		}
		deadline, cancel := context.WithTimeout(ctx, boundary.timeout)
		defer cancel()
		next.ServeHTTP(writer, request.WithContext(deadline))
	})
}

func allowedPreflight(request *http.Request) bool {
	switch request.Header.Get("Access-Control-Request-Method") {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	allowedHeaders := map[string]struct{}{"authorization": {}, "content-type": {}, "idempotency-key": {}, "if-match": {}, "x-audio-size": {}, "x-csrf-token": {}, "x-file-name": {}, "x-kodex-project-id": {}}
	rawHeaders := request.Header.Get("Access-Control-Request-Headers")
	if rawHeaders == "" {
		return true
	}
	values := strings.Split(rawHeaders, ",")
	if len(values) > len(allowedHeaders) {
		return false
	}
	for _, raw := range values {
		name := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowedHeaders[name]; !ok {
			return false
		}
	}
	return true
}

func withProjectReference(ctx context.Context, request *http.Request) (context.Context, error) {
	values := request.Header.Values(ProjectReferenceHeader)
	if len(values) > 1 {
		return nil, errors.New("multiple project references are not allowed")
	}
	headerReference := ""
	if len(values) == 1 {
		headerReference = values[0]
	}
	queryReference := ""
	if isRealtimePath(request.URL.Path) {
		queryValues := request.URL.Query()["projectId"]
		if len(queryValues) > 1 {
			return nil, errors.New("multiple realtime project references are not allowed")
		}
		if len(queryValues) == 1 {
			queryReference = queryValues[0]
		}
	}
	pathReference, err := exactProjectPathReference(request)
	if err != nil {
		return nil, err
	}
	if headerReference != "" && queryReference != "" && headerReference != queryReference {
		return nil, errors.New("project references do not match")
	}
	reference := headerReference
	if reference == "" {
		reference = queryReference
	}
	if reference != "" && pathReference != "" && reference != pathReference {
		return nil, errors.New("project references do not match")
	}
	if reference == "" {
		reference = pathReference
	}
	if reference == "" {
		return ctx, nil
	}
	ctx, err = controlplaneclient.WithProjectReference(ctx, reference)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, projectReferenceContextKey{}, reference), nil
}

func isRealtimePath(path string) bool {
	return path == "/api/v1/session/stream"
}

func exactProjectPathReference(request *http.Request) (string, error) {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		return "", nil
	}
	remainder := strings.TrimPrefix(request.URL.Path, prefix)
	reference, _, _ := strings.Cut(remainder, "/")
	if reference == "" {
		return "", nil
	}
	if !validOpaqueProjectReference(reference) {
		return "", errors.New("invalid project reference in path")
	}
	return reference, nil
}

func validOpaqueProjectReference(reference string) bool {
	if len(reference) < 13 || len(reference) > 96 || !strings.HasPrefix(reference, "prj_") {
		return false
	}
	for _, character := range strings.TrimPrefix(reference, "prj_") {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validOpaqueReference(reference string) bool {
	if len(reference) < 8 || len(reference) > 128 {
		return false
	}
	for _, character := range reference {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (boundary *Boundary) VerifyAuthorization(ctx context.Context, authorization string) (oidcauth.Principal, string, error) {
	return boundary.verifier.VerifyAuthorization(ctx, authorization)
}

func (boundary *Boundary) IssueSession(principal oidcauth.Principal, bearer string, purpose *SessionPurpose) (session.Claims, string, string, error) {
	if !boundary.limiter.Allow(principal.OrganizationID + ":" + principal.Subject) {
		return session.Claims{}, "", "", ErrRateLimited
	}
	now := boundary.now().UTC()
	if !principal.ExpiresAt.After(now.Add(time.Minute)) {
		return session.Claims{}, "", "", ErrUnauthenticated
	}
	if purpose == nil {
		return boundary.sessions.Issue(principal.Subject, principal.OrganizationID, principal.SessionID, principal.SessionRevision, bearer, principal.ExpiresAt)
	}
	if purpose.Kind != session.ElevationKindRuntimeSecretReveal || !validOpaqueReference(purpose.ProjectRef) || !validOpaqueReference(purpose.SecretRef) {
		return session.Claims{}, "", "", ErrSessionPurposeInvalid
	}
	if principal.AuthenticatedAt.IsZero() || principal.AuthenticatedAt.After(now.Add(freshAuthenticationFutureSkew)) ||
		now.Sub(principal.AuthenticatedAt) > freshAuthenticationWindow {
		return session.Claims{}, "", "", ErrFreshAuthenticationRequired
	}
	elevationExpiry := principal.AuthenticatedAt.Add(freshAuthenticationWindow).UTC()
	if maximumExpiry := now.Add(freshAuthenticationWindow); elevationExpiry.After(maximumExpiry) {
		elevationExpiry = maximumExpiry
	}
	if elevationExpiry.After(principal.ExpiresAt) {
		elevationExpiry = principal.ExpiresAt.UTC()
	}
	return boundary.sessions.IssueWithElevation(principal.Subject, principal.OrganizationID, principal.SessionID, principal.SessionRevision, bearer, principal.ExpiresAt, &session.Elevation{
		Kind: purpose.Kind, ProjectRef: purpose.ProjectRef, SecretRef: purpose.SecretRef, ExpiresAt: elevationExpiry.Unix(),
	})
}

// ConsumeRuntimeSecretReveal атомарно расходует elevation и заменяет
// отозванную purpose-session обычной browser session без повышения полномочий.
func (boundary *Boundary) ConsumeRuntimeSecretReveal(ctx context.Context, writer http.ResponseWriter, projectRef, secretRef string) error {
	identity, identityOK := IdentityFromContext(ctx)
	authenticated, sessionOK := ctx.Value(authenticatedSessionContextKey{}).(authenticatedSession)
	if !identityOK || !sessionOK || identity.Elevation == nil ||
		identity.Elevation.Kind != session.ElevationKindRuntimeSecretReveal ||
		identity.Elevation.ProjectRef != projectRef || identity.Elevation.SecretRef != secretRef ||
		!boundary.now().UTC().Before(time.Unix(identity.Elevation.ExpiresAt, 0).UTC()) {
		return ErrElevationRequired
	}
	won, err := boundary.revocations.ConsumeOnce(ctx, identity.BrowserSessionID)
	if err != nil {
		return ErrElevationUnavailable
	}
	if !won {
		return ErrElevationConsumed
	}
	claims, encoded, csrf, err := boundary.sessions.Issue(
		authenticated.claims.Subject,
		authenticated.claims.OrganizationID,
		authenticated.claims.OIDCSessionID,
		authenticated.claims.SessionRevision,
		authenticated.claims.Bearer,
		authenticated.bearerExpiry,
	)
	if err != nil {
		return ErrElevationUnavailable
	}
	SetOwnerSessionCookies(writer, claims, encoded, csrf)
	return nil
}

func (boundary *Boundary) RevokeSession(ctx context.Context, identity Identity) error {
	return boundary.revocations.Revoke(ctx, identity.BrowserSessionID)
}

func (boundary *Boundary) StopAdmission() { boundary.stopping.Store(true) }

func (boundary *Boundary) AllowsOrigin(origin string) bool {
	_, ok := boundary.origins[origin]
	return ok
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

func ProjectReferenceFromContext(ctx context.Context) (string, bool) {
	reference, ok := ctx.Value(projectReferenceContextKey{}).(string)
	return reference, ok && reference != ""
}

func VerifiedAuthorizationFromContext(ctx context.Context) (oidcauth.Principal, string, bool) {
	verified, ok := ctx.Value(verifiedAuthorizationContextKey{}).(verifiedAuthorization)
	return verified.principal, verified.bearer, ok
}

func VerifyCSRFToken(identity Identity, token string) bool {
	claims := session.Claims{CSRFHash: identity.CSRFHash}
	return session.VerifyCSRF(claims, token)
}

func (boundary *Boundary) authenticate(request *http.Request) (Identity, session.Claims, time.Time, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Identity{}, session.Claims{}, time.Time{}, errors.New("owner session is unavailable")
	}
	claims, err := boundary.sessions.Open(cookie.Value)
	if err != nil {
		return Identity{}, session.Claims{}, time.Time{}, err
	}
	revoked, err := boundary.revocations.Revoked(request.Context(), claims.SessionID)
	if err != nil {
		return Identity{}, session.Claims{}, time.Time{}, ErrSessionValidationUnavailable
	}
	if revoked {
		return Identity{}, session.Claims{}, time.Time{}, errors.New("owner session is revoked")
	}
	principal, err := boundary.verifier.VerifyToken(request.Context(), claims.Bearer)
	if errors.Is(err, oidcauth.ErrSigningKeysUnavailable) {
		return Identity{}, session.Claims{}, time.Time{}, err
	}
	if err != nil || principal.Subject != claims.Subject || principal.OrganizationID != claims.OrganizationID || principal.SessionID != claims.OIDCSessionID ||
		principal.SessionRevision != claims.SessionRevision {
		return Identity{}, session.Claims{}, time.Time{}, errors.New("owner session binding is invalid")
	}
	return Identity{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		BrowserSessionID: claims.SessionID, SessionRevision: claims.SessionRevision, CSRFHash: claims.CSRFHash,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(), Elevation: claims.Elevation,
	}, claims, principal.ExpiresAt, nil
}

func (boundary *Boundary) renewSession(writer http.ResponseWriter, request *http.Request, claims session.Claims, bearerExpiry time.Time) (session.Claims, bool, error) {
	origin := request.Header.Get("Origin")
	if origin == "" || !boundary.AllowsOrigin(origin) || !boundary.verifyCSRF(request, claims) {
		return claims, false, nil
	}
	csrfCookie, err := request.Cookie(CSRFCookieName)
	if err != nil {
		return claims, false, nil
	}
	renewedClaims, encoded, renewed, err := boundary.sessions.Renew(claims, bearerExpiry)
	if err != nil || !renewed {
		return renewedClaims, false, err
	}
	SetOwnerSessionCookies(writer, renewedClaims, encoded, csrfCookie.Value)
	return renewedClaims, true, nil
}

func SetOwnerSessionCookies(writer http.ResponseWriter, claims session.Claims, encoded, csrf string) {
	maxAge := int(time.Until(time.Unix(claims.ExpiresAt, 0)).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	if maxAge > 3600 {
		maxAge = 3600
	}
	writer.Header().Set("Cache-Control", "no-store")
	http.SetCookie(writer, &http.Cookie{Name: SessionCookieName, Value: encoded, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	http.SetCookie(writer, &http.Cookie{Name: CSRFCookieName, Value: csrf, Path: "/", Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func authenticationProblem(err error) (int, string, bool) {
	if errors.Is(err, oidcauth.ErrSigningKeysUnavailable) || errors.Is(err, ErrSessionValidationUnavailable) {
		return http.StatusServiceUnavailable, "UNAVAILABLE", true
	}
	return http.StatusUnauthorized, "UNAUTHENTICATED", false
}

func (boundary *Boundary) verifyCSRF(request *http.Request, claims session.Claims) bool {
	header := request.Header.Get("X-CSRF-Token")
	cookie, err := request.Cookie(CSRFCookieName)
	if err != nil || len(header) != len(cookie.Value) || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		return false
	}
	return session.VerifyCSRF(claims, header)
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func writeProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	if statusCode == http.StatusTooManyRequests {
		writer.Header().Set("Retry-After", "1")
	}
	writer.WriteHeader(statusCode)
	title := http.StatusText(statusCode)
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		title = localizer.Localize(code)
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type": "urn:kodex:problem:" + strings.ToLower(code), "title": title,
		"status": statusCode, "code": code, "correlationId": uuid.NewString(), "retryable": retryable,
	})
}
