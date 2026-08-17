package teamprincipal

import (
	"strings"
	"testing"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/google/uuid"
)

func TestOwnerAuthorityRequiresDomainResolvedProject(t *testing.T) {
	t.Parallel()

	provenance := func(source internalrpcauthorityv1.AuthoritySource) *internalrpcauthorityv1.AuthorityProvenance {
		return &internalrpcauthorityv1.AuthorityProvenance{
			Source: source, Revision: 1, DigestSha256: strings.Repeat("a", 64),
		}
	}
	if !validProvenance(provenance(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION),
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION) {
		t.Fatal("verified OIDC actor provenance was rejected")
	}
	if !validProvenance(provenance(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE),
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE) {
		t.Fatal("server-resolved project provenance was rejected")
	}
	if validProvenance(provenance(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION),
		internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE) {
		t.Fatal("caller-carried project provenance was accepted")
	}
}

func TestReadinessAuthorityAllowsTenantScopeWithoutProject(t *testing.T) {
	t.Parallel()
	provenance := func(source internalrpcauthorityv1.AuthoritySource) *internalrpcauthorityv1.AuthorityProvenance {
		return &internalrpcauthorityv1.AuthorityProvenance{Source: source, Revision: 1, DigestSha256: strings.Repeat("a", 64)}
	}
	authority := &internalrpcauthorityv1.CallerAuthority{
		ActorKind: internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN,
		Actor: &internalrpcauthorityv1.AuthorityIdentity{Id: uuid.NewString(),
			Provenance: provenance(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION)},
		Tenant: &internalrpcauthorityv1.AuthorityIdentity{Id: uuid.NewString(),
			Provenance: provenance(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION)},
	}
	principal, err := principalFromAuthority(authority, false)
	if err != nil || principal.ProjectID != "" {
		t.Fatalf("tenant-scoped readiness authority rejected: principal=%+v err=%v", principal, err)
	}
	if _, err := principalFromAuthority(authority, true); err == nil {
		t.Fatal("project-scoped operation accepted authority without project")
	}
}
