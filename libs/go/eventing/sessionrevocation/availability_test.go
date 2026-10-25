package sessionrevocation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/availability"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestRevocationReadRetainsNativeClass separates absent revocation from broker failure without changing the authoritative false/error boundary.
// TestRevocationReadRetainsNativeClass отделяет отсутствие отзыва от отказа брокера без изменения авторитетной границы false/error.
func TestRevocationReadRetainsNativeClass(t *testing.T) {
	for _, item := range []struct {
		err   error
		class string
	}{{context.Canceled, "canceled"}, {nats.ErrDisconnected, "disconnected"}, {nats.ErrNoResponders, "no_responders"}, {errors.New("private-credential"), "unknown"}} {
		store := &Store{stream: &fakeStream{err: item.err}, timeout: time.Second}
		found, err := store.Revoked(t.Context(), "f480e57b-f1f3-4978-ab52-7f6eb0d97410")
		if found || err == nil || availability.Class(err) != item.class {
			t.Fatal("revocation read lost failure boundary")
		}
	}
	store := &Store{stream: &fakeStream{err: jetstream.ErrMsgNotFound}, timeout: time.Second}
	if found, err := store.Revoked(t.Context(), "f480e57b-f1f3-4978-ab52-7f6eb0d97410"); found || err != nil {
		t.Fatal("absence became unavailable")
	}
}
