// Package boundary enforces the browser transport checks around verified identity, session state and request context; domain owners still decide resource permissions.
// Package boundary выполняет браузерные транспортные проверки личности, состояния сессии и контекста запроса; права на ресурсы по-прежнему определяют владельцы доменов.
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
	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

const (
	// SessionCookieName names the Secure, host-only browser credential cookie; its __Host- prefix requires Path=/ and forbids Domain.
	// SessionCookieName задаёт имя защищённой cookie браузерной сессии; префикс __Host- требует Path=/ и запрещает Domain.
	SessionCookieName = "__Host-kodex-session"
	// CSRFCookieName names the JavaScript-readable CSRF cookie paired with the authenticated claim hash, not an independent credential.
	// CSRFCookieName задаёт имя доступной JavaScript CSRF-cookie, связанной с хешем в проверенных claims, а не самостоятельного удостоверения.
	CSRFCookieName = "__Host-kodex-csrf"
	// ProjectReferenceHeader carries a requested project selector. The boundary validates its shape and agreement with the route, not project membership.
	// ProjectReferenceHeader передаёт запрошенный проект. Граница проверяет формат и согласованность с маршрутом, но не членство в проекте.
	ProjectReferenceHeader = "X-Kodex-Project-ID"
	// freshAuthenticationWindow limits authentication age for runtime-secret elevation to strictly less than two minutes.
	// freshAuthenticationWindow ограничивает возраст входа для раскрытия runtime-секрета строго менее чем двумя минутами.
	freshAuthenticationWindow = 2 * time.Minute
	// emailFreshAuthenticationWindow allows less than five minutes of authentication age for receipt reconciliation; elevation itself remains capped separately.
	// emailFreshAuthenticationWindow допускает возраст входа менее пяти минут для сверки почтового receipt; срок самого повышения ограничивается отдельно.
	emailFreshAuthenticationWindow = 5 * time.Minute
	// freshAuthenticationFutureSkew tolerates at most thirty seconds of future auth_time when assessing fresh authentication.
	// freshAuthenticationFutureSkew допускает auth_time не более чем на тридцать секунд в будущем при проверке свежего входа.
	freshAuthenticationFutureSkew = 30 * time.Second
)

var (
	// ErrRateLimited reports rejection by the per-organization/subject issuance limiter before creating a session or elevation.
	// ErrRateLimited обозначает отказ ограничителя выдачи по организации и субъекту до создания сессии или повышения.
	ErrRateLimited = errors.New("owner rate limit exceeded")
	// ErrUnauthenticated rejects missing or inconsistent authenticated browser bindings; it does not imply that an identity provider is unavailable.
	// ErrUnauthenticated отклоняет отсутствующие или несогласованные привязки проверенной браузерной сессии и не означает недоступность провайдера входа.
	ErrUnauthenticated = errors.New("owner credential cannot establish session")
	// ErrSessionPurposeInvalid rejects an unknown purpose or a malformed/mixed runtime-secret and email-receipt binding.
	// ErrSessionPurposeInvalid отклоняет неизвестную цель либо некорректную или смешанную привязку runtime-секрета и почтового receipt.
	ErrSessionPurposeInvalid = errors.New("owner session purpose is invalid")
	// ErrFreshAuthenticationRequired requires a recent acceptable auth_time and, for email reconciliation, nonempty ACR and AMR evidence.
	// ErrFreshAuthenticationRequired требует допустимый недавний auth_time, а для почтовой сверки также непустые ACR и AMR.
	ErrFreshAuthenticationRequired = errors.New("fresh owner authentication is required")
	// ErrElevationRequired rejects a missing, expired or differently scoped elevation before consuming the one-use record.
	// ErrElevationRequired отклоняет отсутствующее, истёкшее или привязанное к другой области повышение до расхода одноразовой записи.
	ErrElevationRequired = errors.New("owner operation elevation is required")
	// ErrElevationConsumed reports that the browser-session one-use claim was not won; no replacement cookies may be issued.
	// ErrElevationConsumed означает, что одноразовая запись браузерной сессии не была получена; заменяющие cookies не выдаются.
	ErrElevationConsumed = errors.New("owner operation elevation is already consumed")
	// ErrElevationUnavailable closes the elevation path when consumption or replacement fails; a previous successful consume is not rolled back.
	// ErrElevationUnavailable закрывает путь повышения при ошибке расхода или замены; уже успешный расход не откатывается.
	ErrElevationUnavailable = errors.New("owner operation elevation store is unavailable")
	// ErrSessionValidationUnavailable signals an unavailable required family/revocation dependency and maps authentication failures to retryable 503.
	// ErrSessionValidationUnavailable обозначает недоступность обязательного хранилища семейств или отзывов и переводит ошибку аутентификации в повторяемый ответ 503.
	ErrSessionValidationUnavailable = errors.New("owner session validation store is unavailable")
	// ErrSessionCSRFRejected rejects the Origin/CSRF proof required before backend family renewal and maps to nonretryable 403.
	// ErrSessionCSRFRejected отклоняет доказательство Origin/CSRF до серверного обновления семейства и отображается в неповторяемый ответ 403.
	ErrSessionCSRFRejected = errors.New("owner session CSRF binding is invalid")
)

