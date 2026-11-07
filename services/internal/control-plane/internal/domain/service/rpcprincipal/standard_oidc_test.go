package rpcprincipal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// standardOwnerFixture separates signature evidence from independently allocated application authority and captures admitted caller selectors.
// standardOwnerFixture отделяет signature evidence от независимо назначенных application authority и сохраняет admitted caller selectors.
type standardOwnerFixture struct {
	*ownerFixture
	binding  platformrepo.StandardOIDCBinding
	identity platformrepo.StandardOIDCIdentity
	failure  error
}

// ResolveStandardOIDCIdentity records the typed evidence before returning an independent session binding or safe owner failure.
// ResolveStandardOIDCIdentity сохраняет typed evidence и возвращает независимый session binding либо safe owner failure.
func (fixture *standardOwnerFixture) ResolveStandardOIDCIdentity(_ context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	fixture.identity = input
	return fixture.binding, fixture.failure
}

// RevokeStandardOIDCSession is unavailable in this resolution-only fixture; ordinary authentication may not issue a revoke command.
// RevokeStandardOIDCSession недоступен в resolution-only fixture; обычная аутентификация не выпускает revoke command.
func (*standardOwnerFixture) RevokeStandardOIDCSession(context.Context, string, string, uint64) error {
	return errs.ErrForbidden
}

// standardCredentialFixture records native standard verification separately from the embedded legacy verifier counter.
// standardCredentialFixture считает native standard verification отдельно от счётчика embedded legacy verifier.
type standardCredentialFixture struct {
	*credentialFixture
	identity      oidcverifier.VerifiedIdentity
	identityCalls int
	failure       error
}

// VerifyIdentity returns synthetic checked evidence and counts its use; failure never permits an automatic legacy verification attempt.
// VerifyIdentity возвращает synthetic checked evidence и считает вызовы; failure не разрешает автоматическую legacy verification attempt.
func (fixture *standardCredentialFixture) VerifyIdentity(context.Context, string) (oidcverifier.VerifiedIdentity, error) {
	fixture.identityCalls++
	return fixture.identity, fixture.failure
}

// TestPlatformManagedRPCPrincipal propagates transport admission and database revision into the ordinary protected application principal without custom owner claims.
// TestPlatformManagedRPCPrincipal передаёт transport admission и database revision в обычный protected application principal без custom owner claims.
func TestPlatformManagedRPCPrincipal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	identity := oidcverifier.VerifiedIdentity{Issuer: "https://identity.example.invalid/realm/synthetic", Subject: "20000000-0000-4000-8000-000000000001",
		SessionID: "30000000-0000-4000-8000-000000000001", TokenID: "40000000-0000-4000-8000-000000000001", IssuedAt: now, AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	owner := &standardOwnerFixture{ownerFixture: &ownerFixture{resolved: &platformrepo.ProofAuthority{ActorID: "50000000-0000-4000-8000-000000000001", OrganizationID: "60000000-0000-4000-8000-000000000001"}},
		binding: platformrepo.StandardOIDCBinding{ActorID: "50000000-0000-4000-8000-000000000001", OrganizationID: "60000000-0000-4000-8000-000000000001", Issuer: identity.Issuer,
			Subject: identity.Subject, SessionID: identity.SessionID, Revision: 27, State: "ACTIVE", AuthenticatedAt: identity.AuthenticatedAt, AbsoluteExpiry: now.Add(time.Hour)}}
	credentials := &standardCredentialFixture{credentialFixture: &credentialFixture{}, identity: identity}
	service, err := NewWithAuthorityMode(owner, credentials, oidcverifier.PlatformManagedAuthorityMode)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Admission: serviceidentity.Admission{RPCProfile: "service-v1", TargetSPIFFEID: target, Peer: serviceidentity.PeerIdentity{SPIFFEID: gateway},
		OperationID: "platform.query.bootstrap.get", Permission: "platform.query.bootstrap.get", ActorMode: serviceidentity.UserActor}, Authorization: "Bearer synthetic", RequestDigestSHA256: strings.Repeat("a", 64)}
	principal, err := service.Resolve(t.Context(), input)
	if err != nil || principal.CredentialRevision != 27 || principal.ActorID != owner.binding.ActorID || owner.identity.CallerWorkload != "control-api-gateway" || owner.identity.RPCProfile != "service-v1" ||
		owner.identity.Operation != input.Admission.OperationID || !owner.input.PlatformManagedOIDC || owner.input.OwnerClaim || owner.input.ExternalSessionID != identity.SessionID ||
		!owner.input.ExternalIssuedAt.Equal(identity.IssuedAt) || credentials.calls != 0 || credentials.identityCalls != 1 {
		t.Fatalf("standard principal lost admitted identity/session authority: %v", err)
	}
	for _, failure := range []error{errs.ErrForbidden, errs.ErrUnavailable} {
		owner.failure = failure
		if _, err := service.Resolve(t.Context(), input); !errors.Is(err, failure) || credentials.calls != 0 || owner.calls != 1 {
			t.Fatal("owner failure fell back to legacy claims or application proof resolution")
		}
	}
}

// TestPlatformManagedRPCRequiresNativePorts rejects unknown modes and missing standard verifier/owner ports before request admission.
// TestPlatformManagedRPCRequiresNativePorts отклоняет неизвестные modes и отсутствие standard verifier/owner ports до request admission.
func TestPlatformManagedRPCRequiresNativePorts(t *testing.T) {
	for _, mode := range []oidcverifier.AuthorityMode{"unknown", oidcverifier.PlatformManagedAuthorityMode} {
		if _, err := NewWithAuthorityMode(&ownerFixture{}, &credentialFixture{}, mode); err == nil {
			t.Fatal("unsupported mode or absent standard authority ports were accepted")
		}
	}
}
