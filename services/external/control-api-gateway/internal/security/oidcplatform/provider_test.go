package oidcplatform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nativeFixture separates native verification and one-shot browser effects from independently returned owner authority.
// nativeFixture отделяет native verification и one-shot browser effects от независимо возвращаемой owner authority.
type nativeFixture struct {
	identity                             oidcverifier.VerifiedIdentity
	failure                              error
	verifyCalls, codeCalls, refreshCalls int
	fresh                                bool
}

// VerifyIdentity records native admission and returns only the configured standard evidence or native failure.
// VerifyIdentity считает native admission и возвращает лишь заданные standard evidence либо native failure.
func (fixture *nativeFixture) VerifyIdentity(context.Context, string) (oidcverifier.VerifiedIdentity, error) {
	fixture.verifyCalls++
	return fixture.identity, fixture.failure
}

// AuthorizationURL captures fresh-authentication intent without contacting any real identity provider.
// AuthorizationURL сохраняет fresh-authentication intent без обращения к реальному identity provider.
func (fixture *nativeFixture) AuthorizationURL(_, _, _ string, fresh bool) (string, error) {
	fixture.fresh = fresh
	return "https://identity.example.invalid/authorize", nil
}

// ExchangeIdentityCode represents one already verified PKCE/nonce exchange; it never supplies application authority fields.
// ExchangeIdentityCode представляет один уже проверенный PKCE/nonce exchange без application authority fields.
func (fixture *nativeFixture) ExchangeIdentityCode(context.Context, string, string, string) (oidcverifier.IdentityBrowserTokens, error) {
	fixture.codeCalls++
	return fixture.tokens(), fixture.failure
}

// RefreshIdentity counts rotated-token effects separately, allowing tests to prove that owner failures do not replay them.
// RefreshIdentity считает rotated-token effects отдельно, позволяя доказать отсутствие повторов при owner failures.
func (fixture *nativeFixture) RefreshIdentity(context.Context, string) (oidcverifier.IdentityBrowserTokens, error) {
	fixture.refreshCalls++
	return fixture.tokens(), fixture.failure
}

