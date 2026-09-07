package grpc

import (
	"strings"
	"testing"
	"time"

	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTranscriptionLocatorAcceptsBoundedContinuationDeadline(t *testing.T) {
	now := time.Now()
	locator, verified := transcriptionAuthorityFixture(now.Add(30*time.Second), now.Add(20*time.Second))
	if !transcriptionLocatorMatches(locator, verified) {
		t.Fatal("bounded continuation deadline was rejected")
	}

	verified.ExpiresAt = timestamppb.New(now.Add(31 * time.Second))
	if transcriptionLocatorMatches(locator, verified) {
		t.Fatal("continuation deadline expanded root authority")
	}

	verified.ExpiresAt = timestamppb.New(now.Add(20 * time.Second))
	verified.Authority.Actor.Provenance.DigestSha256 = strings.Repeat("c", 64)
	if transcriptionLocatorMatches(locator, verified) {
		t.Fatal("changed actor provenance was accepted")
	}
}

func transcriptionAuthorityFixture(locatorExpiry, verifiedExpiry time.Time) (*sttv1.DelegatedAuthorityLocator, *authorityv1.VerifiedAuthorizationContext) {
	const actorID = "1c70bbfd-c5db-401f-9332-ed6008d78a22"
	const tenantID = "a3038aa6-8ab4-4e7c-980a-42a55d2464e6"
	digest := strings.Repeat("a", 64)
	actor := &authorityv1.AuthorityProvenance{Source: authorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION, Reference: "actor", Revision: 3, DigestSha256: digest}
	tenant := &authorityv1.AuthorityProvenance{Source: authorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE, Reference: "tenant", Revision: 4, DigestSha256: digest}
	return &sttv1.DelegatedAuthorityLocator{
			RequestId: "b05d9c6e-9d1b-4ae5-842a-b741662a18b0", CorrelationId: "stt-policy-test",
			RootActorId: actorID, TenantId: tenantID, SourceRevision: 7, SourceDigestSha256: digest,
			Actor:     &sttv1.AuthorityIdentityProvenance{Source: int32(actor.Source), Reference: actor.Reference, Revision: actor.Revision, DigestSha256: actor.DigestSha256},
			Tenant:    &sttv1.AuthorityIdentityProvenance{Source: int32(tenant.Source), Reference: tenant.Reference, Revision: tenant.Revision, DigestSha256: tenant.DigestSha256},
			ExpiresAt: timestamppb.New(locatorExpiry),
		}, &authorityv1.VerifiedAuthorizationContext{
			ExpiresAt: verifiedExpiryTimestamp(verifiedExpiry), SourceRevision: 7, SourceDigestSha256: digest,
			Authority: &authorityv1.CallerAuthority{
				Actor:  &authorityv1.AuthorityIdentity{Id: actorID, Provenance: actor},
				Tenant: &authorityv1.AuthorityIdentity{Id: tenantID, Provenance: tenant},
			},
		}
}

func verifiedExpiryTimestamp(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value)
}
