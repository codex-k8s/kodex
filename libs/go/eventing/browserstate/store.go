// Package browserstate stores opaque encrypted BFF state using subject-level CAS.
// Package browserstate хранит непрозрачное зашифрованное состояние BFF через CAS по subject.
// The gateway owns its contents and transitions; this package does not interpret credentials.
// Gateway владеет содержимым и переходами; пакет не толкует credentials.
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
	// StreamName pins the dedicated JetStream stream for encrypted browser state.
	// StreamName закрепляет отдельный поток JetStream для зашифрованного состояния браузера.
	StreamName = "CONTROL_API_BROWSER_STATE"
	// SubjectFilter bounds the stream to browser-state subjects.
	// SubjectFilter ограничивает поток subject-ами состояния браузера.
	SubjectFilter = "kodex.control_api.browser_state.*"
	// subjectPrefix derives one subject from a validated browser-state identifier.
	// subjectPrefix формирует один subject из проверенного идентификатора состояния браузера.
	subjectPrefix = "kodex.control_api.browser_state."
	// MaximumValueBytes bounds each opaque ciphertext before publication and readback.
	// MaximumValueBytes ограничивает каждый шифротекст до публикации и при чтении.
	MaximumValueBytes = 64 << 10
	// Retention keeps browser state beyond the session lifetime without indefinite storage.
	// Retention хранит состояние дольше срока сеанса, но не бессрочно.
	Retention = 13 * time.Hour
	// maximumRecords bounds the stream's aggregate message count.
	// maximumRecords ограничивает общее число сообщений потока.
	maximumRecords = 100_000
	// maximumBytes bounds aggregate ciphertext storage in the stream.
	// maximumBytes ограничивает общий объём шифротекста в потоке.
	maximumBytes = 512 << 20
)

var (
	// ErrNotFound distinguishes an absent state from storage unavailability.
	// ErrNotFound отличает отсутствие состояния от недоступности хранилища.
	ErrNotFound = errors.New("browser state is absent")
	// ErrConflict denotes a stale subject sequence or a competing writer.
	// ErrConflict обозначает устаревшую последовательность subject или конкурирующую запись.
	ErrConflict = errors.New("browser state version conflict")
	// ErrUnavailable closes reads and writes when the stream outcome cannot be trusted.
	// ErrUnavailable закрыто отклоняет чтение и запись при недостоверном исходе потока.
	ErrUnavailable = errors.New("browser state is unavailable")
)

// streamAccess is the minimal readback surface needed to validate and read this stream.
// streamAccess — минимальный интерфейс для проверки и чтения этого потока.
type streamAccess interface {
	// Info returns the served stream contract for exact readiness checks.
	// Info возвращает действующий контракт потока для точной проверки готовности.
	Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error)
	// GetLastMsgForSubject reads only the latest state for one exact subject.
	// GetLastMsgForSubject читает только последнее состояние одного точного subject.
	GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error)
}

// publisher is the narrow publication surface used for CAS writes.
// publisher — узкий интерфейс публикации для записей CAS.
type publisher interface {
	// PublishMsg returns the broker acknowledgement for one attempted write.
	// PublishMsg возвращает подтверждение брокера для одной попытки записи.
	PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Store owns bounded access to the exact browser-state stream and publisher.
// Store владеет ограниченным доступом к точному потоку состояния браузера и издателю.
type Store struct {
	stream    streamAccess
	publisher publisher
	replicas  int
	timeout   time.Duration
}

// Record pairs the broker sequence with an isolated copy of opaque ciphertext.
// Record связывает последовательность брокера с отдельной копией непрозрачного шифротекста.
type Record struct {
	Sequence   uint64
	Ciphertext []byte
}

// StreamConfig supplies the same exact stream limits to bootstrap and readiness.
// StreamConfig передаёт одинаковые точные лимиты потока в bootstrap и проверку готовности.
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

// New opens the existing stream and rejects incompatible served configuration before use.
// New открывает существующий поток и отклоняет несовместимую действующую конфигурацию до работы.
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

// Check verifies the live stream against the pinned retention and CAS contract.
// Check сверяет действующий поток с закреплённым контрактом хранения и CAS.
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

// Read returns a validated copy of the latest record for one UUID-shaped identifier.
// Read возвращает проверенную копию последней записи для одного идентификатора в форме UUID.
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

// CompareAndSwap attempts one version-fenced write and never retries publication.
// CompareAndSwap выполняет одну запись с проверкой версии и не повторяет публикацию.
// A lost ACK is recovered only by exact readback of this attempt's ciphertext; absence never becomes an unconditional write.
// Потерянный ACK восстанавливается только точным чтением шифротекста этой попытки; отсутствие записи не превращается в безусловную запись.
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

// compatible rejects stream drift that could evict, overwrite, or expose browser state.
// compatible отклоняет изменение потока, способное вытеснить, перезаписать или раскрыть состояние браузера.
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
