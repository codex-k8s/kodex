package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryCallerAuthInterceptorStoresVerifiedCaller(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		metadataAuthorization, "Bearer shared-token",
		metadataCallerType, "service",
		metadataCallerID, "staff-gateway",
	))
	_, err := UnaryCallerAuthInterceptor(true, "shared-token")(ctx, nil, unaryInfo(), func(ctx context.Context, _ any) (any, error) {
		identity, ok := CallerIdentityFromContext(ctx)
		if !ok {
			t.Fatal("caller identity is missing")
		}
		if identity.Type != "service" || identity.ID != "staff-gateway" {
			t.Fatalf("caller identity = %+v, want service/staff-gateway", identity)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("UnaryCallerAuthInterceptor(): %v", err)
	}
}

func TestUnaryCallerAuthInterceptorRejectsMissingToken(t *testing.T) {
	t.Parallel()

	_, err := UnaryCallerAuthInterceptor(true, "shared-token")(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %s, want unauthenticated", status.Code(err))
	}
}
