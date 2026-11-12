package grpc

import (
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestProjectedAPIKeyAcceptsOnlyExactAPIKeyDocument rejects legacy, foreign, duplicate and malformed secret envelopes before projection.
// TestProjectedAPIKeyAcceptsOnlyExactAPIKeyDocument отклоняет старые, чужие, дублированные и повреждённые документы до выдачи ключа.
func TestProjectedAPIKeyAcceptsOnlyExactAPIKeyDocument(t *testing.T) {
	t.Parallel()
	valid := `{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-openai","API_KEY":"synthetic-key"}`
	key, err := projectedAPIKey([]byte(valid))
	if err != nil || string(key) != "synthetic-key" {
		t.Fatal("valid API key projection failed", err)
	}
	clear(key)
	for _, raw := range []string{
		`{"OPENAI_API_KEY":"synthetic-key","auth_mode":"apikey"}`,
		`{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-foreign","API_KEY":"synthetic-key"}`,
		`{"auth_mode":"gateway-api-key","provider_definition_key":"opencode-openai","API_KEY":"synthetic-key","API_KEY":"other-key"}`,
		valid + `{}`, strings.Replace(valid, "synthetic-key", " short ", 1),
		strings.Replace(valid, `"API_KEY":`, `"unknown":true,"API_KEY":`, 1),
	} {
		if value, err := projectedAPIKey([]byte(raw)); err == nil {
			clear(value)
			t.Fatal("invalid provider credential was projected")
		}
	}
}

// TestDelegatedAuthorityLocatorRequiresExactVerifiedBinding mutates project/source revision/provenance or
// shortens the parent below verified expiry and requires comparison refusal without verifying a signature.
// TestDelegatedAuthorityLocatorRequiresExactVerifiedBinding меняет project/source revision/provenance либо
// сокращает parent ниже verified expiry и требует отказа сравнения без проверки signature.
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
			value.ExpiresAt = timestamppb.New(value.ExpiresAt.AsTime().Add(-time.Second))
		},
	}
	for _, mutate := range mutations {
		candidate := proto.Clone(locator).(*sttv1.DelegatedAuthorityLocator)
		mutate(candidate)
		if sameDelegatedAuthorityLocator(candidate, verified) {
			t.Fatalf("changed delegated locator was accepted: %#v", candidate)
		}
	}
}

// TestDelegatedAuthorityLocatorAcceptsBoundedContinuation permits a shortened verified expiry with or
// without project and rejects missing/invalid/past/extended continuation timestamps.
// TestDelegatedAuthorityLocatorAcceptsBoundedContinuation допускает shortened verified expiry с project
// либо без него и отклоняет missing/invalid/past/extended continuation timestamps.
func TestDelegatedAuthorityLocatorAcceptsBoundedContinuation(t *testing.T) {
	t.Parallel()
	for _, projectScoped := range []bool{false, true} {
		verified, locator := projectionAuthorityFixtures()
		if !projectScoped {
			verified.Authority.Project = nil
			locator.ProjectId, locator.Project = "", nil
		}
		parentExpiry := locator.ExpiresAt.AsTime()
		verified.ExpiresAt = timestamppb.New(parentExpiry.Add(-20 * time.Second))
		if !sameDelegatedAuthorityLocator(locator, verified) {
			t.Fatal("shortened continuation was rejected")
		}
		for _, expiry := range []*timestamppb.Timestamp{
			nil,
			{Seconds: 253402300800},
			timestamppb.New(time.Now().Add(-time.Second)),
			timestamppb.New(parentExpiry.Add(time.Nanosecond)),
		} {
			verified.ExpiresAt = expiry
			if sameDelegatedAuthorityLocator(locator, verified) {
				t.Fatal("invalid or extended continuation was accepted")
			}
		}
	}
}

// TestOrganizationProjectionScopeIsMethodSpecific rejects orphan project provenance and checks the
// runtime/assistant/STT absent-versus-present project matrix on synthetic identities.
// TestOrganizationProjectionScopeIsMethodSpecific отклоняет orphan project provenance и проверяет
// runtime/assistant/STT матрицу absent/present project на synthetic identities.
func TestOrganizationProjectionScopeIsMethodSpecific(t *testing.T) {
	verified, locator := projectionAuthorityFixtures()
	project := verified.Authority.Project
	verified.Authority.Project = nil
	locator.ProjectId, locator.Project = "", nil
	if !sameDelegatedAuthorityLocator(locator, verified) {
		t.Fatal("organization locator rejected")
	}
	locator.Project = projectionLocatorProvenance(project)
	if sameDelegatedAuthorityLocator(locator, verified) {
		t.Fatal("project provenance without project accepted")
	}
	for _, test := range []struct {
		method          string
		absent, present bool
	}{
		{secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName, false, true},
		{secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName, true, false},
		{sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName, true, true},
	} {
		if validProjectionProject(test.method, nil) != test.absent || validProjectionProject(test.method, project) != test.present {
			t.Fatalf("projection scope matrix mismatch: %s", test.method)
		}
	}
}

// projectionAuthorityFixtures creates matching synthetic identities/provenance/locator with a short future
// parent expiry; it does not create a middleware-verified proof or sign a token.
// projectionAuthorityFixtures создаёт совпадающие synthetic identities/provenance/locator с коротким
// future parent expiry; middleware-verified proof и подпись token не создаёт.
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

// projectionIdentity builds one caller-selected synthetic identity with DOMAIN_STATE provenance and a
// repeated-character digest, not a domain lookup.
// projectionIdentity создаёт одну caller-selected synthetic identity с DOMAIN_STATE provenance и
// repeated-character digest, не выполняя domain lookup.
func projectionIdentity(id, reference string, digestCharacter byte) *internalrpcauthorityv1.AuthorityIdentity {
	return &internalrpcauthorityv1.AuthorityIdentity{Id: id, Provenance: &internalrpcauthorityv1.AuthorityProvenance{
		Source:    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE,
		Reference: reference, Revision: 3, DigestSha256: projectionHex(digestCharacter),
	}}
}

// projectionLocatorProvenance copies identity provenance fields into the STT locator DTO without signing
// or validating them.
// projectionLocatorProvenance копирует identity provenance fields в STT locator DTO без подписи и
// проверки.
func projectionLocatorProvenance(identity *internalrpcauthorityv1.AuthorityIdentity) *sttv1.AuthorityIdentityProvenance {
	value := identity.GetProvenance()
	return &sttv1.AuthorityIdentityProvenance{Source: int32(value.GetSource()), Reference: value.GetReference(), Revision: value.GetRevision(), DigestSha256: value.GetDigestSha256()}
}

// projectionHex repeats a supplied byte 64 times for fixtures; callers choose hexadecimal bytes where a
// valid digest shape is required.
// projectionHex повторяет переданный byte 64 раза для fixtures; caller выбирает hexadecimal bytes, когда
// нужна valid digest shape.
func projectionHex(character byte) string {
	value := make([]byte, 64)
	for index := range value {
		value[index] = character
	}
	return string(value)
}
