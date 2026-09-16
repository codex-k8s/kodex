package serviceidentity

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTrustedClusterRequiresExplicitConfigurationAndExactBinding(t *testing.T) {
	for _, profile := range []string{"", "service-v1", "insecure", "production"} {
		if _, err := NewTrustedCluster(profile, target, []Binding{binding()}); err == nil {
			t.Fatal("implicit trusted profile accepted")
		}
	}
	bindings := []Binding{binding()}
	auth, err := NewTrustedCluster(TrustedClusterProfile, target, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].Permission = "changed.permission"
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller,
		"x-kodex-permission", "everything", "x-kodex-actor", "owner"))
	admission, err := auth.Admit(ctx, method)
	if err != nil || admission.RPCProfile != TrustedClusterProfile || admission.Permission != binding().Permission || admission.ActorMode != ServiceActor || admission.Peer.SPIFFEID != caller || admission.Peer.CertificateSHA256 != [32]byte{} || !admission.Peer.NotAfter.IsZero() {
		t.Fatal("trusted admission changed authority or claimed certificate verification")
	}
	if _, err := auth.Admit(ctx, method+"Other"); status.Code(err) != codes.PermissionDenied {
		t.Fatal("unknown method accepted")
	}
	protected, err := New(target, []Binding{binding()}, CertificateLifetimeBoundary{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protected.Admit(ctx, method); status.Code(err) != codes.Unauthenticated {
		t.Fatal("trusted header bypassed protected transport")
	}
}

func TestTrustedClusterAdmissionProfileRequiresServerInterceptor(t *testing.T) {
	auth, err := NewTrustedCluster(TrustedClusterProfile, target, []Binding{binding()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller))
	if _, ok := FromContext(ctx); ok {
		t.Fatal("request metadata created server admission")
	}
	called := false
	_, err = auth.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		called = true
		admission, ok := FromContext(ctx)
		if !ok || admission.RPCProfile != TrustedClusterProfile || admission.FullMethod != method || admission.Peer.SPIFFEID != caller {
			t.Fatal("server admission profile or binding missing")
		}
		return nil, nil
	})
	if err != nil || !called {
		t.Fatal("permitted handler was not called")
	}
	called = false
	_, err = auth.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method + "Other"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatal("rejected binding reached handler")
	}
}

func TestTrustedClusterRejectsAmbiguousAndLegacyMetadata(t *testing.T) {
	auth, err := NewTrustedCluster(TrustedClusterProfile, target, []Binding{binding()})
	if err != nil {
		t.Fatal(err)
	}
	for _, pairs := range [][]string{
		{},
		{ProfileMetadataKey, "service-v1", CallerMetadataKey, caller},
		{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller, CallerMetadataKey, caller},
		{ProfileMetadataKey, TrustedClusterProfile, ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller},
		{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller, "x-kodex-authorization", "legacy"},
		{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, target},
	} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(pairs...))
		if _, err := auth.Admit(ctx, method); err == nil {
			t.Fatal("invalid trusted metadata accepted")
		}
	}
}
