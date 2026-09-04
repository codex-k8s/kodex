package projection

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type policyClient struct {
	response *sttv1.ResolveTranscriptionPolicyResponse
}

func (client policyClient) ResolveTranscriptionPolicy(context.Context, *sttv1.ResolveTranscriptionPolicyRequest, ...grpc.CallOption) (*sttv1.ResolveTranscriptionPolicyResponse, error) {
	return client.response, nil
}
func (policyClient) CheckReadiness(context.Context, *sttv1.TranscriptionPolicyProjectionServiceCheckReadinessRequest, ...grpc.CallOption) (*sttv1.TranscriptionPolicyProjectionServiceCheckReadinessResponse, error) {
	return &sttv1.TranscriptionPolicyProjectionServiceCheckReadinessResponse{Ready: true}, nil
}

func TestPolicyProjectionRequiresExactAuthorityEcho(t *testing.T) {
	principal := value.Principal{ActorID: "actor", TenantID: "tenant", ProjectID: "prj_abcdefgh", AuthorityRevision: 4, AuthorityDigestSHA256: strings.Repeat("a", 64)}
	client, err := NewPolicy(policyClient{response: validPolicyResponse(principal)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), principal, "request"); err != nil {
		t.Fatalf("точный authority echo отклонён: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*sttv1.ResolveTranscriptionPolicyResponse)
	}{
		{name: "tenant", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.TenantId = "other-tenant" }},
		{name: "authority revision", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.AuthorityRevision++ }},
		{name: "authority digest", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.AuthorityDigestSha256 = strings.Repeat("b", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := validPolicyResponse(principal)
			test.mutate(response)
			client, err := NewPolicy(policyClient{response: response})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), principal, "request"); err == nil {
				t.Fatal("несовпадающий authority echo принят")
			}
		})
	}
}

func TestPolicyProjectionRejectsUnsignedLimitOverflow(t *testing.T) {
	principal := value.Principal{ActorID: "actor", TenantID: "tenant", ProjectID: "prj_abcdefgh", AuthorityRevision: 4, AuthorityDigestSHA256: strings.Repeat("a", 64)}
	for _, test := range []struct {
		name   string
		mutate func(*sttv1.ResolveTranscriptionPolicyResponse)
	}{
		{name: "maximum audio bytes", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.MaximumAudioBytes = math.MaxUint64 }},
		{name: "maximum audio duration", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.MaximumAudioDurationMilliseconds = math.MaxUint64
		}},
		{name: "provider timeout", mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.ProviderTimeoutMilliseconds = math.MaxUint64
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := validPolicyResponse(principal)
			test.mutate(response)
			client, err := NewPolicy(policyClient{response: response})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), principal, "request"); err == nil {
				t.Fatal("uint64 limit overflow принят")
			}
		})
	}
}

func validPolicyResponse(principal value.Principal) *sttv1.ResolveTranscriptionPolicyResponse {
	return &sttv1.ResolveTranscriptionPolicyResponse{
		RequestId: "request", ActorId: principal.ActorID, TenantId: principal.TenantID,
		ProjectId: principal.ProjectID, AuthorityRevision: principal.AuthorityRevision, AuthorityDigestSha256: principal.AuthorityDigestSHA256,
		MaximumAudioBytes: 1024, MaximumAudioDurationMilliseconds: 1000, ProviderTimeoutMilliseconds: 1000,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}
}
