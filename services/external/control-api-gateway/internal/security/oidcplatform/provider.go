// Package oidcplatform binds standard OIDC evidence to typed protected control-plane session authority while retaining single-attempt provider exchanges.
// Package oidcplatform связывает standard OIDC evidence с typed protected control-plane session authority, сохраняя single-attempt provider exchanges.
package oidcplatform

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IdentityVerifier verifies the real standard token without manufacturing application authority.
// IdentityVerifier проверяет реальный standard token без создания application authority.
type IdentityVerifier interface {
	VerifyIdentity(context.Context, string) (oidcverifier.VerifiedIdentity, error)
}

// BrowserProvider owns actual one-shot PKCE/nonce code and refresh exchanges plus exact provider URL construction.
// BrowserProvider владеет реальными one-shot PKCE/nonce code и refresh exchanges и построением exact provider URL.
type BrowserProvider interface {
	AuthorizationURL(string, string, string, bool) (string, error)
	ExchangeIdentityCode(context.Context, string, string, string) (oidcverifier.IdentityBrowserTokens, error)
	RefreshIdentity(context.Context, string) (oidcverifier.IdentityBrowserTokens, error)
}

// Provider composes native evidence, one browser provider and the generated protected self-session client; none of its methods accepts tenant/revision selectors from a request.
// Provider объединяет native evidence, один browser provider и generated protected self-session client; методы не принимают tenant/revision selectors из request.
type Provider struct {
	verifier IdentityVerifier
	browser  BrowserProvider
	sessions cp.OIDCSessionServiceClient
	timeout  time.Duration
}

// New requires all native/typed ports and a bounded RPC budget, performing no login or bootstrap as a readiness shortcut.
// New требует все native/typed ports и bounded RPC budget, не выполняя login и bootstrap для обхода readiness.
func New(verifier IdentityVerifier, browser BrowserProvider, sessions cp.OIDCSessionServiceClient, timeout time.Duration) (*Provider, error) {
	if verifier == nil || browser == nil || sessions == nil || timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return nil, errors.New("platform OIDC provider configuration is invalid")
	}
	return &Provider{verifier: verifier, browser: browser, sessions: sessions, timeout: timeout}, nil
}

// AuthorizationURL always requests fresh authentication for a new platform family so a revoked SID/auth_time pair cannot be silently reopened by SSO.
// Backend refresh retains the established family and does not use this login path.
// AuthorizationURL всегда требует fresh authentication новой platform family, чтобы SSO не переоткрыл revoked SID/auth_time pair.
// Backend refresh сохраняет established family и не использует этот login path.
func (provider *Provider) AuthorizationURL(state, nonce, challenge string, _ bool) (string, error) {
	return provider.browser.AuthorizationURL(state, nonce, challenge, true)
}

// ExchangeCode performs one native checked exchange and resolves current platform binding before any browser family can be created.
// ExchangeCode выполняет один native checked exchange и разрешает current platform binding до создания browser family.
func (provider *Provider) ExchangeCode(ctx context.Context, code, verifier, nonce string) (oidcverifier.BrowserTokens, error) {
	tokens, err := provider.browser.ExchangeIdentityCode(ctx, code, verifier, nonce)
	if err != nil {
		return oidcverifier.BrowserTokens{}, err
	}
	principal, err := provider.resolve(ctx, tokens.AccessToken, tokens.Identity, false)
	if err != nil {
		return oidcverifier.BrowserTokens{}, err
	}
	return oidcverifier.BrowserTokens{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, RefreshExpiresAt: tokens.RefreshExpiresAt, Principal: principal}, nil
}

// Refresh consumes exactly one native provider refresh and maps its result; a failed owner read never replays the rotated refresh token.
// Refresh расходует ровно один native provider refresh и сопоставляет результат; ошибка owner read не повторяет rotated refresh token.
func (provider *Provider) Refresh(ctx context.Context, token string) (oidcverifier.BrowserTokens, error) {
	tokens, err := provider.browser.RefreshIdentity(ctx, token)
	if err != nil {
		return oidcverifier.BrowserTokens{}, err
	}
	principal, err := provider.resolve(ctx, tokens.AccessToken, tokens.Identity, false)
	if err != nil {
		return oidcverifier.BrowserTokens{}, errors.Join(oidcverifier.ErrExchangeUncertain, err)
	}
	return oidcverifier.BrowserTokens{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, RefreshExpiresAt: tokens.RefreshExpiresAt, Principal: principal}, nil
}

