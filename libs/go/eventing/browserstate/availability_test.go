package browserstate

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/eventing/availability"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// failedReadStream injects one native read failure while retaining the existing local publication recorder.
// failedReadStream вводит один исходный отказ чтения, сохраняя прежний локальный счётчик публикаций.
type failedReadStream struct {
	*memoryStream
	err error
}

// GetLastMsgForSubject returns only the injected failure and never simulates a successful broker receipt.
// GetLastMsgForSubject возвращает только заданный отказ, не имитируя успешную квитанцию брокера.
func (stream *failedReadStream) GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error) {
	return nil, stream.err
}

// TestReadAndUncertainWriteKeepNativeClass checks the real store paths preserve diagnostics without changing absent records, lost ACK recovery or publication count.
// TestReadAndUncertainWriteKeepNativeClass проверяет настоящие пути хранилища без изменения отсутствующих записей, восстановления потерянного ACK и числа публикаций.
func TestReadAndUncertainWriteKeepNativeClass(t *testing.T) {
	for _, item := range []struct {
		err   error
		class string
	}{{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline"}, {nats.ErrConnectionClosed, "connection_closed"}, {nats.ErrNoResponders, "no_responders"}, {errors.New("private-credential"), "unknown"}} {
		stream := &failedReadStream{memoryStream: &memoryStream{loseACK: true}, err: item.err}
		store := testStore(stream.memoryStream)
		store.stream = stream
		if _, err := store.Read(t.Context(), testID); !errors.Is(err, ErrUnavailable) || availability.Class(err) != item.class {
			t.Fatal("read lost native cause")
		}
		if _, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("sealed-fixture")); !errors.Is(err, ErrUnavailable) || availability.Class(err) != item.class || stream.writes != 1 {
			t.Fatal("uncertain write changed effect count or cause")
		}
	}
	store := testStore(&memoryStream{})
	if _, err := store.Read(t.Context(), testID); !errors.Is(err, ErrNotFound) {
		t.Fatal("absence became unavailable")
	}
	store.publisher = store.stream.(*memoryStream)
	store.stream.(*memoryStream).loseACK = true
	if _, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("sealed-fixture")); err != nil || store.stream.(*memoryStream).writes != 1 {
		t.Fatal("exact ACK recovery changed")
	}
}
