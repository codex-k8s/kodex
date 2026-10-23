package boundary

import (
	"context"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

// LoginCookieName names the host-only transient browser-login binding cookie, distinct from authenticated session and CSRF cookies.
// LoginCookieName задаёт имя временной host-only cookie привязки браузерного входа, отдельной от cookies проверенной сессии и CSRF.
const LoginCookieName = "__Host-kodex-login"

// IssueWebSocketTicket requires a context identity with a family and delegates ticket issuance using its exact family/browser/CSRF bindings; it does not open a socket.
// IssueWebSocketTicket требует личность контекста с семейством и выдаёт ticket по точным family/browser/CSRF-привязкам; сокет не открывается.
func (boundary *Boundary) IssueWebSocketTicket(ctx context.Context) (string, time.Time, error) {
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity.FamilyID == "" || boundary.families == nil {
		return "", time.Time{}, ErrUnauthenticated
	}
	return boundary.families.IssueWebSocketTicket(ctx, identity.FamilyID, identity.BrowserSessionID, identity.CSRFHash)
}

// ConsumeWebSocketTicket delegates one-use verification against the authenticated family/browser/CSRF tuple and fails closed without family support.
// ConsumeWebSocketTicket передаёт одноразовую проверку по проверенным family/browser/CSRF и закрыто отказывает без поддержки семейств.
func (boundary *Boundary) ConsumeWebSocketTicket(ctx context.Context, token string) error {
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity.FamilyID == "" || boundary.families == nil {
		return ErrUnauthenticated
	}
	return boundary.families.ConsumeWebSocketTicket(ctx, identity.FamilyID, identity.BrowserSessionID, identity.CSRFHash, token)
}

// SessionMetadata exposes token-free generation/version, server time and renewal/expiry ceilings. BackendRefresh distinguishes durable-family metadata from ordinary cookie metadata.
// SessionMetadata предоставляет поколение, версию, серверное время и границы обновления без токенов. BackendRefresh отличает метаданные устойчивого семейства от обычной cookie.
type SessionMetadata struct {
	Generation        string
	Version           uint64
	SessionRevision   uint64
	ServerTime        time.Time
	ExpiresAt         time.Time
	AccessExpiresAt   time.Time
	AbsoluteExpiresAt time.Time
	RenewAfter        time.Time
	BackendRefresh    bool
}

// BeginAuthorization starts a server-owned login transaction only when both login and family support exist, forwarding the exact purpose and fresh-login flag.
// BeginAuthorization запускает серверную транзакцию входа только при наличии поддержки входов и семейств, передавая точные цель и признак свежего входа.
func (boundary *Boundary) BeginAuthorization(ctx context.Context, purpose *SessionPurpose, fresh bool) (string, string, error) {
	if boundary.logins == nil || boundary.families == nil {
		return "", "", ErrSessionValidationUnavailable
	}
	return boundary.logins.Start(ctx, purpose, fresh)
}

// CompleteAuthorization validates the binding-cookie shape, exchanges the login once, creates or reloads its family, verifies token identity and current CSRF, and rejects revocation before returning cookies and metadata. Failures after family creation do not transactionally undo earlier writes.
// CompleteAuthorization проверяет формат binding-cookie, обменивает вход один раз, создаёт либо читает семейство, проверяет личность токена и текущий CSRF, отклоняет отзыв перед возвратом cookies и метаданных. Ошибка после создания семейства не откатывает предыдущие записи общей транзакцией.
func (boundary *Boundary) CompleteAuthorization(ctx context.Context, cookie, state, code string) (session.Claims, string, string, SessionMetadata, error) {
	if boundary.logins == nil || boundary.families == nil {
		return session.Claims{}, "", "", SessionMetadata{}, ErrSessionValidationUnavailable
	}
	id, binding, ok := strings.Cut(cookie, ".")
	if !ok || len(cookie) != 80 {
		return session.Claims{}, "", "", SessionMetadata{}, ErrUnauthenticated
	}
	transaction, tokens, err := boundary.logins.Exchange(ctx, id, binding, state, code)
	if err != nil {
		return session.Claims{}, "", "", SessionMetadata{}, err
	}
	var family session.Family
	csrf := transaction.CSRF
	if transaction.FamilyID != "" {
		family, err = boundary.families.Read(ctx, transaction.FamilyID)
	} else {
		var elevation *session.Elevation
		elevation, err = boundary.sessionElevation(tokens.Principal, transaction.Purpose)
		if err == nil {
			family, csrf, err = boundary.families.CreateWithCSRF(ctx, tokens, elevation)
		}
		if err == nil {
			err = boundary.logins.Complete(ctx, transaction, family.ID, csrf)
		}
	}
	if err != nil {
		return session.Claims{}, "", "", SessionMetadata{}, err
	}
	principal, err := boundary.verifier.VerifyToken(ctx, family.AccessToken)
	if err != nil || principal.Subject != family.Principal.Subject || principal.OrganizationID != family.Principal.OrganizationID ||
		principal.SessionID != family.Principal.SessionID || principal.SessionRevision != family.Principal.SessionRevision {
		return session.Claims{}, "", "", SessionMetadata{}, ErrUnauthenticated
	}
	// Replayed callbacks must match the current family CSRF and cannot restore a consumed elevation or older browser generation.
	// Повтор callback должен совпадать с текущим CSRF семейства и не восстанавливает израсходованное повышение либо старое поколение браузера.
	claims, encoded, err := boundary.families.Cookie(family)
	if err != nil || !session.VerifyCSRF(claims, csrf) {
		return session.Claims{}, "", "", SessionMetadata{}, ErrUnauthenticated
	}
	revoked, err := boundary.revocations.Revoked(ctx, claims.SessionID)
	if err != nil || revoked {
		return session.Claims{}, "", "", SessionMetadata{}, ErrUnauthenticated
	}
	return claims, encoded, csrf, boundary.familyMetadata(family), nil
}

// SessionMetadata reads authenticated context, rechecks current family generation/CSRF when applicable, and otherwise computes the ordinary cookie's two-thirds renewal point. It returns no bearer or refresh token.
// SessionMetadata читает проверенный контекст, при наличии семейства сверяет текущее поколение и CSRF, иначе вычисляет обновление обычной cookie на двух третях срока. Bearer и refresh token не возвращаются.
func (boundary *Boundary) SessionMetadata(ctx context.Context) (SessionMetadata, error) {
	value, ok := ctx.Value(authenticatedSessionContextKey{}).(authenticatedSession)
	if !ok {
		return SessionMetadata{}, ErrUnauthenticated
	}
	if value.claims.FamilyID != "" {
		if boundary.families == nil {
			return SessionMetadata{}, ErrSessionValidationUnavailable
		}
		family, err := boundary.families.Read(ctx, value.claims.FamilyID)
		if err != nil {
			return SessionMetadata{}, err
		}
		if family.BrowserSessionID != value.claims.SessionID || family.CSRFHash != value.claims.CSRFHash {
			return SessionMetadata{}, ErrUnauthenticated
		}
		return boundary.familyMetadata(family), nil
	}
	expires := time.Unix(value.claims.ExpiresAt, 0).UTC()
	issued := time.Unix(value.claims.IssuedAt, 0).UTC()
	return SessionMetadata{Generation: value.claims.SessionID, Version: 1, SessionRevision: value.claims.SessionRevision,
		ServerTime: boundary.now().UTC(), ExpiresAt: expires, AccessExpiresAt: value.bearerExpiry,
		AbsoluteExpiresAt: value.bearerExpiry, RenewAfter: issued.Add(expires.Sub(issued) * 2 / 3)}, nil
}

// familyMetadata projects the supplied family into token-free browser metadata using the boundary clock and family renewal policy; it does not reload or validate state.
// familyMetadata проецирует переданное семейство в браузерные метаданные без токенов, используя часы границы и правило обновления семейства; состояние не перечитывается и не проверяется.
func (boundary *Boundary) familyMetadata(family session.Family) SessionMetadata {
	now := boundary.now().UTC()
	renew := boundary.families.RenewAfter(family)
	return SessionMetadata{Generation: family.BrowserSessionID, Version: family.Version, SessionRevision: family.Principal.SessionRevision,
		ServerTime: now, ExpiresAt: family.IdleExpiresAt, AccessExpiresAt: family.Principal.ExpiresAt,
		AbsoluteExpiresAt: family.AbsoluteExpiresAt, RenewAfter: renew, BackendRefresh: true}
}
