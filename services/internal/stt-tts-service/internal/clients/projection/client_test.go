package projection

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type allowBinder struct{ calls int }

func (binder *allowBinder) BindDelegated(ctx context.Context, _ value.Principal, _, _, _, _ string) (context.Context, error) {
	binder.calls++
	return ctx, nil
}

type denyBinder struct{}

func (denyBinder) BindDelegated(context.Context, value.Principal, string, string, string, string) (context.Context, error) {
	return nil, errs.ErrDelegatedProofPending
}

type policyClient struct {
	calls  int
	mutate func(*sttv1.ResolveTranscriptionPolicyResponse)
}

func (client *policyClient) ResolveTranscriptionPolicy(_ context.Context, request *sttv1.ResolveTranscriptionPolicyRequest, _ ...grpc.CallOption) (*sttv1.ResolveTranscriptionPolicyResponse, error) {
	client.calls++
	response := validPolicyResponse(request.GetAuthority())
	if client.mutate != nil {
		client.mutate(response)
	}
	return response, nil
}

type credentialClient struct{ calls int }

func (client *credentialClient) ProjectTranscriptionCredential(_ context.Context, request *sttv1.ProjectTranscriptionCredentialRequest, _ ...grpc.CallOption) (*sttv1.ProjectTranscriptionCredentialResponse, error) {
	client.calls++
	return &sttv1.ProjectTranscriptionCredentialResponse{
		ApiKey: []byte("test-only-key"), ProviderAccountRef: request.GetProviderAccountRef(),
		ProviderCredentialGeneration: request.GetProviderCredentialGeneration(), ConfigDigestSha256: request.GetConfigDigestSha256(),
		ConfigRevision: request.GetConfigRevision(), ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second)),
		Authority: proto.Clone(request.GetAuthority()).(*sttv1.DelegatedAuthorityLocator),
	}, nil
}

func TestPolicyProjectionRequiresExactAuthorityEcho(t *testing.T) {
	principal := validPrincipal()
	for _, test := range []struct {
		name      string
		mutate    func(*sttv1.ResolveTranscriptionPolicyResponse)
		wantError bool
	}{
		{name: "exact", wantError: false},
		{name: "tenant", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.TenantId = "other" }},
		{name: "source revision", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.SourceRevision++ }},
		{name: "actor provenance", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.Actor.Reference = "other" }},
		{name: "correlation", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.CorrelationId = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, binder := &policyClient{mutate: test.mutate}, &allowBinder{}
			client, err := NewPolicy(raw, binder)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Resolve(t.Context(), principal, principal.RequestID, "correlation")
			if (err != nil) != test.wantError {
				t.Fatalf("result = %v", err)
			}
			if raw.calls != 1 || binder.calls != 1 {
				t.Fatal("нарушен единичный projection path")
			}
		})
	}
}

func TestPolicyProjectionRejectsUnsignedLimitOverflow(t *testing.T) {
	for _, mutate := range []func(*sttv1.ResolveTranscriptionPolicyResponse){
		func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.MaximumAudioBytes = math.MaxUint64 },
		func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.MaximumAudioDurationMilliseconds = math.MaxUint64
		},
		func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.ProviderTimeoutMilliseconds = math.MaxUint64
		},
	} {
		raw, binder := &policyClient{mutate: mutate}, &allowBinder{}
		client, _ := NewPolicy(raw, binder)
		if _, err := client.Resolve(t.Context(), validPrincipal(), validPrincipal().RequestID, "correlation"); err == nil {
			t.Fatal("uint64 limit overflow принят")
		}
	}
}

func TestProjectionRequiresDelegatedProofBeforeRPC(t *testing.T) {
	raw := &policyClient{}
	client, _ := NewPolicy(raw, denyBinder{})
	_, err := client.Resolve(t.Context(), validPrincipal(), validPrincipal().RequestID, "correlation")
	if !errors.Is(err, errs.ErrDelegatedProofPending) || raw.calls != 0 {
		t.Fatalf("outbound RPC не закрыт до proof: calls=%d err=%v", raw.calls, err)
	}
}

func TestCredentialProjectionUsesSameAuthorityLocator(t *testing.T) {
	principal := validPrincipal()
	policy := value.Policy{Revision: 7, DigestSHA256: strings.Repeat("b", 64), ProviderAccountRef: "account", ProviderCredentialGeneration: 3}
	raw, binder := &credentialClient{}, &allowBinder{}
	client, _ := NewCredential(raw, binder)
	credential, err := client.Project(t.Context(), principal, principal.RequestID, "correlation", policy)
	if err != nil || raw.calls != 1 || binder.calls != 1 || credential.ProviderCredentialGeneration != 3 {
		t.Fatalf("credential projection result: %#v, %v", credential, err)
	}
}

func validPolicyResponse(authority *sttv1.DelegatedAuthorityLocator) *sttv1.ResolveTranscriptionPolicyResponse {
	return &sttv1.ResolveTranscriptionPolicyResponse{
		ConfigRevision: 7, ConfigDigestSha256: strings.Repeat("b", 64), Model: value.DefaultModel, Language: value.DefaultLanguage,
		MaximumAudioBytes: 1024, MaximumAudioDurationMilliseconds: 1000, ProviderTimeoutMilliseconds: 1000,
		ProviderAccountRef: "account", ProviderCredentialGeneration: 3,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), Authority: proto.Clone(authority).(*sttv1.DelegatedAuthorityLocator),
	}
}

func validPrincipal() value.Principal {
	provenance := value.AuthorityProvenance{Source: int32(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE), Reference: "source", Revision: 2, DigestSHA256: strings.Repeat("a", 64)}
	return value.Principal{
		ActorID: "actor", TenantID: "tenant", ProjectID: "prj_abcdefgh", Actor: provenance, Tenant: provenance, Project: provenance,
		RequestID: "95ec3bbc-a719-4de0-9a44-c9790c659c66", AuthorityRevision: 4,
		AuthorityDigestSHA256: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Minute),
	}
}
