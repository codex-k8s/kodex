package controlplaneclient

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestServiceIdentityForwardsExplicitContextOnceWithoutLegacy(t *testing.T) {
	const method = "/example.v1.Service/Read"
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "stale", "x-kodex-authorization", "stale", "x-kodex-project-ref", "foreign"))
	ctx, err := WithApplicationGrant(ctx, "synthetic-user-credential")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithProjectReference(ctx, "prj_abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = serviceIdentityUnary(operationSet{method: "project.read"}, map[string]struct{}{"project.read": {}})(ctx, method, nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		calls++
		md, _ := metadata.FromOutgoingContext(ctx)
		if len(md.Get("x-kodex-authorization")) != 0 || len(md.Get("authorization")) != 1 || md.Get("authorization")[0] != "Bearer synthetic-user-credential" || md.Get("x-kodex-project-ref")[0] != "prj_abcdefghijk" || md.Get("x-kodex-rpc-profile")[0] != "service-v1" {
			t.Fatal("incorrect service metadata")
		}
		return status.Error(codes.Unavailable, "synthetic unavailable")
	})
	if status.Code(err) != codes.Unavailable || calls != 1 {
		t.Fatal("unexpected fallback or retry")
	}
}

func TestServiceIdentityRejectsMissingProjectBeforeInvoke(t *testing.T) {
	called := false
	err := serviceIdentityUnary(operationSet{"/example.v1.Service/Read": "project.read"}, map[string]struct{}{"project.read": {}})(t.Context(), "/example.v1.Service/Read", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		called = true
		return nil
	})
	if status.Code(err) != codes.InvalidArgument || called {
		t.Fatal("missing project reached transport")
	}
}

func TestServiceIdentityOrdinaryReadinessDoesNotRequireLegacyIssuer(t *testing.T) {
	client := &Client{serviceIdentity: true}
	if err := client.CheckLocalAuthority(t.Context()); err != nil {
		t.Fatalf("ordinary service readiness depends on legacy issuer: %v", err)
	}
}

func TestServiceProjectOperationsAcceptsRegisteredProofOperation(t *testing.T) {
	projects, err := serviceProjectOperations(
		operationSet{"/controlplane.v1.Runtime/Claim": "runtime.claim"},
		map[string]string{"runtime.credentials.materialize": "/secretbroker.v1.Runtime/Materialize"},
		map[string]struct{}{"runtime.credentials.materialize": {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := projects["runtime.credentials.materialize"]; !ok {
		t.Fatal("registered proof operation is missing")
	}
}

func TestServiceProjectOperationsRejectsUnknownOperation(t *testing.T) {
	_, err := serviceProjectOperations(
		operationSet{"/controlplane.v1.Runtime/Claim": "runtime.claim"},
		map[string]string{"runtime.credentials.materialize": "/secretbroker.v1.Runtime/Materialize"},
		map[string]struct{}{"runtime.credentials.unknown": {}},
	)
	if err == nil || err.Error() != "service project operation is not registered" {
		t.Fatalf("unknown operation must be rejected: %v", err)
	}
}
