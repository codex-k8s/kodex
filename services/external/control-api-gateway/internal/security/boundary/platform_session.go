package boundary

import (
	"context"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/availability"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

// platformSessionOwner is available only in the explicitly selected platform mode and exposes self-closure, never an arbitrary actor/tenant API.
// platformSessionOwner существует лишь в явно выбранном platform mode и предоставляет self-closure, а не произвольный actor/tenant API.
type platformSessionOwner interface {
	VerifyClosureToken(context.Context, string) (oidcverifier.Principal, error)
	RevokeIdentitySession(context.Context, string, oidcverifier.Principal) error
}

// isPlatformLogout selects the sole HTTP action permitted to reconcile a pending or confirmed platform-session closure.
// isPlatformLogout выбирает единственное HTTP action, которому разрешена сверка pending либо confirmed platform-session closure.
func isPlatformLogout(request *http.Request) bool {
	return request != nil && request.URL != nil && request.Method == http.MethodDelete && request.URL.Path == "/api/v1/session"
}

// IssueSessionContext retains legacy issuance and creates an encrypted, access-only family for a verified platform API bearer without fake refresh credentials.
// Sensitive-purpose checks remain server-side and run before family creation.
// IssueSessionContext сохраняет legacy issuance и создаёт encrypted access-only family для verified platform API bearer без fake refresh credentials.
// Sensitive-purpose checks остаются серверными и выполняются до family creation.
func (boundary *Boundary) IssueSessionContext(ctx context.Context, principal oidcverifier.Principal, bearer string, purpose *SessionPurpose) (session.Claims, string, string, error) {
	if !principal.PlatformManagedOIDC {
		return boundary.IssueSession(principal, bearer, purpose)
	}
	if ctx == nil || boundary.families == nil {
		return session.Claims{}, "", "", ErrSessionValidationUnavailable
	}
	if _, ok := boundary.verifier.(platformSessionOwner); !ok {
		return session.Claims{}, "", "", ErrUnauthenticated
	}
	elevation, err := boundary.sessionElevation(principal, purpose)
	if err != nil {
		return session.Claims{}, "", "", err
	}
	family, csrf, err := boundary.families.CreateBearerWithCSRF(ctx, principal, bearer, elevation)
	if err != nil {
		return session.Claims{}, "", "", err
	}
	claims, cookie, err := boundary.families.Cookie(family)
	if err != nil {
		return session.Claims{}, "", "", err
	}
	return claims, cookie, csrf, nil
}

// authenticatePlatformClosure accepts only a sealed mode-bound family cookie on exact DELETE, matching all server-side browser/identity/CSRF bindings.
// Pending closure still requires the real valid bearer; confirmed receipts permit only local logout completion, with no application grant attached.
// authenticatePlatformClosure принимает лишь sealed mode-bound family cookie на exact DELETE, сверяя все server-side browser/identity/CSRF bindings.
// Pending closure всё ещё требует real valid bearer; confirmed receipt разрешает лишь local logout completion без application grant.
func (boundary *Boundary) authenticatePlatformClosure(request *http.Request, claims session.Claims) (Identity, session.Claims, time.Time, error) {
	var zero Identity
	if !isPlatformLogout(request) || !claims.PlatformManagedOIDC || claims.FamilyID == "" || boundary.families == nil || request.Header.Get(ProjectReferenceHeader) != "" || request.URL.RawQuery != "" {
		return zero, session.Claims{}, time.Time{}, ErrUnauthenticated
	}
	owner, ok := boundary.verifier.(platformSessionOwner)
	if !ok {
		return zero, session.Claims{}, time.Time{}, ErrUnauthenticated
	}
	family, err := boundary.families.ReadForClosure(request.Context(), claims.FamilyID)
	if err != nil {
		return zero, session.Claims{}, time.Time{}, availability.At("auth_family_read", err)
	}
	if family.BrowserSessionID != claims.SessionID || family.CSRFHash != claims.CSRFHash || family.Principal.Subject != claims.Subject || family.Principal.OrganizationID != claims.OrganizationID ||
		family.Principal.SessionID != claims.OIDCSessionID || family.Principal.SessionRevision != claims.SessionRevision || !family.Principal.PlatformManagedOIDC {
		return zero, session.Claims{}, time.Time{}, ErrUnauthenticated
	}
	identity := Identity{FamilyID: claims.FamilyID, Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID, BrowserSessionID: claims.SessionID,
		SessionRevision: claims.SessionRevision, CSRFHash: claims.CSRFHash, ExpiresAt: time.Unix(claims.ExpiresAt, 0)}
	if family.OwnerRevokeConfirmed {
		identity.closureComplete = true
		claims.Bearer = ""
		return identity, claims, identity.ExpiresAt, nil
	}
	principal, err := owner.VerifyClosureToken(request.Context(), family.AccessToken)
	if err != nil {
		return zero, session.Claims{}, time.Time{}, availability.At("auth_oidc_verify", err)
	}
	if !principal.PlatformManagedOIDC || principal.Issuer != family.Principal.Issuer || principal.Subject != claims.Subject || principal.OrganizationID != claims.OrganizationID || principal.SessionID != claims.OIDCSessionID ||
		principal.SessionRevision != claims.SessionRevision || !principal.AuthenticatedAt.Equal(family.Principal.AuthenticatedAt) ||
		!principal.PlatformSessionExpiresAt.Equal(family.Principal.PlatformSessionExpiresAt) {
		return zero, session.Claims{}, time.Time{}, ErrUnauthenticated
	}
	claims.Bearer, claims.Elevation = family.AccessToken, nil
	if principal.ExpiresAt.Before(identity.ExpiresAt) {
		identity.ExpiresAt = principal.ExpiresAt
	}
	return identity, claims, principal.ExpiresAt, nil
}

// revokePlatformSession persists a local refresh fence, requires exact owner revoke confirmation, then closes family and browser revocation state.
// Any unknown step remains fail-closed and rejoins the same tuple on retry; provider code/refresh exchanges are never replayed.
// revokePlatformSession сохраняет local refresh fence, требует exact owner revoke confirmation и закрывает family и browser revocation state.
// Неизвестный шаг остаётся fail-closed и присоединяется к тому же tuple при retry; provider code/refresh exchanges не повторяются.
func (boundary *Boundary) revokePlatformSession(ctx context.Context, identity Identity, owner platformSessionOwner) error {
	if boundary.families == nil || identity.FamilyID == "" {
		return ErrSessionValidationUnavailable
	}
	current, err := boundary.families.ReadForClosure(ctx, identity.FamilyID)
	if err != nil {
		return availability.At("auth_family_read", err)
	}
	if current.Principal.Subject != identity.Subject || current.Principal.OrganizationID != identity.OrganizationID || current.Principal.SessionID != identity.SessionID || current.Principal.SessionRevision != identity.SessionRevision {
		return ErrUnauthenticated
	}
	family, err := boundary.families.BeginPlatformRevoke(ctx, identity.FamilyID, identity.BrowserSessionID, identity.CSRFHash)
	if err != nil {
		return availability.At("auth_family_read", err)
	}
	if !family.OwnerRevokeConfirmed {
		if err := owner.RevokeIdentitySession(ctx, family.AccessToken, family.Principal); err != nil {
			return availability.At("auth_oidc_verify", err)
		}
		if err := boundary.families.CompletePlatformRevoke(ctx, identity.FamilyID, identity.BrowserSessionID, identity.CSRFHash, identity.SessionRevision); err != nil {
			return ErrSessionValidationUnavailable
		}
	}
	return boundary.revocations.Revoke(ctx, identity.BrowserSessionID)
}
