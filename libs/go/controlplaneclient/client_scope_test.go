package controlplaneclient

import (
	"context"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type captureResolver struct {
	request         *internalrpcauthorityv1.ResolveAuthorityProofRequest
	failures        []error
	calls           int
	idempotencyKeys []string
}

func (resolver *captureResolver) ResolveAuthorityProof(
	_ context.Context,
	request *internalrpcauthorityv1.ResolveAuthorityProofRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.ResolveAuthorityProofResponse, error) {
	resolver.request = request
	resolver.idempotencyKeys = append(resolver.idempotencyKeys, request.GetIdempotencyKey())
	call := resolver.calls
	resolver.calls++
	if call < len(resolver.failures) {
		return nil, resolver.failures[call]
	}
	return &internalrpcauthorityv1.ResolveAuthorityProofResponse{
		AuthorityProofCompactJws: "header.payload.signature",
		ProofRevision:            1,
		SignerGeneration:         1,
	}, nil
}

type staticProofFailure struct {
	err           error
	correlationID string
}

func (provider staticProofFailure) AuthorityProof(
	context.Context,
	string,
	string,
) (string, string, error) {
	return "", provider.correlationID, provider.err
}

func (*captureResolver) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse{Ready: true}, nil
}

func TestWithProjectReferenceAcceptsOnlyUUID(t *testing.T) {
	t.Parallel()

	if _, err := WithProjectReference(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("invalid project reference was accepted")
	}
	if _, err := WithProjectReference(nil, "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"); err == nil {
		t.Fatal("nil request context was accepted")
	}
	ctx, err := WithProjectReference(context.Background(), "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d")
	if err != nil {
		t.Fatalf("valid project reference was rejected: %v", err)
	}
	if value, _ := ctx.Value(projectReferenceContextKey{}).(string); value != "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d" {
		t.Fatalf("project reference was not bound: %q", value)
	}
}

func TestAuthorityProofSendsLocatorOnlyForProjectOperation(t *testing.T) {
	t.Parallel()

	const (
		projectID        = "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"
		projectOperation = "control.resource.list"
		projectMethod    = "/controlplane.v1.ControlPlaneService/ListResources"
		globalOperation  = "control.project.list"
		globalMethod     = "/controlplane.v1.ControlPlaneService/ListProjects"
	)
	resolver := &captureResolver{}
	client := &Client{
		resolver: resolver,
		proofOperations: operationSet{
			projectMethod: projectOperation,
			globalMethod:  globalOperation,
		},
		projectRequired: map[string]struct{}{projectOperation: {}},
	}
	ctx, err := WithApplicationGrant(context.Background(), "owner-bearer")
	if err != nil {
		t.Fatalf("bind grant: %v", err)
	}
	ctx, err = WithProjectReference(ctx, projectID)
	if err != nil {
		t.Fatalf("bind project: %v", err)
	}
	if _, _, err := client.AuthorityProof(ctx, projectOperation, projectMethod); err != nil {
		t.Fatalf("project proof: %v", err)
	}
	if resolver.request.GetProjectReference() != projectID {
		t.Fatalf("project locator was not sent: %q", resolver.request.GetProjectReference())
	}
	if _, _, err := client.AuthorityProof(ctx, globalOperation, globalMethod); err != nil {
		t.Fatalf("global proof: %v", err)
	}
	if resolver.request.GetProjectReference() != "" {
		t.Fatalf("project locator leaked into global proof: %q", resolver.request.GetProjectReference())
	}
}

func TestAuthorityProofRetriesTransientResolverFailure(t *testing.T) {
	t.Parallel()

	const (
		operation = "control.resource.list"
		method    = "/controlplane.v1.ControlPlaneService/ListResources"
	)
	resolver := &captureResolver{failures: []error{
		status.Error(codes.Unavailable, "private resolver failure"),
		status.Error(codes.DeadlineExceeded, "private resolver failure"),
	}}
	client := &Client{
		resolver: resolver,
		proofOperations: operationSet{
			method: operation,
		},
		projectRequired: map[string]struct{}{},
	}
	ctx, err := WithApplicationGrant(context.Background(), "owner-bearer")
	if err != nil {
		t.Fatalf("bind grant: %v", err)
	}
	if _, correlationID, err := client.AuthorityProof(ctx, operation, method); err != nil || correlationID == "" {
		t.Fatalf("proof retry failed: correlation=%q err=%v", correlationID, err)
	}
	if resolver.calls != proofResolutionAttempts {
		t.Fatalf("resolver calls = %d", resolver.calls)
	}
	for _, key := range resolver.idempotencyKeys[1:] {
		if key != resolver.idempotencyKeys[0] {
			t.Fatal("proof retry changed idempotency key")
		}
	}
}

func TestLocalAuthorityErrorInterceptorAddsTrustedDetail(t *testing.T) {
	t.Parallel()

	const (
		method        = "/controlplane.v1.ControlPlaneService/ListResources"
		operation     = "control.resource.list"
		correlationID = "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"
	)
	issuer := authorityclient.IssuerUnaryClientInterceptor(nil, operationSet{method: operation}, staticProofFailure{
		err: status.Error(codes.Unavailable, "private resolver failure"), correlationID: correlationID,
	})
	localErr := issuer(context.Background(), method, nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			t.Fatal("downstream RPC was called")
			return nil
		},
	)
	normalizedErr := localAuthorityErrorInterceptor()(context.Background(), method, nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return localErr
		},
	)
	current, ok := status.FromError(normalizedErr)
	if !ok || current.Code() != codes.Unavailable || len(current.Details()) != 1 {
		t.Fatalf("normalized status = %#v", current)
	}
	detail, ok := current.Details()[0].(*controlplanev1.ErrorDetail)
	if !ok || detail.GetReason() != controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE ||
		detail.GetCode() != "UNAVAILABLE" || detail.GetCorrelationId() != correlationID || !detail.GetRetryable() {
		t.Fatalf("normalized detail = %#v", current.Details()[0])
	}
}
