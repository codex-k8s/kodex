package serviceidentity

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnaryAdmissionDoesNotCallHandlerOnDeniedPeer(t *testing.T) {
	check := &revocationCheck{}
	auth, err := New(target, []Binding{binding()}, check)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := syntheticPeer(t)
	calls := 0
	handler := func(ctx context.Context, _ any) (any, error) {
		calls++
		admission, ok := FromContext(ctx)
		if !ok || admission.FullMethod != method || admission.Peer.SPIFFEID != caller {
			t.Fatal("verified admission missing")
		}
		return nil, nil
	}
	if _, ok := FromContext(ctx); ok {
		t.Fatal("admission existed before interceptor")
	}
	intercept := auth.UnaryServerInterceptor()
	if _, err = intercept(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler); err != nil {
		t.Fatal(err)
	}
	check.err = errors.New("revoked")
	if _, err = intercept(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler); status.Code(err) != codes.PermissionDenied {
		t.Fatal("revoked peer accepted")
	}
	if calls != 1 {
		t.Fatal("denied call reached handler")
	}
}

type testStream struct {
	grpc.ServerStream
	ctx       context.Context
	onReceive func()
	sent      int
}

func (stream *testStream) Context() context.Context { return stream.ctx }
func (stream *testStream) RecvMsg(any) error        { stream.onReceive(); return nil }
func (stream *testStream) SendMsg(any) error        { stream.sent++; return nil }

func TestStreamRevocationDuringReceivePreventsDeliveryAndSend(t *testing.T) {
	check := &revocationCheck{}
	auth, err := New(target, []Binding{binding()}, check)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := syntheticPeer(t)
	stream := &testStream{ctx: ctx, onReceive: func() { check.err = errors.New("revoked during receive") }}
	err = auth.StreamServerInterceptor()(nil, stream, &grpc.StreamServerInfo{FullMethod: method}, func(_ any, wrapped grpc.ServerStream) error {
		if _, ok := FromContext(wrapped.Context()); !ok {
			t.Fatal("stream admission missing")
		}
		if e := wrapped.RecvMsg(nil); status.Code(e) != codes.PermissionDenied {
			t.Fatal("revoked message delivered")
		}
		if e := wrapped.SendMsg(nil); status.Code(e) != codes.PermissionDenied {
			t.Fatal("revoked stream sent data")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stream.sent != 0 {
		t.Fatal("denied send reached transport")
	}
}
