package browserstate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Тест запускается поддерживаемым script entrypoint на disposable loopback NATS.
// В обычном unit suite внешняя среда не предполагается.
func TestBrowserStateComponent(t *testing.T) {
	endpoint := os.Getenv("KODEX_BROWSER_STATE_TEST_URL")
	if endpoint == "" {
		t.Skip("run make test-browser-state-component")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "nats" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" {
		t.Fatal("component endpoint must be disposable loopback")
	}
	var connection *nats.Conn
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, err = nats.Connect(endpoint, nats.NoReconnect(), nats.Timeout(200*time.Millisecond))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("disposable NATS did not start")
	}
	defer connection.Close()
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("KODEX_BROWSER_STATE_TEST_PHASE") == "write" {
		if _, err := js.CreateStream(t.Context(), validConfig()); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(t.Context(), connection, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("KODEX_BROWSER_STATE_TEST_PHASE") == "read" {
		restored, err := store.Read(t.Context(), testID)
		if err != nil || restored.Sequence < 4 || string(restored.Ciphertext) != "sealed-terminal" {
			t.Fatal("restart lost authoritative terminal record")
		}
		if _, err := store.CompareAndSwap(t.Context(), testID, restored.Sequence-1, []byte("late-refresh")); !errors.Is(err, ErrConflict) {
			t.Fatal("restart reopened stale CAS")
		}
		return
	}
	if os.Getenv("KODEX_BROWSER_STATE_TEST_PHASE") != "write" {
		t.Fatal("component phase is invalid")
	}
	initial, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("sealed-initial"))
	if err != nil {
		t.Fatal(err)
	}
	// Сервер действительно фиксирует запись; fixture теряет только ACK до
	// возврата в caller. Adapter обязан прочитать exact durable результат.
	store.publisher = lostComponentACK{next: js}
	rotating, err := store.CompareAndSwap(t.Context(), testID, initial.Sequence, []byte("sealed-refreshing"))
	if err != nil || rotating.Sequence <= initial.Sequence {
		t.Fatal("lost ACK was not recovered from real server")
	}
	store.publisher = js
	results := make(chan error, 8)
	for i := range 8 {
		go func() {
			_, err := store.CompareAndSwap(t.Context(), testID, rotating.Sequence, []byte(fmt.Sprintf("sealed-winner-%d", i)))
			results <- err
		}()
	}
	winners := 0
	for range 8 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("real server CAS admitted multiple winners")
	}
	current, err := store.Read(t.Context(), testID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompareAndSwap(t.Context(), testID, current.Sequence, []byte("sealed-terminal")); err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatal("per-subject replacement did not preserve exactly one record")
	}
	drift := validConfig()
	drift.MaxAge = 2 * time.Hour
	if _, err := js.UpdateStream(t.Context(), drift); err != nil {
		t.Fatal(err)
	}
	if err := store.Check(t.Context()); err == nil {
		t.Fatal("readiness accepted insufficient retention")
	}
	if _, err := js.UpdateStream(t.Context(), validConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type lostComponentACK struct{ next publisher }

func (p lostComponentACK) PublishMsg(ctx context.Context, message *nats.Msg, options ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if _, err := p.next.PublishMsg(ctx, message, options...); err != nil {
		return nil, err
	}
	return nil, errors.New("fixture lost committed ACK")
}
