package controlplaneclient

import (
	"context"
	"testing"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
)

type captureResolver struct {
	request *internalrpcauthorityv1.ResolveAuthorityProofRequest
}

func (resolver *captureResolver) ResolveAuthorityProof(
	_ context.Context,
	request *internalrpcauthorityv1.ResolveAuthorityProofRequest,
	_ ...grpc.CallOption,
) (*internalrpcauthorityv1.ResolveAuthorityProofResponse, error) {
	resolver.request = request
	return &internalrpcauthorityv1.ResolveAuthorityProofResponse{
		AuthorityProofCompactJws: "header.payload.signature",
		ProofRevision:            1,
		SignerGeneration:         1,
	}, nil
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
