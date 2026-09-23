package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTrustedTranscriptionLocatorBindsFreshSessionAndSnapshot(t *testing.T) {
	p := value.Principal{ActorID: "11111111-1111-4111-8111-111111111111", AuthorityTenant: "22222222-2222-4222-8222-222222222222",
		CallerWorkload: "stt-tts-service", Permission: "platform.stt.policy.resolve", CredentialRevision: 9}
	snapshot := &sttv1.TrustedTranscriptionAuthority{RpcProfile: "trusted-cluster", RequestId: "33333333-3333-4333-8333-333333333333",
		ActorId: p.ActorID, TenantId: p.AuthorityTenant, CredentialRevision: p.CredentialRevision, Permission: "stt.transcribe",
		ExpiresAt: timestamppb.New(time.Now().Add(20 * time.Second))}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	locator := &sttv1.DelegatedAuthorityLocator{RequestId: snapshot.RequestId, CorrelationId: "synthetic-correlation",
		RootActorId: p.ActorID, TenantId: p.AuthorityTenant, SourceRevision: p.CredentialRevision,
		SourceDigestSha256: hex.EncodeToString(digest[:]), ExpiresAt: snapshot.ExpiresAt}
	if !trustedTranscriptionLocatorMatches(locator, p) {
		t.Fatal("valid trusted snapshot rejected")
	}
	for _, mutate := range []func(*sttv1.DelegatedAuthorityLocator, *value.Principal){
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.RootActorId = "44444444-4444-4444-8444-444444444444"
		},
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.TenantId = "44444444-4444-4444-8444-444444444444"
		},
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) { l.ProjectId = "prj_untrusted" },
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.Actor = &sttv1.AuthorityIdentityProvenance{}
		},
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.SourceDigestSha256 = strings.Repeat("0", 64)
		},
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(l *sttv1.DelegatedAuthorityLocator, _ *value.Principal) {
			l.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute))
		},
		func(_ *sttv1.DelegatedAuthorityLocator, p *value.Principal) { p.CredentialRevision++ },
		func(_ *sttv1.DelegatedAuthorityLocator, p *value.Principal) { p.CallerWorkload = "email-bridge" },
		func(_ *sttv1.DelegatedAuthorityLocator, p *value.Principal) { p.Permission = "organization.manage" },
	} {
		candidate, principal := proto.Clone(locator).(*sttv1.DelegatedAuthorityLocator), p
		mutate(candidate, &principal)
		if trustedTranscriptionLocatorMatches(candidate, principal) {
			t.Fatal("changed authority binding accepted")
		}
	}
}

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