type (
	// identityContextKey privately separates the verified request Identity from caller-provided context values of other key types.
	// identityContextKey отдельным закрытым типом ключа отделяет проверенную Identity запроса от значений контекста с другими ключами.
	identityContextKey struct{}
	// verifiedAuthorizationContextKey identifies the principal/bearer pair installed only after the session-creation authorization check.
	// verifiedAuthorizationContextKey помечает пару principal/bearer, установленную после проверки авторизации при создании сессии.
	verifiedAuthorizationContextKey struct{}
	// authenticatedSessionContextKey selects the opened claims and verified bearer ceiling used by renewal, metadata and elevation consumption.
	// authenticatedSessionContextKey выбирает открытые claims и проверенную границу срока bearer для обновления, метаданных и расхода повышения.
	authenticatedSessionContextKey struct{}
	// projectReferenceContextKey stores a syntactically reconciled project reference without asserting domain eligibility.
	// projectReferenceContextKey хранит согласованную по формату ссылку на проект, не утверждая доменные права доступа.
	projectReferenceContextKey struct{}
)

// verifiedAuthorization retains a verifier-produced principal and its bearer for session issuance; it must not be constructed from a request payload.
// verifiedAuthorization хранит principal и bearer, полученные от проверяющего адаптера для выдачи сессии, а не из тела запроса.
type verifiedAuthorization struct {
	principal oidcauth.Principal
	bearer    string
}

// authenticatedSession retains authenticated cookie claims and the verified access-token expiry for subsequent boundary operations.
// authenticatedSession хранит проверенные claims cookie и срок access token для последующих операций границы безопасности.
type authenticatedSession struct {
	claims       session.Claims
	bearerExpiry time.Time
}

// SessionPurpose aliases the session package's closed login-purpose schema so issuance and browser login share one binding representation.
// SessionPurpose ссылается на закрытую схему цели входа из пакета session, сохраняя единое представление привязки при выдаче и браузерном входе.
type SessionPurpose = session.LoginPurpose

// Identity exposes verifier-bound organization, subject, OIDC/browser session and CSRF data, with expiry capped by both cookie and access token. It is not project authorization.
// Identity предоставляет связанные проверкой организацию, субъекта, OIDC/browser-сессии и CSRF; срок ограничен cookie и access token. Это не разрешение на проект.
type Identity struct {
	FamilyID         string
	Subject          string
	OrganizationID   string
	SessionID        string
	BrowserSessionID string
	SessionRevision  uint64
	CSRFHash         string
	ExpiresAt        time.Time
	Elevation        *session.Elevation
}

// OIDCVerifier verifies authorization headers or raw access tokens and returns trusted identity/session bindings; browser cookies do not replace these checks.
// OIDCVerifier проверяет заголовки авторизации либо access tokens и возвращает доверенные привязки личности и сессии; браузерные cookies эти проверки не заменяют.
type OIDCVerifier interface {
	// VerifyAuthorization validates the complete authorization value and returns both the verified principal and extracted bearer, or an error.
	// VerifyAuthorization проверяет полное значение авторизации и возвращает проверенного principal и извлечённый bearer либо ошибку.
	VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error)
	// VerifyToken validates a raw bearer and returns its verified principal; the caller still compares its organization and session bindings.
	// VerifyToken проверяет bearer и возвращает подтверждённого principal; вызывающая сторона дополнительно сверяет организацию и сессионные привязки.
	VerifyToken(context.Context, string) (oidcauth.Principal, error)
}

// SessionStore defines cookie issue/open/renew operations; durable family state and one-use revocations belong to separate dependencies.
// SessionStore задаёт выдачу, открытие и обновление cookies; устойчивое состояние семейств и одноразовые отзывы принадлежат отдельным зависимостям.
type SessionStore interface {
	// Issue creates ordinary browser claims, their encoded cookie and CSRF token from already verified identity and bearer expiry.
	// Issue создаёт обычные браузерные claims, закодированную cookie и CSRF из уже проверенной личности и срока bearer.
	Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error)
	// IssueWithElevation additionally encodes the exact validated elevation; the boundary checks purpose and freshness before calling it.
	// IssueWithElevation дополнительно кодирует точное проверенное повышение; цель и свежесть входа граница проверяет до вызова.
	IssueWithElevation(string, string, string, uint64, string, time.Time, *session.Elevation) (session.Claims, string, string, error)
	// Open authenticates and decodes cookie claims; revocation lookup and comparison with a live bearer remain boundary responsibilities.
	// Open проверяет и декодирует claims cookie; поиск отзыва и сверка с действующим bearer остаются обязанностью границы.
	Open(string) (session.Claims, error)
	// Renew may replace an ordinary cookie within the supplied bearer expiry, returning whether an actual renewal occurred and any error.
	// Renew может заменить обычную cookie в пределах переданного срока bearer и сообщает, было ли обновление, либо возвращает ошибку.
	Renew(session.Claims, time.Time) (session.Claims, string, bool, error)
}