// VerifyAuthorization accepts one exact bearer header and returns the unchanged token only after native and durable-owner validation.
// VerifyAuthorization принимает один exact bearer header и возвращает исходный token лишь после native и durable-owner validation.
func (provider *Provider) VerifyAuthorization(ctx context.Context, authorization string) (oidcverifier.Principal, string, error) {
	token := strings.TrimPrefix(authorization, "Bearer ")
	if token == authorization || token == "" || strings.TrimSpace(token) != token {
		return oidcverifier.Principal{}, "", oidcverifier.ErrGrantRejected
	}
	principal, err := provider.VerifyToken(ctx, token)
	if err != nil {
		return oidcverifier.Principal{}, "", err
	}
	return principal, token, nil
}

// VerifyToken requires current application eligibility and session state after real standard signature/issuer/audience/expiry verification.
// VerifyToken требует current application eligibility и session state после реальной standard signature/issuer/audience/expiry verification.
func (provider *Provider) VerifyToken(ctx context.Context, token string) (oidcverifier.Principal, error) {
	identity, err := provider.verifier.VerifyIdentity(ctx, token)
	if err != nil {
		return oidcverifier.Principal{}, err
	}
	return provider.resolve(ctx, token, identity, false)
}

// VerifyClosureToken reads only the signed caller's existing tombstone for the exact logout path; it does not create session or application eligibility.
// VerifyClosureToken читает лишь existing tombstone signed caller для exact logout path; session и application eligibility не создаются.
func (provider *Provider) VerifyClosureToken(ctx context.Context, token string) (oidcverifier.Principal, error) {
	identity, err := provider.verifier.VerifyIdentity(ctx, token)
	if err != nil {
		return oidcverifier.Principal{}, err
	}
	return provider.resolve(ctx, token, identity, true)
}

// resolve sends an empty owner request with the real credential on the protected context and validates the response against independently checked identity.
// resolve отправляет пустой owner request с real credential в protected context и сверяет response с independently checked identity.
func (provider *Provider) resolve(ctx context.Context, token string, identity oidcverifier.VerifiedIdentity, closure bool) (oidcverifier.Principal, error) {
	bounded, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	bound, err := controlplaneclient.WithApplicationGrant(bounded, token)
	if err != nil {
		return oidcverifier.Principal{}, oidcverifier.ErrGrantRejected
	}
	var binding *cp.OIDCSessionBinding
	if closure {
		response, err := provider.sessions.GetOIDCSessionClosure(bound, &cp.GetOIDCSessionClosureRequest{})
		if err != nil {
			return oidcverifier.Principal{}, ownerFailure(err)
		}
		binding = response.GetBinding()
	} else {
		response, err := provider.sessions.ResolveOIDCSession(bound, &cp.ResolveOIDCSessionRequest{})
		if err != nil {
			return oidcverifier.Principal{}, ownerFailure(err)
		}
		binding = response.GetBinding()
	}
	return principalFromBinding(identity, binding, closure)
}

