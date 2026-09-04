package grpc

import (
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProjectedAPIKeyAcceptsOnlyExactAPIKeyDocument(t *testing.T) {
	t.Parallel()
	key, err := projectedAPIKey([]byte(`{"OPENAI_API_KEY":"synthetic-key","auth_mode":"apikey"}`))
	if err != nil || string(key) != "synthetic-key" {
		t.Fatalf("valid API key projection failed: key=%q err=%v", key, err)
	}
	clear(key)
	for _, raw := range [][]byte{
		[]byte(`{"OPENAI_API_KEY":"synthetic-key","auth_mode":"oauth"}`),
		[]byte(`{"OPENAI_API_KEY":"synthetic-key","auth_mode":"apikey","extra":"value"}`),
		[]byte("{\"OPENAI_API_KEY\":\"synthetic-key\",\"auth_mode\":\"apikey\"}\n{}"),
		[]byte(`{"OPENAI_API_KEY":" short ","auth_mode":"apikey"}`),
	} {
		if value, err := projectedAPIKey(raw); err == nil {
			clear(value)
			t.Fatalf("invalid provider credential was projected: %s", raw)
		}
	}
}

func TestDelegatedAuthorityLocatorRequiresExactVerifiedBinding(t *testing.T) {
	t.Parallel()
	verified, locator := projectionAuthorityFixtures()
	if !sameDelegatedAuthorityLocator(locator, verified) {
		t.Fatal("exact delegated locator was rejected")
	}
	mutations := []func(*sttv1.DelegatedAuthorityLocator){
		func(value *sttv1.DelegatedAuthorityLocator) { value.ProjectId = "35cd6c54-9bd6-43d4-a17a-5553cc893a5d" },
		func(value *sttv1.DelegatedAuthorityLocator) { value.SourceRevision++ },
		func(value *sttv1.DelegatedAuthorityLocator) { value.Actor.Reference = "different-reference" },
		func(value *sttv1.DelegatedAuthorityLocator) {
			value.ExpiresAt = timestamppb.New(value.ExpiresAt.AsTime().Add(time.Second))
		},
	}
	for _, mutate := range mutations {
		_, candidate := projectionAuthorityFixtures()
		mutate(candidate)
		if sameDelegatedAuthorityLocator(candidate, verified) {
			t.Fatalf("changed delegated locator was accepted: %#v", candidate)
		}
	}
}

func projectionAuthorityFixtures() (*internalrpcauthorityv1.VerifiedAuthorizationContext, *sttv1.DelegatedAuthorityLocator) {
	expiresAt := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	actor := projectionIdentity("c20ac176-c0ca-499f-91a4-6fc65c4ef30e", "actor-reference", 'a')
	tenant := projectionIdentity("71adb021-5229-4903-9f75-9fd34797665a", "tenant-reference", 'b')
	project := projectionIdentity("e92277a1-c5d0-4d40-af73-54c34a256ef5", "project-reference", 'c')
	verified := &internalrpcauthorityv1.VerifiedAuthorizationContext{
		Authority:      &internalrpcauthorityv1.CallerAuthority{Actor: actor, Tenant: tenant, Project: project},
		SourceRevision: 9, SourceDigestSha256: projectionHex('d'), ExpiresAt: timestamppb.New(expiresAt),
	}
	return verified, &sttv1.DelegatedAuthorityLocator{
		RequestId: "9671137c-0288-4446-803e-f3c2d13dcbe8", CorrelationId: "stt-request:1",
		RootActorId: actor.GetId(), TenantId: tenant.GetId(), ProjectId: project.GetId(),
		SourceRevision: verified.GetSourceRevision(), SourceDigestSha256: verified.GetSourceDigestSha256(),
		Actor: projectionLocatorProvenance(actor), Tenant: projectionLocatorProvenance(tenant), Project: projectionLocatorProvenance(project),
		ExpiresAt: timestamppb.New(expiresAt),
	}
}

func projectionIdentity(id, reference string, digestCharacter byte) *internalrpcauthorityv1.AuthorityIdentity {
	return &internalrpcauthorityv1.AuthorityIdentity{Id: id, Provenance: &internalrpcauthorityv1.AuthorityProvenance{
		Source:    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE,
		Reference: reference, Revision: 3, DigestSha256: projectionHex(digestCharacter),
	}}
}

func projectionLocatorProvenance(identity *internalrpcauthorityv1.AuthorityIdentity) *sttv1.AuthorityIdentityProvenance {
	value := identity.GetProvenance()
	return &sttv1.AuthorityIdentityProvenance{Source: int32(value.GetSource()), Reference: value.GetReference(), Revision: value.GetRevision(), DigestSha256: value.GetDigestSha256()}
}

func projectionHex(character byte) string {
	value := make([]byte, 64)
	for index := range value {
		value[index] = character
	}
	return string(value)
}
