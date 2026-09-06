package browserstate

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const testID = "f480e57b-f1f3-4978-ab52-7f6eb0d97410"

type memoryStream struct {
	mu      sync.Mutex
	record  *jetstream.RawStreamMsg
	loseACK bool
	writes  int
}

func (s *memoryStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	return &jetstream.StreamInfo{Config: validConfig()}, nil
}

func (s *memoryStream) GetLastMsgForSubject(_ context.Context, subject string) (*jetstream.RawStreamMsg, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil {
		return nil, jetstream.ErrMsgNotFound
	}
	copy := *s.record
	return &copy, nil
}

func (s *memoryStream) PublishMsg(_ context.Context, message *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	expected, err := strconv.ParseUint(message.Header.Get(jetstream.ExpectedLastSubjSeqHeader), 10, 64)
	if err != nil || message.Header.Get(jetstream.ExpectedStreamHeader) != StreamName {
		return nil, errors.New("invalid CAS headers")
	}
	current := uint64(0)
	if s.record != nil {
		current = s.record.Sequence
	}
	if current != expected {
		return nil, errors.New("wrong last sequence")
	}
	s.record = &jetstream.RawStreamMsg{Subject: message.Subject, Sequence: current + 1, Data: message.Data}
	if s.loseACK {
		return nil, errors.New("response lost")
	}
	return &jetstream.PubAck{Stream: StreamName, Sequence: current + 1}, nil
}

func testStore(stream *memoryStream) *Store {
	return &Store{stream: stream, publisher: stream, replicas: 1, timeout: time.Second}
}

func TestLostACKReadbackDoesNotRepeatPublication(t *testing.T) {
	t.Parallel()
	stream := &memoryStream{loseACK: true}
	store := testStore(stream)
	record, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("unique-sealed-attempt"))
	if err != nil || record.Sequence != 1 || stream.writes != 1 {
		t.Fatal("lost ACK did not recover exact committed attempt")
	}
	if _, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("different-attempt")); !errors.Is(err, ErrConflict) {
		t.Fatal("different attempt was accepted as recovered commit")
	}
}

func TestConcurrentRotationHasSingleWinnerAndTerminalFencesOldVersion(t *testing.T) {
	t.Parallel()
	store := testStore(&memoryStream{})
	initial, err := store.CompareAndSwap(t.Context(), testID, 0, []byte("initial"))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, payload := range []string{"rotation-a", "rotation-b"} {
		go func() {
			_, err := store.CompareAndSwap(t.Context(), testID, initial.Sequence, []byte(payload))
			results <- err
		}()
	}
	success, conflict := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent CAS did not have one winner")
	}
	current, err := store.Read(t.Context(), testID)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := store.CompareAndSwap(t.Context(), testID, current.Sequence, []byte("terminal"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompareAndSwap(t.Context(), testID, current.Sequence, []byte("late-provider-response")); !errors.Is(err, ErrConflict) {
		t.Fatal("late response bypassed terminal fence")
	}
	got, err := store.Read(t.Context(), testID)
	if err != nil || got.Sequence != terminal.Sequence || string(got.Ciphertext) != "terminal" {
		t.Fatal("terminal state changed")
	}
}

func TestMissingStateCannotRecreateWithOldSequence(t *testing.T) {
	t.Parallel()
	store := testStore(&memoryStream{})
	if _, err := store.Read(t.Context(), testID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing state was not explicit")
	}
	if _, err := store.CompareAndSwap(t.Context(), testID, 23, []byte("old-family")); !errors.Is(err, ErrConflict) {
		t.Fatal("missing family was recreated")
	}
}

func validConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{SubjectFilter}, Storage: jetstream.FileStorage, Replicas: 1,
		Retention: jetstream.LimitsPolicy, Discard: jetstream.DiscardNew, MaxMsgsPerSubject: 1,
		MaxMsgSize: MaximumValueBytes, MaxMsgs: maximumRecords, MaxBytes: maximumBytes, MaxAge: Retention,
		Duplicates: 2 * time.Minute, DenyDelete: true, DenyPurge: true,
	}
}

func TestReadinessRejectsEvictionPrematureExpiryAndOverwriteDrift(t *testing.T) {
	t.Parallel()
	if !compatible(validConfig(), 1) {
		t.Fatal("canonical stream was rejected")
	}
	for name, mutate := range map[string]func(*jetstream.StreamConfig){
		"eviction":        func(c *jetstream.StreamConfig) { c.Discard = jetstream.DiscardOld },
		"short-retention": func(c *jetstream.StreamConfig) { c.MaxAge = 2 * time.Hour },
		"no-replacement":  func(c *jetstream.StreamConfig) { c.DiscardNewPerSubject = true },
		"caller-expiry":   func(c *jetstream.StreamConfig) { c.AllowMsgTTL = true },
		"delete":          func(c *jetstream.StreamConfig) { c.DenyDelete = false },
		"memory":          func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage },
	} {
		t.Run(name, func(t *testing.T) {
			config := validConfig()
			mutate(&config)
			if compatible(config, 1) {
				t.Fatal("unsafe stream drift accepted")
			}
		})
	}
}
