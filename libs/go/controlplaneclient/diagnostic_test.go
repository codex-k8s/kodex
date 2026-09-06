package controlplaneclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type diagnosticResolver struct {
	api.AuthorityProofResolverServiceClient
	err   error
	calls int
}

func (resolver *diagnosticResolver) ResolveAuthorityProof(context.Context, *api.ResolveAuthorityProofRequest, ...grpc.CallOption) (*api.ResolveAuthorityProofResponse, error) {
	resolver.calls++
	return &api.ResolveAuthorityProofResponse{}, resolver.err
}

func TestProofProviderReportsExactPreIssuerFailure(t *testing.T) {
	const secret = "PRIVATE-credential-path-message-sentinel"
	for _, stage := range []authorityclient.DiagnosticStage{authorityclient.StageProofOperation, authorityclient.StageGrantMissing, authorityclient.StageGrantRead, authorityclient.StageProofResolve, authorityclient.StageProofResponse} {
		t.Run(string(stage), func(t *testing.T) {
			resolver := &diagnosticResolver{}
			client := &Client{resolver: resolver, proofOperations: operationSet{"/test": "test"}}
			ctx := t.Context()
			switch stage {
			case authorityclient.StageProofOperation:
				client.proofOperations = nil
			case authorityclient.StageGrantRead:
				client.grantFile = filepath.Join(t.TempDir(), secret)
			case authorityclient.StageProofResolve, authorityclient.StageProofResponse:
				var err error
				ctx, err = WithApplicationGrant(ctx, secret)
				if err != nil {
					t.Fatal(err)
				}
				if stage == authorityclient.StageProofResolve {
					resolver.err = status.Error(codes.PermissionDenied, secret)
				}
			}
			proof, _, err := client.AuthorityProof(ctx, "test", "/test")
			if err == nil || proof != "" || !strings.Contains(err.Error(), "authority_stage="+string(stage)) || strings.Contains(err.Error(), secret) {
				t.Fatal("provider stage or redaction mismatch")
			}
			wantCalls := 0
			if stage == authorityclient.StageProofResolve || stage == authorityclient.StageProofResponse {
				wantCalls = 1
			}
			if resolver.calls != wantCalls {
				t.Fatal("resolver reached before credential validation or repeated")
			}
		})
	}
	// Неверное содержимое существующего файла также остаётся закрытым grant_read.
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte(secret+"\ninvalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &Client{proofOperations: operationSet{"/test": "test"}, grantFile: path}
	_, _, err := client.AuthorityProof(t.Context(), "test", "/test")
	if err == nil || !strings.Contains(err.Error(), "authority_stage=grant_read") || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid credential escaped")
	}
}
