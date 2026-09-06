package boundary

import (
	"context"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

const LoginCookieName = "__Host-kodex-login"

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

func (boundary *Boundary) BeginAuthorization(ctx context.Context, purpose *SessionPurpose, fresh bool) (string, string, error) {
	if boundary.logins == nil || boundary.families == nil {
		return "", "", ErrSessionValidationUnavailable
	}
	return boundary.logins.Start(ctx, purpose, fresh)
}

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
	// Повтор callback не восстанавливает family после расхода elevation/CSRF.
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

func (boundary *Boundary) familyMetadata(family session.Family) SessionMetadata {
	now := boundary.now().UTC()
	renew := boundary.families.RenewAfter(family)
	return SessionMetadata{Generation: family.BrowserSessionID, Version: family.Version, SessionRevision: family.Principal.SessionRevision,
		ServerTime: now, ExpiresAt: family.IdleExpiresAt, AccessExpiresAt: family.Principal.ExpiresAt,
		AbsoluteExpiresAt: family.AbsoluteExpiresAt, RenewAfter: renew, BackendRefresh: true}
}
