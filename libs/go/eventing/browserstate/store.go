// Package browserstate хранит непрозрачные зашифрованные состояния BFF с CAS.
// Содержимое и переходы принадлежат gateway; библиотека не толкует credentials.
package browserstate

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName        = "CONTROL_API_BROWSER_STATE"
	SubjectFilter     = "kodex.control_api.browser_state.*"
	subjectPrefix     = "kodex.control_api.browser_state."
	MaximumValueBytes = 64 << 10
	Retention         = 13 * time.Hour
	maximumRecords    = 100_000
	maximumBytes      = 512 << 20
)

var (
	ErrNotFound    = errors.New("browser state is absent")
	ErrConflict    = errors.New("browser state version conflict")
	ErrUnavailable = errors.New("browser state is unavailable")
)

type streamAccess interface {
	Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error)
	GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error)
}

type publisher interface {
	PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type Store struct {
	stream    streamAccess
	publisher publisher
	replicas  int
	timeout   time.Duration
}

type Record struct {
	Sequence   uint64
	Ciphertext []byte
}

// StreamConfig используется bootstrap и readiness без отдельных defaults.
func StreamConfig(connection natsjetstream.Config) natsjetstream.Config {
	connection.Stream = StreamName
	connection.Subjects = []string{SubjectFilter}
	connection.MaxMessageBytes = MaximumValueBytes
	connection.MaxMessages = maximumRecords
	connection.MaxBytes = maximumBytes
	connection.MaxPerSubject = 1
	connection.MaxAge = Retention
	connection.DuplicateWindow = 2 * time.Minute
	connection.Discard = jetstream.DiscardNew
	return connection
}

func New(ctx context.Context, connection *nats.Conn, replicas int, timeout time.Duration) (*Store, error) {
	if connection == nil || replicas < 1 || replicas > 5 || timeout < 100*time.Millisecond || timeout > 10*time.Second {
		return nil, errors.New("browser state configuration is invalid")
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stream, err := js.Stream(bounded, StreamName)
	if err != nil {
		return nil, ErrUnavailable
	}
	store := &Store{stream: stream, publisher: js, replicas: replicas, timeout: timeout}
	if err := store.Check(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *Store) Check(ctx context.Context) error {
	if store == nil || store.stream == nil || store.publisher == nil {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	info, err := store.stream.Info(bounded)
	if err != nil || info == nil || !compatible(info.Config, store.replicas) {
		return ErrUnavailable
	}
	return nil
}

func (store *Store) Read(ctx context.Context, id string) (Record, error) {
	if store == nil || store.stream == nil || uuid.Validate(id) != nil {
		return Record{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	message, err := store.stream.GetLastMsgForSubject(bounded, subjectPrefix+id)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return Record{}, ErrNotFound
	}
	if err != nil || message == nil || message.Subject != subjectPrefix+id || message.Sequence == 0 ||
		len(message.Data) == 0 || len(message.Data) > MaximumValueBytes {
		return Record{}, ErrUnavailable
	}
	return Record{Sequence: message.Sequence, Ciphertext: bytes.Clone(message.Data)}, nil
}

// CompareAndSwap не повторяет публикацию. Потерянный ACK восстанавливается
// только точным readback уникального ciphertext, созданного владельцем attempt.
// Отсутствие записи никогда не преобразуется в безусловную запись.
func (store *Store) CompareAndSwap(ctx context.Context, id string, expected uint64, ciphertext []byte) (Record, error) {
	if store == nil || store.publisher == nil || uuid.Validate(id) != nil || len(ciphertext) == 0 || len(ciphertext) > MaximumValueBytes {
		return Record{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	message := &nats.Msg{Subject: subjectPrefix + id, Data: bytes.Clone(ciphertext), Header: nats.Header{}}
	message.Header.Set(jetstream.ExpectedStreamHeader, StreamName)
	message.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(expected, 10))
	ack, err := store.publisher.PublishMsg(bounded, message, jetstream.WithRetryAttempts(0))
	if err == nil && ack != nil && ack.Stream == StreamName && ack.Sequence > expected && !ack.Duplicate {
		return Record{Sequence: ack.Sequence, Ciphertext: bytes.Clone(ciphertext)}, nil
	}
	current, readErr := store.Read(ctx, id)
	if readErr == nil && current.Sequence > expected && bytes.Equal(current.Ciphertext, ciphertext) {
		return current, nil
	}
	if readErr == nil && current.Sequence != expected {
		return Record{}, ErrConflict
	}
	if errors.Is(readErr, ErrNotFound) && expected != 0 {
		return Record{}, ErrConflict
	}
	return Record{}, ErrUnavailable
}

func compatible(actual jetstream.StreamConfig, replicas int) bool {
	return actual.Name == StreamName && slices.Equal(actual.Subjects, []string{SubjectFilter}) &&
		actual.Storage == jetstream.FileStorage && actual.Replicas == replicas &&
		actual.Retention == jetstream.LimitsPolicy && actual.Discard == jetstream.DiscardNew &&
		!actual.DiscardNewPerSubject && actual.MaxMsgsPerSubject == 1 &&
		actual.MaxMsgSize == MaximumValueBytes && actual.MaxMsgs == maximumRecords && actual.MaxBytes == maximumBytes &&
		actual.MaxAge == Retention && actual.Duplicates == 2*time.Minute &&
		actual.DenyDelete && actual.DenyPurge && !actual.Sealed && !actual.AllowRollup && !actual.AllowMsgTTL &&
		actual.Mirror == nil && len(actual.Sources) == 0 && actual.RePublish == nil && actual.SubjectTransform == nil
}
