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

	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	oidcauth "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/authorization/oidc"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

const (
	SessionCookieName      = "__Host-mattercodex-session"
	CSRFCookieName         = "__Host-mattercodex-csrf"
	ProjectReferenceHeader = "X-MatterCodex-Project-ID"
)

var (
	ErrRateLimited     = errors.New("owner rate limit exceeded")
	ErrUnauthenticated = errors.New("owner credential cannot establish session")
)

type (
	identityContextKey              struct{}
	verifiedAuthorizationContextKey struct{}
)

type verifiedAuthorization struct {
	principal oidcauth.Principal
	bearer    string
}

type Identity struct {
	Subject         string
	OrganizationID  string
	SessionID       string
	SessionRevision uint64
	CSRFHash        string
	ExpiresAt       time.Time
}

type Config struct {
	Origins  []string
	Verifier *oidcauth.Verifier
	Sessions *session.Store
	Limiter  *ratelimit.Limiter
	Timeout  time.Duration
}

type Boundary struct {
	origins  map[string]struct{}
	verifier *oidcauth.Verifier
	sessions *session.Store
	limiter  *ratelimit.Limiter
	timeout  time.Duration
	stopping atomic.Bool
}

func New(config Config) (*Boundary, error) {
	if config.Verifier == nil || config.Sessions == nil || config.Limiter == nil ||
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
	return &Boundary{origins: origins, verifier: config.Verifier, sessions: config.Sessions, limiter: config.Limiter, timeout: config.Timeout}, nil
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
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-MatterCodex-Project-ID")
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
				writeProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
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
		identity, claims, err := func() (Identity, session.Claims, error) {
			defer releasePreAuth()
			return boundary.authenticate(request)
		}()
		if err != nil {
			writeProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
			return
		}
		subjectKey := identity.OrganizationID + ":" + identity.Subject
		if !boundary.limiter.Allow(subjectKey) {
			writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
			return
		}
		var release func()
		var ok bool
		if request.URL.Path == "/api/v1/realtime" {
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
		ctx = context.WithValue(ctx, identityContextKey{}, identity)
		if request.URL.Path == "/api/v1/realtime" {
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
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		return false
	}
	allowedHeaders := map[string]struct{}{"authorization": {}, "content-type": {}, "idempotency-key": {}, "if-match": {}, "x-csrf-token": {}, "x-mattercodex-project-id": {}}
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
	if request.URL.Path == "/api/v1/realtime" {
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
	return controlplaneclient.WithProjectReference(ctx, reference)
}

func exactProjectPathReference(request *http.Request) (string, error) {
	if request.Method != http.MethodPut && request.Method != http.MethodDelete {
		return "", nil
	}
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		return "", nil
	}
	reference := strings.TrimPrefix(request.URL.Path, prefix)
	if reference == "" || strings.Contains(reference, "/") {
		return "", nil
	}
	parsed, err := uuid.Parse(reference)
	if err != nil || parsed.String() != reference {
		return "", errors.New("invalid project reference in path")
	}
	return reference, nil
}

func (boundary *Boundary) VerifyAuthorization(ctx context.Context, authorization string) (oidcauth.Principal, string, error) {
	return boundary.verifier.VerifyAuthorization(ctx, authorization)
}

func (boundary *Boundary) IssueSession(principal oidcauth.Principal, bearer string) (session.Claims, string, string, error) {
	if !boundary.limiter.Allow(principal.OrganizationID + ":" + principal.Subject) {
		return session.Claims{}, "", "", ErrRateLimited
	}
	if !principal.ExpiresAt.After(time.Now().UTC().Add(time.Minute)) {
		return session.Claims{}, "", "", ErrUnauthenticated
	}
	return boundary.sessions.Issue(principal.Subject, principal.OrganizationID, principal.SessionID, principal.SessionRevision, bearer, principal.ExpiresAt)
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

func VerifiedAuthorizationFromContext(ctx context.Context) (oidcauth.Principal, string, bool) {
	verified, ok := ctx.Value(verifiedAuthorizationContextKey{}).(verifiedAuthorization)
	return verified.principal, verified.bearer, ok
}

func VerifyCSRFToken(identity Identity, token string) bool {
	claims := session.Claims{CSRFHash: identity.CSRFHash}
	return session.VerifyCSRF(claims, token)
}

func (boundary *Boundary) authenticate(request *http.Request) (Identity, session.Claims, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Identity{}, session.Claims{}, errors.New("owner session is unavailable")
	}
	claims, err := boundary.sessions.Open(cookie.Value)
	if err != nil {
		return Identity{}, session.Claims{}, err
	}
	principal, err := boundary.verifier.VerifyToken(request.Context(), claims.Bearer)
	if err != nil || principal.Subject != claims.Subject || principal.OrganizationID != claims.OrganizationID || principal.SessionID != claims.OIDCSessionID ||
		principal.SessionRevision != claims.SessionRevision {
		return Identity{}, session.Claims{}, errors.New("owner session binding is invalid")
	}
	return Identity{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, CSRFHash: claims.CSRFHash,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
	}, claims, nil
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
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type": "https://mattercodex.local/problems/" + code, "title": http.StatusText(statusCode),
		"status": statusCode, "code": code, "correlationId": uuid.NewString(), "retryable": retryable,
	})
}
