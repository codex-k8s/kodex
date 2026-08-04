package result

import (
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

func TestResultAuthorityProvenanceRequiresExactLineage(t *testing.T) {
	t.Parallel()
	claims := integrationgatewayauth.Claims{
		TurnID: "turn-2", Attempt: 3, GrantGeneration: 7, InputSHA256: "input-digest",
	}
	identity := &internalrpcauthorityv1.AuthorityIdentity{Provenance: &internalrpcauthorityv1.AuthorityProvenance{
		Source:    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_INTEGRATION_CONTINUATION,
		Reference: "turn-2/3/7", Revision: 7, DigestSha256: "input-digest",
	}}
	if !matchesResultAuthorityProvenance(identity, claims) {
		t.Fatal("exact result authority provenance was rejected")
	}
	for name, mutate := range map[string]func(*internalrpcauthorityv1.AuthorityProvenance){
		"source": func(value *internalrpcauthorityv1.AuthorityProvenance) {
			value.Source = internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AGENT_SESSION
		},
		"turn":       func(value *internalrpcauthorityv1.AuthorityProvenance) { value.Reference = "turn-3/3/7" },
		"generation": func(value *internalrpcauthorityv1.AuthorityProvenance) { value.Revision = 8 },
		"input":      func(value *internalrpcauthorityv1.AuthorityProvenance) { value.DigestSha256 = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *identity.GetProvenance()
			mutate(&copy)
			if matchesResultAuthorityProvenance(
				&internalrpcauthorityv1.AuthorityIdentity{Provenance: &copy}, claims,
			) {
				t.Fatal("mismatched result authority provenance was accepted")
			}
		})
	}
}