// RevocationStore owns browser-session revocation and atomic one-use consumption; unavailable storage must not be treated as an unused claim.
// RevocationStore владеет отзывом браузерных сессий и атомарным одноразовым расходом; недоступность хранилища нельзя считать неиспользованной записью.
type RevocationStore interface {
	// Revoke records a browser-session identifier as revoked and returns any persistence failure to the caller.
	// Revoke фиксирует отзыв идентификатора браузерной сессии и возвращает ошибку сохранения вызывающей стороне.
	Revoke(context.Context, string) error
	// Revoked reads whether the exact browser-session identifier is revoked, distinguishing a negative answer from storage failure.
	// Revoked проверяет отзыв точного идентификатора браузерной сессии, отличая отсутствие отзыва от ошибки хранилища.
	Revoked(context.Context, string) (bool, error)
	// ConsumeOnce atomically competes for the exact browser-session one-use record; only a true, nil result permits replacement.
	// ConsumeOnce атомарно получает одноразовую запись точной браузерной сессии; замену разрешает только результат true без ошибки.
	ConsumeOnce(context.Context, string) (bool, error)
}

// Config supplies exact HTTPS origins, mandatory verifier/session/revocation/limiter dependencies and a bounded HTTP timeout; family/login support is optional.
// Config задаёт точные HTTPS origins, обязательные проверки, хранилища, ограничитель и ограниченный HTTP timeout; поддержка семейств и браузерного входа необязательна.
type Config struct {
	BrowserOrigin string
	Logins        *session.Logins
	Families      *session.Families
	Origins       []string
	Verifier      OIDCVerifier
	Sessions      SessionStore
	Revocations   RevocationStore
	Limiter       *ratelimit.Limiter
	Timeout       time.Duration
}

// Boundary composes CORS, session/bearer binding, revocation, CSRF, rate/concurrency limits and request contexts. Its immutable origin set and atomic stop flag do not grant domain permissions.
// Boundary объединяет CORS, привязку сессии и bearer, отзывы, CSRF, ограничения частоты и параллельности, контексты запросов. Набор origins и атомарный флаг остановки не выдают доменные права.
type Boundary struct {
	browserOrigin string
	logins        *session.Logins
	families      *session.Families
	origins       map[string]struct{}
	verifier      OIDCVerifier
	sessions      SessionStore
	revocations   RevocationStore
	limiter       *ratelimit.Limiter
	timeout       time.Duration
	now           func() time.Time
	stopping      atomic.Bool
}

// New validates required dependencies, a one-second to one-minute timeout and one to eight exact HTTPS origins, copying origins into a private set. Configured browser login additionally requires BrowserOrigin in that set.
// New проверяет обязательные зависимости, timeout от секунды до минуты и от одного до восьми точных HTTPS origins, копируя их в закрытый набор. Для браузерного входа BrowserOrigin также должен входить в этот набор.
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
	if config.Logins != nil {
		if _, ok := origins[config.BrowserOrigin]; !ok {
			return nil, errors.New("OIDC browser origin is not permitted")
		}
	}
	return &Boundary{browserOrigin: config.BrowserOrigin, logins: config.Logins, families: config.Families, origins: origins, verifier: config.Verifier, sessions: config.Sessions, revocations: config.Revocations, limiter: config.Limiter, timeout: config.Timeout, now: time.Now}, nil
}