// tokens returns synthetic server-only credentials and checked evidence with no fake organization, owner flag or session revision.
// tokens возвращает synthetic server-only credentials и checked evidence без fake organization, owner flag и session revision.
func (fixture *nativeFixture) tokens() oidcverifier.IdentityBrowserTokens {
	return oidcverifier.IdentityBrowserTokens{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", RefreshExpiresAt: fixture.identity.AuthenticatedAt.Add(time.Hour), Identity: fixture.identity}
}

// ownerFixture implements only the generated typed self-session API and tracks each exact read/write route and bounded context.
// ownerFixture реализует лишь generated typed self-session API и считает exact read/write routes и bounded context.
type ownerFixture struct {
	binding                                 *cp.OIDCSessionBinding
	failure                                 error
	resolveCalls, closureCalls, revokeCalls int
	bounded                                 bool
}

// ResolveOIDCSession returns independently configured active owner authority; its empty request cannot choose another actor or tenant.
// ResolveOIDCSession возвращает независимо заданную active owner authority; пустой request не выбирает другого actor или tenant.
func (fixture *ownerFixture) ResolveOIDCSession(ctx context.Context, _ *cp.ResolveOIDCSessionRequest, _ ...grpc.CallOption) (*cp.ResolveOIDCSessionResponse, error) {
	fixture.resolveCalls++
	_, fixture.bounded = ctx.Deadline()
	return &cp.ResolveOIDCSessionResponse{Binding: fixture.binding}, fixture.failure
}

// GetOIDCSessionClosure serves the separate read-only tombstone path and never increments the mutation counter.
// GetOIDCSessionClosure обслуживает отдельный read-only tombstone path и не увеличивает mutation counter.
func (fixture *ownerFixture) GetOIDCSessionClosure(ctx context.Context, _ *cp.GetOIDCSessionClosureRequest, _ ...grpc.CallOption) (*cp.GetOIDCSessionClosureResponse, error) {
	fixture.closureCalls++
	_, fixture.bounded = ctx.Deadline()
	return &cp.GetOIDCSessionClosureResponse{Binding: fixture.binding}, fixture.failure
}

// RevokeOIDCSession represents one exact owner command; its result remains separate from the read-only authentication path.
// RevokeOIDCSession представляет одну exact owner command; её результат отделён от read-only authentication path.
func (fixture *ownerFixture) RevokeOIDCSession(ctx context.Context, _ *cp.RevokeOIDCSessionRequest, _ ...grpc.CallOption) (*cp.RevokeOIDCSessionResponse, error) {
	fixture.revokeCalls++
	_, fixture.bounded = ctx.Deadline()
	return &cp.RevokeOIDCSessionResponse{Binding: fixture.binding}, fixture.failure
}

// providerFixture constructs disjoint standard evidence and durable owner authority using only synthetic identifiers and endpoints.
// providerFixture создаёт раздельные standard evidence и durable owner authority только с synthetic identifiers и endpoints.
func providerFixture(t *testing.T) (*Provider, *nativeFixture, *ownerFixture) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	native := &nativeFixture{identity: oidcverifier.VerifiedIdentity{Issuer: "https://identity.example.invalid/realm/synthetic", Subject: "20000000-0000-4000-8000-000000000001",
		SessionID: "30000000-0000-4000-8000-000000000001", TokenID: "40000000-0000-4000-8000-000000000001", IssuedAt: now,
		AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(5 * time.Minute), Groups: []string{"synthetic.readers"}}}
	owner := &ownerFixture{binding: &cp.OIDCSessionBinding{Issuer: native.identity.Issuer, Subject: native.identity.Subject, SessionId: native.identity.SessionID,
		OrganizationId: "50000000-0000-4000-8000-000000000001", Revision: 27, AuthenticatedAt: timestamppb.New(native.identity.AuthenticatedAt),
		AbsoluteExpiresAt: timestamppb.New(now.Add(time.Hour - time.Minute)), PolicyRevision: 1, PolicyDigestSha256: strings.Repeat("a", 64), State: cp.OIDCSessionState_OIDC_SESSION_STATE_ACTIVE}}
	provider, err := New(native, native, owner, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return provider, native, owner
}

// TestPlatformProviderUsesOwnerAuthority proves native evidence precedes exact bounded owner lookup and the principal carries no first-user owner claim.
// TestPlatformProviderUsesOwnerAuthority проверяет порядок native evidence → exact bounded owner lookup и отсутствие first-user owner claim.
func TestPlatformProviderUsesOwnerAuthority(t *testing.T) {
	provider, native, owner := providerFixture(t)
	principal, token, err := provider.VerifyAuthorization(t.Context(), "Bearer synthetic-access")
	if err != nil || token != "synthetic-access" || !principal.PlatformManagedOIDC || principal.OwnerClaim || principal.SessionRevision != 27 || principal.OrganizationID != owner.binding.OrganizationId ||
		!principal.PlatformSessionExpiresAt.Equal(owner.binding.AbsoluteExpiresAt.AsTime()) || native.verifyCalls != 1 || owner.resolveCalls != 1 || !owner.bounded || owner.closureCalls != 0 || owner.revokeCalls != 0 {
		t.Fatal("standard identity bypassed independent bounded owner authority")
	}
	native.failure = oidcverifier.ErrSigningKeysUnavailable
	if _, err := provider.VerifyToken(t.Context(), "synthetic-invalid"); !errors.Is(err, oidcverifier.ErrSigningKeysUnavailable) || owner.resolveCalls != 1 {
		t.Fatal("failed native verification reached the owner")
	}
}

// TestPlatformProviderBrowserEffectsAreSingleAttempt proves fresh login and no automatic refresh/code replay after owner transport failure.
// TestPlatformProviderBrowserEffectsAreSingleAttempt проверяет fresh login и отсутствие автоматического refresh/code replay при owner transport failure.
func TestPlatformProviderBrowserEffectsAreSingleAttempt(t *testing.T) {
	provider, native, owner := providerFixture(t)
	if _, err := provider.AuthorizationURL("state", "nonce", "challenge", false); err != nil || !native.fresh {
		t.Fatal("new family silently reused a revoked authentication tuple")
	}
	tokens, err := provider.ExchangeCode(t.Context(), "synthetic-code", "synthetic-verifier", "synthetic-nonce")
	if err != nil || tokens.Principal.SessionRevision != 27 || native.codeCalls != 1 {
		t.Fatal("checked code exchange did not bind owner session")
	}
	owner.failure = status.Error(codes.Unavailable, "SYNTHETIC_PRIVATE_DETAIL")
	if _, err := provider.Refresh(t.Context(), "synthetic-refresh"); !errors.Is(err, oidcverifier.ErrExchangeUncertain) || !errors.Is(err, oidcverifier.ErrIdentityStateUnavailable) || native.refreshCalls != 1 || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_DETAIL") {
		t.Fatal("rotated refresh was replayed or private transport detail escaped")
	}
}

