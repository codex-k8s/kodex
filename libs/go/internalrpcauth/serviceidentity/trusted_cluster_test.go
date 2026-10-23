package serviceidentity

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestTrustedClusterRequiresExplicitConfigurationAndExactBinding verifies
// explicit profile selection, immutable exact binding and the inability of
// a trusted header to downgrade a protected mTLS server.
// RU: TestTrustedClusterRequiresExplicitConfigurationAndExactBinding
// проверяет явный выбор профиля, неизменность точного binding и невозможность
// понизить mTLS-сервер trusted-заголовком.
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

// TestTrustedClusterAdmissionProfileRequiresServerInterceptor verifies that
// metadata alone creates no admission and a denied method never reaches the
// unary handler.
// RU: TestTrustedClusterAdmissionProfileRequiresServerInterceptor проверяет,
// что одной metadata недостаточно для допуска, а запрещённый метод не
// достигает unary-обработчика.
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

// TestTrustedClusterRejectsAmbiguousAndLegacyMetadata covers absent, mixed,
// duplicated and legacy authority metadata on the trusted profile.
// RU: TestTrustedClusterRejectsAmbiguousAndLegacyMetadata покрывает
// отсутствующую, смешанную, повторную и устаревшую metadata полномочий.
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

// trustedStreamFixture supplies only an initial context to the gRPC stream
// interceptor so tests can inspect the admission wrapper independently.
// RU: trustedStreamFixture передаёт interceptor только исходный контекст,
// чтобы тест проверял обёртку допуска независимо от транспорта.
type trustedStreamFixture struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the unwrapped fixture context, proving admission is not
// written into caller-controlled metadata before server interception.
// RU: Context возвращает исходный контекст fixture и помогает доказать, что
// допуск не появляется из caller-controlled metadata до server interception.
func (stream trustedStreamFixture) Context() context.Context { return stream.ctx }

// TestTrustedClusterStreamAdmissionAndCancellation verifies exact stream
// binding, preservation of cancellation and no admission leakage into the
// caller's original context.
// RU: TestTrustedClusterStreamAdmissionAndCancellation проверяет точный
// binding потока, сохранение отмены и отсутствие допуска в исходном контексте.
func TestTrustedClusterStreamAdmissionAndCancellation(t *testing.T) {
	auth, err := NewTrustedCluster(TrustedClusterProfile, target, []Binding{binding()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller)))
	defer cancel()
	called := false
	err = auth.StreamServerInterceptor()(nil, trustedStreamFixture{ctx: ctx},
		&grpc.StreamServerInfo{FullMethod: method, IsClientStream: true},
		func(_ any, stream grpc.ServerStream) error {
			called = true
			admission, ok := FromContext(stream.Context())
			if !ok || admission.FullMethod != method || admission.Peer.SPIFFEID != caller ||
				admission.RPCProfile != TrustedClusterProfile || admission.Permission != binding().Permission {
				t.Fatal("stream admission is not bound to the registered method")
			}
			cancel()
			if stream.Context().Err() != context.Canceled {
				t.Fatal("stream lost transport cancellation")
			}
			return status.Error(codes.Canceled, "stream cancelled")
		})
	if !called || status.Code(err) != codes.Canceled {
		t.Fatal("stream handler result was not preserved")
	}
	if _, ok := FromContext(ctx); ok {
		t.Fatal("stream admission leaked into original context")
	}
}

// TestTrustedClusterStreamRejectsBeforeHandler ensures invalid profile,
// caller, method or mixed metadata is rejected before a stream handler runs.
// RU: TestTrustedClusterStreamRejectsBeforeHandler проверяет отказ при
// неверном профиле, caller, методе или смешанной metadata до запуска handler.
func TestTrustedClusterStreamRejectsBeforeHandler(t *testing.T) {
	auth, err := NewTrustedCluster(TrustedClusterProfile, target, []Binding{binding()})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method string
		pairs  []string
	}{
		{method, nil},
		{method, []string{ProfileMetadataKey, "service-v1", CallerMetadataKey, caller}},
		{method, []string{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, target}},
		{method + "Other", []string{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller}},
		{method, []string{ProfileMetadataKey, TrustedClusterProfile, CallerMetadataKey, caller, "x-kodex-authorization", "legacy"}},
	} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(test.pairs...))
		called := false
		err := auth.StreamServerInterceptor()(nil, trustedStreamFixture{ctx: ctx},
			&grpc.StreamServerInfo{FullMethod: test.method}, func(any, grpc.ServerStream) error {
				called = true
				return nil
			})
		if err == nil || called {
			t.Fatal("rejected stream reached handler")
		}
	}
}
