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

// draftIssuer embeds the unused generated interface and records the actual issuance request, optionally returning a fixture PermissionDenied. It returns a marker context, not a signed production token.
// draftIssuer встраивает неиспользуемый generated interface и записывает фактический issuance request с необязательным fixture PermissionDenied. Возвращается marker context, а не подписанный production token.
type draftIssuer struct {
	auth.AuthorizationIssuerServiceClient
	request *auth.IssueAuthorizationContextRequest
	denied  bool
}

// IssueAuthorizationContext captures the request pointer before applying configured fixture refusal and otherwise returns a fixed context marker without validation, signing or replay persistence.
// IssueAuthorizationContext сохраняет request pointer до настроенного fixture refusal и иначе возвращает фиксированный context marker без validation, подписи и replay persistence.
func (i *draftIssuer) IssueAuthorizationContext(_ context.Context, r *auth.IssueAuthorizationContextRequest, _ ...grpc.CallOption) (*auth.IssueAuthorizationContextResponse, error) {
	i.request = r
	if i.denied {
		return nil, status.Error(codes.PermissionDenied, "fixture denial")
	}
	return &auth.IssueAuthorizationContextResponse{CompactJws: "fixture-context"}, nil
}

// draftProof captures the requested operation/method and context-bound request digest and can deny resolution. It does not establish project ownership or create a real authority proof.
// draftProof сохраняет запрошенные operation и method и context-bound request digest и может отказать в resolution. Project ownership не устанавливается и настоящий authority proof не создаётся.
type draftProof struct {
	operation, method, digest string
	denied                    bool
}

// AuthorityProof records the operation/method and any bound digest, then returns synthetic proof/correlation or PermissionDenied. Ignoring a missing digest here leaves the enclosing unit responsible for asserting exact binding.
// AuthorityProof записывает operation, method и доступный bound digest, затем возвращает synthetic proof и correlation либо PermissionDenied. Отсутствующий digest здесь игнорируется, а точную binding проверяет основной unit.
func (p *draftProof) AuthorityProof(ctx context.Context, operation, method string) (string, string, error) {
	p.operation, p.method = operation, method
	p.digest, _ = authorityclient.RequestDigest(ctx)
	if p.denied {
		return "", "", status.Error(codes.PermissionDenied, "fixture denial")
	}
	return "fixture-proof", "fixture-correlation", nil
}

// TestSecretDraftProtectedClientBindsExactRequestAndRejectsMissingAuthority exercises each registered draft method with success/proof/issuer outcomes, pins deterministic protobuf digest and outgoing context, and rejects unregistered RevealSecret before invoke. The final invoker is a local callback, not a broker or real secret store.
// TestSecretDraftProtectedClientBindsExactRequestAndRejectsMissingAuthority проверяет каждый зарегистрированный draft method при success, proof и issuer outcomes, закрепляет deterministic protobuf digest и outgoing context и отклоняет незарегистрированный RevealSecret до invoke. Итоговый invoker — локальный callback, а не broker и реальное secret store.
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