// TestPlatformProviderClosureDoesNotAuthorizeApplication allows only the dedicated tombstone read and requires exact revoked command readback.
// TestPlatformProviderClosureDoesNotAuthorizeApplication допускает лишь dedicated tombstone read и требует exact revoked command readback.
func TestPlatformProviderClosureDoesNotAuthorizeApplication(t *testing.T) {
	provider, _, owner := providerFixture(t)
	active, err := provider.VerifyToken(t.Context(), "synthetic-access")
	if err != nil {
		t.Fatal(err)
	}
	owner.binding.State = cp.OIDCSessionState_OIDC_SESSION_STATE_REVOKED
	if _, err := provider.VerifyToken(t.Context(), "synthetic-access"); err == nil {
		t.Fatal("revoked tombstone admitted an application call")
	}
	if _, err := provider.VerifyClosureToken(t.Context(), "synthetic-access"); err != nil || owner.closureCalls != 1 || owner.revokeCalls != 0 {
		t.Fatal("closure authentication mutated state or lost its tombstone read")
	}
	if err := provider.RevokeIdentitySession(t.Context(), "synthetic-access", active); err != nil || owner.revokeCalls != 1 {
		t.Fatal("exact revoked command readback was not accepted")
	}
	foreign := active
	foreign.OrganizationID = "50000000-0000-4000-8000-000000000099"
	if err := provider.RevokeIdentitySession(t.Context(), "synthetic-access", foreign); err == nil {
		t.Fatal("foreign family accepted another tenant's revoke receipt")
	}
}

// TestPlatformProviderRejectsResponseConfusion covers wrong identity, missing fields, unbounded revisions, mode-state confusion and unavailable owner classification.
// TestPlatformProviderRejectsResponseConfusion покрывает чужую identity, пропущенные fields, unbounded revisions, mode-state confusion и unavailable owner classification.
func TestPlatformProviderRejectsResponseConfusion(t *testing.T) {
	for _, change := range []func(*cp.OIDCSessionBinding){
		func(v *cp.OIDCSessionBinding) { v.Subject = "20000000-0000-4000-8000-000000000099" },
		func(v *cp.OIDCSessionBinding) { v.SessionId = "30000000-0000-4000-8000-000000000099" },
		func(v *cp.OIDCSessionBinding) { v.Issuer = "https://other.example.invalid" },
		func(v *cp.OIDCSessionBinding) { v.OrganizationId = "invalid" },
		func(v *cp.OIDCSessionBinding) { v.Revision = 0 }, func(v *cp.OIDCSessionBinding) { v.Revision = 1 << 53 },
		func(v *cp.OIDCSessionBinding) { v.PolicyRevision = 0 }, func(v *cp.OIDCSessionBinding) { v.PolicyDigestSha256 = "invalid" },
		func(v *cp.OIDCSessionBinding) { v.AuthenticatedAt = nil }, func(v *cp.OIDCSessionBinding) { v.AbsoluteExpiresAt = timestamppb.New(time.Now().Add(-time.Second)) },
		func(v *cp.OIDCSessionBinding) { v.State = cp.OIDCSessionState_OIDC_SESSION_STATE_UNSPECIFIED },
	} {
		provider, _, owner := providerFixture(t)
		change(owner.binding)
		if _, err := provider.VerifyToken(t.Context(), "synthetic-access"); err == nil {
			t.Fatal("inconsistent owner response established application authority")
		}
	}
	provider, _, owner := providerFixture(t)
	owner.failure = status.Error(codes.PermissionDenied, "SYNTHETIC_PRIVATE_DETAIL")
	if _, err := provider.VerifyToken(t.Context(), "synthetic-access"); !errors.Is(err, oidcverifier.ErrGrantRejected) {
		t.Fatal("denied owner response was treated as available authority")
	}
}
