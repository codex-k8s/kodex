package oidcprincipal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// identityFixture supplies synthetic already-verified evidence and a separate injected verification failure to test ordering.
// identityFixture задаёт synthetic already-verified evidence и отдельный verification failure для проверки порядка действий.
type identityFixture struct {
	identity oidcverifier.VerifiedIdentity
	failure  error
}

// VerifyIdentity returns only the configured evidence; real cryptographic verification belongs to the native oidcverifier tests.
// VerifyIdentity возвращает лишь заданные evidence; реальную криптографию проверяют native oidcverifier tests.
func (fixture *identityFixture) VerifyIdentity(context.Context, string) (oidcverifier.VerifiedIdentity, error) {
	return fixture.identity, fixture.failure
}

// ownerFixture records exact verified identity and transport selectors without sharing its returned authority with the token fixture.
// ownerFixture сохраняет точные verified identity и transport selectors, отделяя returned authority от token fixture.
type ownerFixture struct {
	input   platformrepo.StandardOIDCIdentity
	binding platformrepo.StandardOIDCBinding
	failure error
	calls   int
}

// ResolveStandardOIDCIdentity records the owner-bound request and returns independent synthetic authority or the injected domain failure.
// ResolveStandardOIDCIdentity сохраняет owner-bound request и возвращает независимые synthetic authority либо domain failure.
func (fixture *ownerFixture) ResolveStandardOIDCIdentity(_ context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	fixture.input = input
	fixture.calls++
	return fixture.binding, fixture.failure
}

// RevokeStandardOIDCSession is deliberately unavailable in the mapping fixture; identity projection never calls a revocation command.
// RevokeStandardOIDCSession намеренно недоступен в mapping fixture; проекция identity не вызывает команду revoke.
func (*ownerFixture) RevokeStandardOIDCSession(context.Context, string, string, uint64) error {
	return errs.ErrForbidden
}

// standardPrincipalFixture separates identity evidence, independently assigned tenant/session authority and admitted caller selectors.
// standardPrincipalFixture отделяет identity evidence, независимо назначенные tenant/session authority и admitted caller selectors.
func standardPrincipalFixture() (*identityFixture, *ownerFixture, platformrepo.ProofPrincipalInput) {
	now := time.Now().UTC().Truncate(time.Second)
	identity := oidcverifier.VerifiedIdentity{Issuer: "https://identity.example.invalid/realm/synthetic",
		Subject: "20000000-0000-4000-8000-000000000001", SessionID: "30000000-0000-4000-8000-000000000001",
		TokenID: "40000000-0000-4000-8000-000000000001", AuthenticatedAt: now.Add(-time.Minute), IssuedAt: now,
		ExpiresAt: now.Add(10 * time.Minute), Groups: []string{"synthetic.readers"}}
	owner := &ownerFixture{binding: platformrepo.StandardOIDCBinding{Issuer: identity.Issuer, Subject: identity.Subject, SessionID: identity.SessionID,
		OrganizationID: "50000000-0000-4000-8000-000000000001", ActorID: "60000000-0000-4000-8000-000000000001",
		Revision: 27, AuthenticatedAt: identity.AuthenticatedAt, AbsoluteExpiry: now.Add(5 * time.Minute), State: "ACTIVE"}}
	return &identityFixture{identity: identity}, owner, platformrepo.ProofPrincipalInput{CallerWorkload: "control-api-gateway", Operation: "platform.query.bootstrap.get", RPCProfile: "service-v1"}
}

// TestStandardPrincipalOwnerBoundary proves owner-only organization/revision, exact caller propagation, identity binding and the shorter session lifetime.
// TestStandardPrincipalOwnerBoundary проверяет owner-only organization/revision, точную передачу caller, identity binding и меньший session lifetime.
func TestStandardPrincipalOwnerBoundary(t *testing.T) {
	verifier, owner, caller := standardPrincipalFixture()
	principal, err := Resolve(t.Context(), verifier, owner, caller, "synthetic-verified-credential")
	if err != nil || !principal.PlatformManagedOIDC || principal.OwnerClaim || principal.OrganizationID != owner.binding.OrganizationID ||
		principal.SessionRevision != 27 || !principal.ExpiresAt.Equal(owner.binding.AbsoluteExpiry) || owner.input.CallerWorkload != caller.CallerWorkload ||
		owner.input.Operation != caller.Operation || owner.input.RPCProfile != caller.RPCProfile || owner.input.Subject != verifier.identity.Subject {
		t.Fatal("standard evidence bypassed owner authority or lost its admitted caller binding")
	}
	principal.Groups[0] = "changed"
	owner.input.Groups[0] = "changed"
	if verifier.identity.Groups[0] != "synthetic.readers" {
		t.Fatal("returned state aliased verifier-owned identity evidence")
	}
}

// TestStandardPrincipalRejectsOwnerMismatch rejects foreign issuer/subject/session, invented revision, expiry and revoked owner results.
// TestStandardPrincipalRejectsOwnerMismatch отклоняет чужие issuer/subject/session, выдуманную revision, expiry и revoked owner results.
func TestStandardPrincipalRejectsOwnerMismatch(t *testing.T) {
	for _, change := range []func(*platformrepo.StandardOIDCBinding){
		func(b *platformrepo.StandardOIDCBinding) { b.Issuer = "https://other.example.invalid" },
		func(b *platformrepo.StandardOIDCBinding) { b.Subject = "20000000-0000-4000-8000-000000000002" },
		func(b *platformrepo.StandardOIDCBinding) { b.SessionID = "30000000-0000-4000-8000-000000000002" },
		func(b *platformrepo.StandardOIDCBinding) { b.Revision = 0 },
		func(b *platformrepo.StandardOIDCBinding) { b.Revision = 1 << 53 },
		func(b *platformrepo.StandardOIDCBinding) { b.State = "REVOKED" },
		func(b *platformrepo.StandardOIDCBinding) { b.AbsoluteExpiry = time.Now().Add(-time.Second) },
		func(b *platformrepo.StandardOIDCBinding) { b.AuthenticatedAt = b.AuthenticatedAt.Add(time.Second) },
		func(b *platformrepo.StandardOIDCBinding) { b.OrganizationID = "caller-selected-organization" },
	} {
		verifier, owner, caller := standardPrincipalFixture()
		change(&owner.binding)
		if _, err := Resolve(t.Context(), verifier, owner, caller, "synthetic-verified-credential"); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("inconsistent owner binding became an authorized principal")
		}
	}
}

// TestStandardPrincipalVerificationAndOwnerFailures proves no owner mutation before cryptographic acceptance and preserves unavailable/forbidden classification.
// TestStandardPrincipalVerificationAndOwnerFailures проверяет отсутствие owner mutation до криптографического допуска и сохранение unavailable/forbidden.
func TestStandardPrincipalVerificationAndOwnerFailures(t *testing.T) {
	verifier, owner, caller := standardPrincipalFixture()
	verifier.failure = oidcverifier.ErrSigningKeysUnavailable
	if _, err := Resolve(t.Context(), verifier, owner, caller, "synthetic-invalid-credential"); !errors.Is(err, oidcverifier.ErrSigningKeysUnavailable) || owner.calls != 0 {
		t.Fatal("failed signature verification reached the state owner")
	}
	verifier.failure = nil
	for _, failure := range []error{errs.ErrUnavailable, errs.ErrForbidden} {
		owner.failure = failure
		if _, err := Resolve(t.Context(), verifier, owner, caller, "synthetic-verified-credential"); !errors.Is(err, failure) {
			t.Fatal("owner failure was hidden or converted into fallback authority")
		}
	}
}