// RevokeIdentitySession sends one naturally idempotent empty self-revoke request and requires an exact revoked readback before family credentials are cleared.
// RevokeIdentitySession отправляет один естественно идемпотентный empty self-revoke request и требует exact revoked readback до очистки family credentials.
func (provider *Provider) RevokeIdentitySession(ctx context.Context, token string, expected oidcverifier.Principal) error {
	identity, err := provider.verifier.VerifyIdentity(ctx, token)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, provider.timeout)
	defer cancel()
	bound, err := controlplaneclient.WithApplicationGrant(bounded, token)
	if err != nil {
		return oidcverifier.ErrGrantRejected
	}
	response, err := provider.sessions.RevokeOIDCSession(bound, &cp.RevokeOIDCSessionRequest{})
	if err != nil {
		return ownerFailure(err)
	}
	principal, err := principalFromBinding(identity, response.GetBinding(), true)
	if err != nil || response.GetBinding().GetState() != cp.OIDCSessionState_OIDC_SESSION_STATE_REVOKED || !expected.PlatformManagedOIDC ||
		principal.OrganizationID != expected.OrganizationID || principal.Subject != expected.Subject || principal.SessionID != expected.SessionID || principal.Issuer != expected.Issuer ||
		principal.SessionRevision != expected.SessionRevision || !principal.AuthenticatedAt.Equal(expected.AuthenticatedAt) || !principal.PlatformSessionExpiresAt.Equal(expected.PlatformSessionExpiresAt) {
		return oidcverifier.ErrGrantRejected
	}
	return nil
}

// principalFromBinding validates the complete closed owner response and immutable identity/lifetime pins without accepting any owner-looking JWT claim.
// principalFromBinding проверяет полный closed owner response и immutable identity/lifetime pins без приёма owner-looking JWT claim.
func principalFromBinding(identity oidcverifier.VerifiedIdentity, binding *cp.OIDCSessionBinding, closure bool) (oidcverifier.Principal, error) {
	var zero oidcverifier.Principal
	if binding == nil || binding.AuthenticatedAt == nil || !binding.AuthenticatedAt.IsValid() || binding.AbsoluteExpiresAt == nil || !binding.AbsoluteExpiresAt.IsValid() {
		return zero, oidcverifier.ErrGrantRejected
	}
	organization, err := uuid.Parse(binding.OrganizationId)
	digest, digestErr := hex.DecodeString(binding.PolicyDigestSha256)
	auth, absolute := binding.AuthenticatedAt.AsTime(), binding.AbsoluteExpiresAt.AsTime()
	if err != nil || organization == uuid.Nil || organization.String() != binding.OrganizationId || binding.Issuer != identity.Issuer || binding.Subject != identity.Subject || binding.SessionId != identity.SessionID ||
		binding.Revision == 0 || binding.Revision > 1<<53-1 || binding.PolicyRevision == 0 || binding.PolicyRevision > 1<<53-1 || digestErr != nil || len(digest) != 32 || hex.EncodeToString(digest) != binding.PolicyDigestSha256 ||
		!auth.Equal(identity.AuthenticatedAt) || !absolute.After(auth) || absolute.After(auth.Add(12*time.Hour)) || !identity.ExpiresAt.After(time.Now()) ||
		!closure && (binding.State != cp.OIDCSessionState_OIDC_SESSION_STATE_ACTIVE || !absolute.After(time.Now())) ||
		closure && binding.State != cp.OIDCSessionState_OIDC_SESSION_STATE_ACTIVE && binding.State != cp.OIDCSessionState_OIDC_SESSION_STATE_REVOKED {
		return zero, oidcverifier.ErrGrantRejected
	}
	expiry := identity.ExpiresAt
	if !closure && absolute.Before(expiry) {
		expiry = absolute
	}
	return oidcverifier.Principal{Subject: identity.Subject, OrganizationID: binding.OrganizationId, Issuer: identity.Issuer, SessionID: identity.SessionID, SessionRevision: binding.Revision,
		PlatformManagedOIDC: true, PlatformSessionExpiresAt: absolute, IssuedAt: identity.IssuedAt, AuthenticatedAt: identity.AuthenticatedAt, ExpiresAt: expiry,
		DisplayName: identity.DisplayName, EmailHint: identity.EmailHint, Groups: slices.Clone(identity.Groups), ACR: identity.ACR, AMR: slices.Clone(identity.AMR)}, nil
}

// ownerFailure exposes only closed availability/rejection classes; raw gRPC details, identity and credential values are not returned or logged.
// ownerFailure выдаёт лишь closed availability/rejection classes; raw gRPC details, identity и credential values не возвращаются и не логируются.
func ownerFailure(err error) error {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition:
		return oidcverifier.ErrGrantRejected
	default:
		return oidcverifier.ErrIdentityStateUnavailable
	}
}
