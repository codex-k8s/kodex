package serviceidentity

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestUnaryAdmissionDoesNotCallHandlerOnDeniedPeer verifies that server
// context admission is created only by the interceptor and a revoked peer
// cannot reach the unary handler.
// RU: TestUnaryAdmissionDoesNotCallHandlerOnDeniedPeer проверяет, что допуск
// в контексте создаёт только interceptor, а отозванный peer не достигает
// unary-обработчика.
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

// testStream simulates transport receive/send calls while allowing a test to
// revoke the peer during a blocking receive.
// RU: testStream имитирует вызовы транспорта receive/send и позволяет тесту
// отозвать peer во время блокирующего чтения.
type testStream struct {
	grpc.ServerStream
	ctx       context.Context
	onReceive func()
	sent      int
}

// Context returns the original fixture context before the interceptor wraps it.
// RU: Context возвращает исходный контекст тестового потока до его обёртки.
func (stream *testStream) Context() context.Context { return stream.ctx }

// RecvMsg invokes the configured receive hook to simulate revocation between
// the interceptor's pre-read and post-read admission checks.
// RU: RecvMsg вызывает тестовый hook чтения, имитируя отзыв между проверками
// допуска до и после чтения.
func (stream *testStream) RecvMsg(any) error { stream.onReceive(); return nil }

// SendMsg records an actual transport send so the test can detect any send
// that bypasses the wrapper's revocation check.
// RU: SendMsg учитывает реальную отправку через транспорт, чтобы тест выявил
// обход проверки отзыва в обёртке.
func (stream *testStream) SendMsg(any) error { stream.sent++; return nil }

// TestStreamRevocationDuringReceivePreventsDeliveryAndSend verifies the
// post-receive check blocks delivery and the send check blocks output after
// peer revocation on a previously admitted stream.
// RU: TestStreamRevocationDuringReceivePreventsDeliveryAndSend проверяет,
// что после отзыва peer уже допущенный поток не передаёт прочитанное сообщение
// обработчику и не отправляет ответ.
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
