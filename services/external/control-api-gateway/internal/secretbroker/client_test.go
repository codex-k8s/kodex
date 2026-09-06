package secretbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	auth "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type draftIssuer struct {
	auth.AuthorizationIssuerServiceClient
	request *auth.IssueAuthorizationContextRequest
	denied  bool
}

func (i *draftIssuer) IssueAuthorizationContext(_ context.Context, r *auth.IssueAuthorizationContextRequest, _ ...grpc.CallOption) (*auth.IssueAuthorizationContextResponse, error) {
	i.request = r
	if i.denied {
		return nil, status.Error(codes.PermissionDenied, "fixture denial")
	}
	return &auth.IssueAuthorizationContextResponse{CompactJws: "fixture-context"}, nil
}

type draftProof struct {
	operation, method, digest string
	denied                    bool
}

func (p *draftProof) AuthorityProof(ctx context.Context, operation, method string) (string, string, error) {
	p.operation, p.method = operation, method
	p.digest, _ = authorityclient.RequestDigest(ctx)
	if p.denied {
		return "", "", status.Error(codes.PermissionDenied, "fixture denial")
	}
	return "fixture-proof", "fixture-correlation", nil
}

func TestSecretDraftProtectedClientBindsExactRequestAndRejectsMissingAuthority(t *testing.T) {
	inputs := map[string]proto.Message{
		sb.SecretBrokerService_SaveSecretDraft_FullMethodName:           &sb.SaveSecretDraftRequest{OperationGrant: "fixture-grant", Value: []byte("fixture-value")},
		sb.SecretBrokerService_ValidateSecretDraft_FullMethodName:       &sb.ValidateSecretDraftRequest{OperationGrant: "fixture-grant"},
		sb.SecretBrokerService_PublishSecretDraft_FullMethodName:        &sb.PublishSecretDraftRequest{OperationGrant: "fixture-grant"},
		sb.SecretBrokerService_DiscardSecretDraft_FullMethodName:        &sb.DiscardSecretDraftRequest{OperationGrant: "fixture-grant"},
		sb.SecretBrokerService_CheckSecretDraftReadiness_FullMethodName: &sb.CheckSecretDraftReadinessRequest{},
	}
	for operation, method := range controlplaneclient.SecretDraftGatewayOperations() {
		for _, failure := range []string{"", "proof", "issuer"} {
			t.Run(operation+failure, func(t *testing.T) {
				i := &draftIssuer{denied: failure == "issuer"}
				p := &draftProof{denied: failure == "proof"}
				called := false
				request := inputs[method]
				if request == nil {
					t.Fatal("missing exact RPC request")
				}
				err := draftInterceptor(i, p)(t.Context(), method, request, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
					called = true
					md, _ := metadata.FromOutgoingContext(ctx)
					if len(md.Get(authorityclient.AuthorizationMetadata)) != 1 || md.Get(authorityclient.AuthorizationMetadata)[0] != "fixture-context" {
						t.Fatal("issuer context missing")
					}
					return nil
				})
				if failure != "" {
					if err == nil || called {
						t.Fatal("authority failure reached broker")
					}
					return
				}
				raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
				digest := sha256.Sum256(raw)
				if err != nil || !called || p.operation != operation || p.method != method || p.digest != hex.EncodeToString(digest[:]) || i.request.GetRequestDigestSha256() != p.digest || i.request.GetAuthorityProofCompactJws() != "fixture-proof" {
					t.Fatal("request authority lost exact binding")
				}
			})
		}
	}
	i := &draftIssuer{}
	p := &draftProof{}
	called := false
	err := draftInterceptor(i, p)(t.Context(), sb.SecretBrokerService_RevealSecret_FullMethodName, &sb.RevealSecretRequest{}, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		called = true
		return nil
	})
	if status.Code(err) != codes.PermissionDenied || called || p.operation != "" || i.request != nil {
		t.Fatal("unregistered privileged method entered draft connection")
	}
}
