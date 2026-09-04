package projection

import (
	"context"
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
	response := &sttv1.ResolveTranscriptionPolicyResponse{RequestId: "request", ActorId: principal.ActorID, TenantId: principal.TenantID,
		ProjectId: principal.ProjectID, AuthorityRevision: principal.AuthorityRevision, AuthorityDigestSha256: principal.AuthorityDigestSHA256,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	client, err := NewPolicy(policyClient{response: response})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), principal, "request"); err != nil {
		t.Fatal(err)
	}
	response.TenantId = "other-tenant"
	if _, err := client.Resolve(t.Context(), principal, "request"); err == nil {
		t.Fatal("несовпадающий tenant echo принят")
	}
}
