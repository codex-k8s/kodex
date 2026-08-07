package teamprincipal

import (
	"strings"
	"testing"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
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
