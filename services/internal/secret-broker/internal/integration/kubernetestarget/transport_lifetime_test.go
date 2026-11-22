package kubernetestarget

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// lifetimeReadFixture models a peer returning bytes and clean EOF after its request was cancelled.
// lifetimeReadFixture моделирует peer, вернувший bytes и clean EOF после отмены request.
type lifetimeReadFixture struct{ read func([]byte) (int, error) }

// Read executes one controlled response-body read without a replacement network transport.
// Read выполняет controlled response-body read без подмены network transport.
func (fixture lifetimeReadFixture) Read(buffer []byte) (int, error) { return fixture.read(buffer) }

// Close has no resources because the fixture owns only a synchronous body callback.
// Close не освобождает resources: fixture владеет только synchronous body callback.
func (lifetimeReadFixture) Close() error { return nil }

// TestTransportLifetimeDoesNotPromoteCancelledEOF deterministically reproduces the native TLS FIN race.
// TestTransportLifetimeDoesNotPromoteCancelledEOF детерминированно воспроизводит native TLS FIN race.
func TestTransportLifetimeDoesNotPromoteCancelledEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &transportLifetimeBody{ctx: ctx, cancel: cancel, ReadCloser: lifetimeReadFixture{read: func(buffer []byte) (int, error) { cancel(); return copy(buffer, "partial"), io.EOF }}}
	raw, err := io.ReadAll(body)
	if string(raw) != "partial" || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled clean EOF concealed lifetime failure or lost partial bytes")
	}
}

// TestTransportLifetimeRejectsExpiredReadBeforeBody prevents consuming another byte after pin expiry.
// TestTransportLifetimeRejectsExpiredReadBeforeBody запрещает чтение нового byte после pin expiry.
func TestTransportLifetimeRejectsExpiredReadBeforeBody(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	called := false
	body := &transportLifetimeBody{ctx: ctx, cancel: cancel, ReadCloser: lifetimeReadFixture{read: func([]byte) (int, error) { called = true; return 0, io.EOF }}}
	if raw, err := io.ReadAll(body); len(raw) != 0 || !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatal("expired pin reached underlying body")
	}
}

// TestTransportLifetimePreservesTimelyEOF keeps normal completion distinct from its own cleanup cancellation.
// TestTransportLifetimePreservesTimelyEOF отличает normal completion от собственной cleanup cancellation.
func TestTransportLifetimePreservesTimelyEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	body := &transportLifetimeBody{ctx: ctx, cancel: cancel, ReadCloser: io.NopCloser(strings.NewReader("complete"))}
	if raw, err := io.ReadAll(body); err != nil || string(raw) != "complete" {
		t.Fatal("timely EOF was confused with cleanup cancellation")
	}
}