// Middleware rejects stopping/foreign-origin traffic, handles preflight, bounds pre-auth and subject concurrency, and installs verified application grants and identity. Only explicit session PUT renews cookies; mutations require Origin/CSRF and realtime retains the request lifetime instead of the ordinary HTTP timeout.
// Middleware отклоняет запросы при остановке и чужой Origin, обрабатывает preflight, ограничивает предварительную проверку и параллельность субъекта, устанавливает проверенные application grants и личность. Cookies обновляет только явный PUT сессии; изменения требуют Origin/CSRF, realtime сохраняет срок запроса вместо обычного HTTP timeout.
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
		if request.Method == http.MethodPost && (request.URL.Path == "/api/v1/session/authorization" || request.URL.Path == "/api/v1/session/callback") {
			defer releasePreAuth()
			if origin == "" || origin != boundary.browserOrigin {
				writeProblem(writer, http.StatusForbidden, "ORIGIN_REJECTED", false)
				return
			}
			if !boundary.limiter.Allow("oidc-browser-login") {
				writeProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
				return
			}
			deadline, cancel := context.WithTimeout(request.Context(), boundary.timeout)
			defer cancel()
			next.ServeHTTP(writer, request.WithContext(deadline))
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
				if bearerExpiry.Before(identity.ExpiresAt) {
					identity.ExpiresAt = bearerExpiry
				}
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

// allowedPreflight accepts only the declared CRUD methods and case-insensitive approved header names within the fixed list-size bound. It does not validate Origin itself.
// allowedPreflight допускает только заявленные CRUD-методы и разрешённые имена заголовков без учёта регистра с ограничением длины списка. Сам Origin здесь не проверяется.
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

// withProjectReference reconciles a single header, realtime query and exact project path before adding the transport project selector. Exact GET/POST purge recovery stays organization-scoped and rejects a project header; no membership is inferred.
// withProjectReference согласует единственный заголовок, query realtime и точный путь проекта перед добавлением транспортного селектора. Точное восстановление удаления по GET/POST остаётся в области организации и запрещает заголовок проекта; членство не выводится.
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
	if isProjectPurgeRecoveryPath(request) {
		if headerReference != "" {
			return nil, errors.New("project purge recovery requires organization scope")
		}
		return ctx, nil
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

// isRealtimePath recognizes only the exact session stream endpoint, without accepting suffixes or historical stream routes.
// isRealtimePath распознаёт только точный endpoint потока сессии, не принимая суффиксы и прежние маршруты потоков.
func isRealtimePath(path string) bool {
	return path == "/api/v1/session/stream"
}

// isProjectPurgeRecoveryPath recognizes exact GET/POST project purge-recovery routes with a valid opaque project reference and no trailing suffix.
// isProjectPurgeRecoveryPath распознаёт точные GET/POST-маршруты purge-recovery с допустимой непрозрачной ссылкой проекта без дополнительного суффикса.
func isProjectPurgeRecoveryPath(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		return false
	}
	remainder, found := strings.CutPrefix(request.URL.Path, "/api/v1/projects/")
	if !found {
		return false
	}
	reference, suffix, found := strings.Cut(remainder, "/")
	return found && suffix == "purge-recovery" && validOpaqueProjectReference(reference)
}

// exactProjectPathReference extracts and validates the first project path segment, leaving collection paths and GET trash unbound. This is route parsing, not ownership resolution.
// exactProjectPathReference извлекает и проверяет первый сегмент проекта, оставляя пути коллекции и GET корзины без привязки. Это разбор маршрута, а не разрешение владения.
func exactProjectPathReference(request *http.Request) (string, error) {
	// prefix anchors project extraction at the exact API collection path, not an arbitrary URL substring.
	// prefix привязывает извлечение проекта к точному пути API-коллекции, а не произвольной подстроке URL.
	const prefix = "/api/v1/projects/"
	if request.Method == http.MethodGet && request.URL.Path == "/api/v1/projects/trash" {
		return "", nil
	}
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

// validOpaqueProjectReference requires prj_, total length 13..96 and an ASCII alphanumeric, hyphen or underscore suffix; it does not resolve an existing project.
// validOpaqueProjectReference требует prj_, общую длину 13..96 и суффикс из ASCII-букв, цифр, дефиса или подчёркивания; существующий проект не разрешается.
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

// validOpaqueReference accepts 8..128 ASCII alphanumeric, hyphen or underscore characters without imposing a resource prefix or resolving authority.
// validOpaqueReference допускает 8..128 ASCII-букв, цифр, дефисов или подчёркиваний без требования префикса ресурса и без разрешения полномочий.
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

// VerifyAuthorization delegates authorization-header verification unchanged to the configured verifier; it does not itself issue a cookie or consume elevation.
// VerifyAuthorization без преобразований передаёт проверку заголовка настроенному verifier; сама cookie не выдаёт и повышение не расходует.
func (boundary *Boundary) VerifyAuthorization(ctx context.Context, authorization string) (oidcauth.Principal, string, error) {
	return boundary.verifier.VerifyAuthorization(ctx, authorization)
}

// IssueSession first applies subject rate, bearer lifetime and optional purpose/freshness checks, then selects ordinary or elevated cookie issuance using the verified principal.
// IssueSession сначала проверяет частоту субъекта, срок bearer и при необходимости цель со свежестью входа, затем выдаёт обычную либо повышенную cookie по проверенному principal.
func (boundary *Boundary) IssueSession(principal oidcauth.Principal, bearer string, purpose *SessionPurpose) (session.Claims, string, string, error) {
	elevation, err := boundary.sessionElevation(principal, purpose)
	if err != nil {
		return session.Claims{}, "", "", err
	}
	if elevation == nil {
		return boundary.sessions.Issue(principal.Subject, principal.OrganizationID, principal.SessionID, principal.SessionRevision, bearer, principal.ExpiresAt)
	}
	return boundary.sessions.IssueWithElevation(principal.Subject, principal.OrganizationID, principal.SessionID, principal.SessionRevision, bearer, principal.ExpiresAt, elevation)
}

// sessionElevation validates one closed purpose and its exact binding after rate/expiry checks. Email purposes additionally require nonempty ACR/AMR; auth_time age and future skew are bounded, and expiry is the minimum of freshness, maximum elevation lifetime and bearer expiry.
// sessionElevation после ограничителя и срока проверяет закрытую цель и точную привязку. Почтовая цель также требует непустые ACR/AMR; ограничены возраст auth_time и смещение в будущее, срок равен минимуму свежести, максимального срока повышения и срока bearer.
func (boundary *Boundary) sessionElevation(principal oidcauth.Principal, purpose *SessionPurpose) (*session.Elevation, error) {
	if !boundary.limiter.Allow(principal.OrganizationID + ":" + principal.Subject) {
		return nil, ErrRateLimited
	}
	now := boundary.now().UTC()
	if !principal.ExpiresAt.After(now.Add(time.Minute)) {
		return nil, ErrUnauthenticated
	}
	if purpose == nil {
		return nil, nil
	}
	window := freshAuthenticationWindow
	switch purpose.Kind {
	case session.ElevationKindRuntimeSecretReveal:
		if !session.ValidRuntimeSecretBinding(purpose.ScopeKind, purpose.OrganizationRef, purpose.ProjectRef, purpose.SecretRef) ||
			purpose.ReceiptRef != "" || purpose.ReceiptVersion != 0 || purpose.ReceiptDigest != "" {
			return nil, ErrSessionPurposeInvalid
		}
	case session.ElevationKindEmailReconciliation:
		if purpose.ProjectRef != "" || purpose.SecretRef != "" || purpose.ScopeKind != "" || purpose.OrganizationRef != "" || !session.ValidEmailReceiptBinding(purpose.ReceiptRef, purpose.ReceiptVersion, purpose.ReceiptDigest) {
			return nil, ErrSessionPurposeInvalid
		}
		window = emailFreshAuthenticationWindow
		interactive := false
		for _, method := range principal.AMR {
			interactive = interactive || strings.TrimSpace(method) != ""
		}
		if strings.TrimSpace(principal.ACR) == "" || !interactive {
			return nil, ErrFreshAuthenticationRequired
		}
	default:
		return nil, ErrSessionPurposeInvalid
	}
	if principal.AuthenticatedAt.IsZero() || principal.AuthenticatedAt.After(now.Add(freshAuthenticationFutureSkew)) ||
		now.Sub(principal.AuthenticatedAt) >= window {
		return nil, ErrFreshAuthenticationRequired
	}
	elevationExpiry := principal.AuthenticatedAt.Add(window).UTC()
	if maximumExpiry := now.Add(session.MaximumElevationLifetime); elevationExpiry.After(maximumExpiry) {
		elevationExpiry = maximumExpiry
	}
	if elevationExpiry.After(principal.ExpiresAt) {
		elevationExpiry = principal.ExpiresAt.UTC()
	}
	return &session.Elevation{
		Kind: purpose.Kind, ScopeKind: purpose.ScopeKind, OrganizationRef: purpose.OrganizationRef, ProjectRef: purpose.ProjectRef, SecretRef: purpose.SecretRef,
		ReceiptRef: purpose.ReceiptRef, ReceiptVersion: purpose.ReceiptVersion, ReceiptDigest: purpose.ReceiptDigest, ExpiresAt: elevationExpiry.Unix(),
	}, nil
}

// ConsumeRuntimeSecretReveal verifies authenticated context, exact kind/scope/organization/project/secret and unexpired elevation before consuming the browser-session one-use record and issuing ordinary replacement cookies.
// ConsumeRuntimeSecretReveal до расхода одноразовой записи браузерной сессии и выдачи обычных заменяющих cookies проверяет контекст, точные kind/scope/organization/project/secret и действующий срок повышения.
func (boundary *Boundary) ConsumeRuntimeSecretReveal(ctx context.Context, writer http.ResponseWriter, scopeKind, organizationRef, projectRef, secretRef string) error {
	identity, identityOK := IdentityFromContext(ctx)
	authenticated, sessionOK := ctx.Value(authenticatedSessionContextKey{}).(authenticatedSession)
	if !identityOK || !sessionOK || identity.Elevation == nil ||
		identity.Elevation.Kind != session.ElevationKindRuntimeSecretReveal ||
		!session.ValidRuntimeSecretBinding(scopeKind, organizationRef, projectRef, secretRef) || identity.Elevation.ScopeKind != scopeKind || identity.Elevation.OrganizationRef != organizationRef ||
		identity.Elevation.ProjectRef != projectRef || identity.Elevation.SecretRef != secretRef ||
		!boundary.now().UTC().Before(time.Unix(identity.Elevation.ExpiresAt, 0).UTC()) {
		return ErrElevationRequired
	}
	return boundary.consumeElevation(ctx, writer, identity, authenticated)
}

// ConsumeEmailReconciliation requires the exact valid receipt/version/digest, no secret scope fields and an unexpired email elevation before any one-use consumption.
// ConsumeEmailReconciliation до одноразового расхода требует точные допустимые receipt/version/digest, отсутствие полей области секрета и неистёкшее почтовое повышение.
func (boundary *Boundary) ConsumeEmailReconciliation(ctx context.Context, writer http.ResponseWriter, receiptRef string, version int64, digest string) error {
	identity, identityOK := IdentityFromContext(ctx)
	authenticated, sessionOK := ctx.Value(authenticatedSessionContextKey{}).(authenticatedSession)
	if !identityOK || !sessionOK || identity.Elevation == nil ||
		identity.Elevation.Kind != session.ElevationKindEmailReconciliation ||
		identity.Elevation.ScopeKind != "" || identity.Elevation.OrganizationRef != "" ||
		identity.Elevation.ProjectRef != "" || identity.Elevation.SecretRef != "" ||
		identity.Elevation.ReceiptRef != receiptRef || identity.Elevation.ReceiptVersion != version || identity.Elevation.ReceiptDigest != digest ||
		!session.ValidEmailReceiptBinding(receiptRef, version, digest) ||
		!boundary.now().UTC().Before(time.Unix(identity.Elevation.ExpiresAt, 0).UTC()) {
		return ErrElevationRequired
	}
	return boundary.consumeElevation(ctx, writer, identity, authenticated)
}

// consumeElevation first wins the revocation store's one-use record, then rotates a family generation or issues an ordinary cookie and sets its CSRF pair. These are separate operations: a later failure does not undo the consumed record.
// consumeElevation сначала получает одноразовую запись хранилища отзывов, затем меняет поколение семейства либо выдаёт обычную cookie с CSRF. Это отдельные операции: последующая ошибка не отменяет расход.
func (boundary *Boundary) consumeElevation(ctx context.Context, writer http.ResponseWriter, identity Identity, authenticated authenticatedSession) error {
	won, err := boundary.revocations.ConsumeOnce(ctx, identity.BrowserSessionID)
	if err != nil {
		return ErrElevationUnavailable
	}
	if !won {
		return ErrElevationConsumed
	}
	if authenticated.claims.FamilyID != "" {
		if boundary.families == nil {
			return ErrElevationUnavailable
		}
		family, csrf, err := boundary.families.ConsumeElevation(ctx, authenticated.claims.FamilyID, identity.BrowserSessionID)
		if err != nil {
			return ErrElevationUnavailable
		}
		claims, encoded, err := boundary.families.Cookie(family)
		if err != nil {
			return ErrElevationUnavailable
		}
		SetOwnerSessionCookies(writer, claims, encoded, csrf)
		return nil
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

// RevokeSession first revokes the family when present, then persists browser-session revocation; a family failure stops the second step. It does not log out of the identity provider.
// RevokeSession сначала отзывает семейство при его наличии, затем фиксирует отзыв браузерной сессии; ошибка первого шага останавливает второй. Выход из провайдера входа здесь не выполняется.
func (boundary *Boundary) RevokeSession(ctx context.Context, identity Identity) error {
	if identity.FamilyID != "" {
		if boundary.families == nil {
			return ErrSessionValidationUnavailable
		}
		if err := boundary.families.Revoke(ctx, identity.FamilyID); err != nil {
			return ErrSessionValidationUnavailable
		}
	}
	return boundary.revocations.Revoke(ctx, identity.BrowserSessionID)
}

// StopAdmission atomically and irreversibly rejects newly entering middleware requests; it does not cancel handlers already admitted.
// StopAdmission атомарно и необратимо запрещает новые входящие запросы middleware, но не отменяет уже допущенные обработчики.
func (boundary *Boundary) StopAdmission() { boundary.stopping.Store(true) }

// AllowsOrigin performs exact string membership in the constructor-copied origin set, without wildcard or URL normalization.
// AllowsOrigin проверяет точное строковое вхождение в скопированный конструктором набор origins без wildcard и нормализации URL.
func (boundary *Boundary) AllowsOrigin(origin string) bool {
	_, ok := boundary.origins[origin]
	return ok
}

// IdentityFromContext retrieves the boundary-installed identity and reports absence without deriving identity from request headers or payload.
// IdentityFromContext извлекает установленную границей личность и сообщает её отсутствие, не выводя личность из заголовков или тела запроса.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

// ProjectReferenceFromContext returns only a nonempty boundary-installed project selector; its presence does not prove project authorization.
// ProjectReferenceFromContext возвращает только непустой установленный границей селектор проекта; наличие не доказывает право доступа к проекту.
func ProjectReferenceFromContext(ctx context.Context) (string, bool) {
	reference, ok := ctx.Value(projectReferenceContextKey{}).(string)
	return reference, ok && reference != ""
}

// VerifiedAuthorizationFromContext retrieves the verifier result stored for session creation, returning a false flag when that typed context entry is absent.
// VerifiedAuthorizationFromContext извлекает сохранённый для создания сессии результат verifier и возвращает false при отсутствии типизированной записи.
func VerifiedAuthorizationFromContext(ctx context.Context) (oidcauth.Principal, string, bool) {
	verified, ok := ctx.Value(verifiedAuthorizationContextKey{}).(verifiedAuthorization)
	return verified.principal, verified.bearer, ok
}

// VerifyCSRFToken checks a supplied token against the authenticated identity's claim hash; matching the separate request cookie remains the caller's responsibility.
// VerifyCSRFToken сверяет токен с хешем в проверенной личности; совпадение с отдельной cookie запроса проверяет вызывающая сторона.
func VerifyCSRFToken(identity Identity, token string) bool {
	claims := session.Claims{CSRFHash: identity.CSRFHash}
	return session.VerifyCSRF(claims, token)
}

// authenticate opens the cookie, checks revocation and exact family bindings, and re-verifies the current access token. Only an explicit session PUT with allowed Origin/CSRF may renew a family before token verification; returned identity expiry is capped by cookie and bearer.
// authenticate открывает cookie, проверяет отзыв и точные привязки семейства, повторно проверяет текущий access token. Обновить семейство до проверки токена может только явный PUT сессии с допустимыми Origin/CSRF; срок личности ограничен cookie и bearer.
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
	if claims.FamilyID != "" {
		if boundary.families == nil {
			return Identity{}, session.Claims{}, time.Time{}, ErrSessionValidationUnavailable
		}
		family, err := boundary.families.Read(request.Context(), claims.FamilyID)
		if err != nil {
			return Identity{}, session.Claims{}, time.Time{}, err
		}
		if family.BrowserSessionID != claims.SessionID || family.CSRFHash != claims.CSRFHash ||
			family.Principal.Subject != claims.Subject || family.Principal.OrganizationID != claims.OrganizationID ||
			family.Principal.SessionID != claims.OIDCSessionID || family.Principal.SessionRevision != claims.SessionRevision {
			return Identity{}, session.Claims{}, time.Time{}, ErrUnauthenticated
		}
		if request.Method == http.MethodPut && request.URL.Path == "/api/v1/session" {
			if !boundary.AllowsOrigin(request.Header.Get("Origin")) || !boundary.verifyCSRF(request, claims) {
				return Identity{}, session.Claims{}, time.Time{}, ErrSessionCSRFRejected
			}
			family, err = boundary.families.Renew(request.Context(), claims.FamilyID, claims.SessionID, claims.CSRFHash)
			if err != nil {
				return Identity{}, session.Claims{}, time.Time{}, err
			}
		}
		claims.Bearer, claims.Elevation = family.AccessToken, family.Elevation
		claims.ExpiresAt = family.IdleExpiresAt.Unix()
	}
	principal, err := boundary.verifier.VerifyToken(request.Context(), claims.Bearer)
	if errors.Is(err, oidcauth.ErrSigningKeysUnavailable) {
		return Identity{}, session.Claims{}, time.Time{}, err
	}
	if err != nil || principal.Subject != claims.Subject || principal.OrganizationID != claims.OrganizationID || principal.SessionID != claims.OIDCSessionID ||
		principal.SessionRevision != claims.SessionRevision {
		return Identity{}, session.Claims{}, time.Time{}, errors.New("owner session binding is invalid")
	}
	identityExpiry := time.Unix(claims.ExpiresAt, 0).UTC()
	if principal.ExpiresAt.Before(identityExpiry) {
		identityExpiry = principal.ExpiresAt
	}
	return Identity{
		FamilyID: claims.FamilyID,
		Subject:  claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		BrowserSessionID: claims.SessionID, SessionRevision: claims.SessionRevision, CSRFHash: claims.CSRFHash,
		ExpiresAt: identityExpiry, Elevation: claims.Elevation,
	}, claims, principal.ExpiresAt, nil
}

// renewSession rechecks Origin and double-submit CSRF, then emits the current matching family cookie or delegates ordinary cookie renewal. Missing proof is a no-op; backend family refresh occurs earlier in authenticate, not here.
// renewSession повторно проверяет Origin и двойное подтверждение CSRF, затем выдаёт текущую согласованную cookie семейства либо обновляет обычную. Отсутствие доказательства ничего не меняет; серверное обновление семейства выполняется ранее в authenticate.
func (boundary *Boundary) renewSession(writer http.ResponseWriter, request *http.Request, claims session.Claims, bearerExpiry time.Time) (session.Claims, bool, error) {
	origin := request.Header.Get("Origin")
	if origin == "" || !boundary.AllowsOrigin(origin) || !boundary.verifyCSRF(request, claims) {
		return claims, false, nil
	}
	csrfCookie, err := request.Cookie(CSRFCookieName)
	if err != nil {
		return claims, false, nil
	}
	if claims.FamilyID != "" {
		family, err := boundary.families.Read(request.Context(), claims.FamilyID)
		if err != nil {
			return session.Claims{}, false, err
		}
		if family.BrowserSessionID != claims.SessionID || family.CSRFHash != claims.CSRFHash {
			return session.Claims{}, false, ErrUnauthenticated
		}
		renewed, encoded, err := boundary.families.Cookie(family)
		if err != nil {
			return session.Claims{}, false, err
		}
		SetOwnerSessionCookies(writer, renewed, encoded, csrfCookie.Value)
		renewed.Bearer = family.AccessToken
		return renewed, true, nil
	}
	renewedClaims, encoded, renewed, err := boundary.sessions.Renew(claims, bearerExpiry)
	if err != nil || !renewed {
		return renewedClaims, false, err
	}
	SetOwnerSessionCookies(writer, renewedClaims, encoded, csrfCookie.Value)
	return renewedClaims, true, nil
}

// SetOwnerSessionCookies emits no-store, Secure, SameSite=Strict, Path=/ cookie pairs, with HttpOnly only on the session cookie and MaxAge clamped to 1..3600 seconds. It does not validate or renew the supplied claims.
// SetOwnerSessionCookies выдаёт пару cookies с no-store, Secure, SameSite=Strict, Path=/, HttpOnly только у сессионной и MaxAge в пределах 1..3600 секунд. Переданные claims не проверяются и не обновляются.
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

// authenticationProblem distinguishes CSRF rejection (403), known retryable verifier/state/renewal/CAS failures (503), and all other authentication errors (401). It does not expose internal error messages.
// authenticationProblem различает отказ CSRF (403), известные повторяемые ошибки verifier, состояния, обновления и CAS (503) и остальные ошибки входа (401), не раскрывая внутренний текст ошибки.
func authenticationProblem(err error) (int, string, bool) {
	if errors.Is(err, ErrSessionCSRFRejected) {
		return http.StatusForbidden, "CSRF_REJECTED", false
	}
	if errors.Is(err, oidcauth.ErrSigningKeysUnavailable) || errors.Is(err, ErrSessionValidationUnavailable) ||
		errors.Is(err, browserstate.ErrUnavailable) || errors.Is(err, session.ErrRenewalPending) || errors.Is(err, browserstate.ErrConflict) {
		return http.StatusServiceUnavailable, "UNAVAILABLE", true
	}
	return http.StatusUnauthorized, "UNAUTHENTICATED", false
}

// verifyCSRF requires equal-length constant-time equality between header and CSRF cookie, then verifies the header against the authenticated claim hash.
// verifyCSRF требует равную длину и сравнение за постоянное время заголовка с CSRF-cookie, затем сверяет заголовок с хешем проверенных claims.
func (boundary *Boundary) verifyCSRF(request *http.Request, claims session.Claims) bool {
	header := request.Header.Get("X-CSRF-Token")
	cookie, err := request.Cookie(CSRFCookieName)
	if err != nil || len(header) != len(cookie.Value) || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		return false
	}
	return session.VerifyCSRF(claims, header)
}

// isMutation marks only POST, PUT, PATCH and DELETE for Origin/CSRF enforcement; unknown methods are not classified as mutations here.
// isMutation относит только POST, PUT, PATCH и DELETE к изменениям с обязательными Origin/CSRF; неизвестные методы здесь изменениями не считаются.
func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// writeProblem writes a no-store problem+json response with a fresh correlation ID and optional localized title; only 429 adds Retry-After=1. Encoding failure is deliberately ignored after headers are sent.
// writeProblem выдаёт ответ no-store problem+json с новым correlation ID и возможным локализованным заголовком; только 429 добавляет Retry-After=1. Ошибка кодирования после отправки заголовков намеренно игнорируется.
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
