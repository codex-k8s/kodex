// Package sessionrevocation stores authoritative browser-session revocation in NATS JetStream.
// Package sessionrevocation хранит авторитетный отзыв браузерных сеансов в NATS JetStream.
package sessionrevocation

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/availability"
	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// StreamName pins the separate stream that records session revocations.
	// StreamName закрепляет отдельный поток записей об отзыве сеансов.
	StreamName = "CONTROL_API_SESSION_REVOCATIONS"
	// SubjectFilter restricts the stream to exact revocation subjects.
	// SubjectFilter ограничивает поток точными subject-ами отзыва.
	SubjectFilter = "kodex.control_api.session_revocation.*"
	// subjectPrefix derives one record subject from a validated session identifier.
	// subjectPrefix формирует subject записи из проверенного идентификатора сеанса.
	subjectPrefix = "kodex.control_api.session_revocation."
	// revokedValue is the only accepted durable payload for a revoked session.
	// revokedValue — единственное допускаемое постоянное содержимое записи об отзыве.
	revokedValue = "revoked-v1"
)

const (
	// maximumRevocations bounds the number of retained revocation records.
	// maximumRevocations ограничивает число сохраняемых записей об отзыве.
	maximumRevocations = 100_000
	// maximumBytes bounds aggregate storage for revocation records.
	// maximumBytes ограничивает общий объём записей об отзыве.
	maximumBytes = 16 << 20
	// maximumValueBytes bounds the size of an individual revocation payload.
	// maximumValueBytes ограничивает размер отдельного сообщения об отзыве.
	maximumValueBytes = 1024
	// revocationMaxAge determines how long revocation proof remains available.
	// revocationMaxAge определяет срок доступности доказательства отзыва.
	revocationMaxAge = 2 * time.Hour
)

// streamAccess is the minimum interface for exact stream readback.
// streamAccess — минимальный интерфейс точного чтения потока.
type streamAccess interface {
	// Info returns the served stream configuration for readiness validation.
	// Info возвращает действующую конфигурацию потока для проверки готовности.
	Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error)
	// GetLastMsgForSubject reads the latest revocation value of one session.
	// GetLastMsgForSubject читает последнее значение отзыва одного сеанса.
	GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error)
}

// jetStreamAccess limits the store to opening its stream and publishing records.
// jetStreamAccess ограничивает хранилище открытием потока и публикацией записей.
type jetStreamAccess interface {
	// Stream opens the named existing stream without creating it.
	// Stream открывает названный существующий поток без его создания.
	Stream(context.Context, string) (jetstream.Stream, error)
	// PublishMsg returns one broker acknowledgement for a revocation write.
	// PublishMsg возвращает одно подтверждение брокера для записи отзыва.
	PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Store owns the pinned revocation stream and bounded broker operations.
// Store владеет закреплённым потоком отзывов и ограниченными операциями брокера.
type Store struct {
	jetstream jetStreamAccess
	stream    streamAccess
	replicas  int
	timeout   time.Duration
}

// StreamConfig provides one canonical stream contract for bootstrap and readback.
// StreamConfig возвращает один канонический контракт потока для bootstrap и чтения.
func StreamConfig(connection natsjetstream.Config) natsjetstream.Config {
	connection.Stream = StreamName
	connection.Subjects = []string{SubjectFilter}
	connection.MaxMessageBytes = maximumValueBytes
	connection.MaxMessages = maximumRevocations
	connection.MaxBytes = maximumBytes
	connection.MaxPerSubject = 1
	connection.MaxAge = revocationMaxAge
	connection.DuplicateWindow = 2 * time.Minute
	connection.Discard = jetstream.DiscardNew
	return connection
}

// New opens the preexisting stream and verifies its served retention contract.
// New открывает существующий поток и проверяет его действующий контракт хранения.
func New(ctx context.Context, connection *nats.Conn, replicas int, timeout time.Duration) (*Store, error) {
	if connection == nil || replicas < 1 || replicas > 5 || timeout < 100*time.Millisecond || timeout > 10*time.Second {
		return nil, errors.New("session revocation store configuration is invalid")
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, errors.New("construct session revocation JetStream client")
	}
	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		return nil, errors.New("open session revocation stream")
	}
	store := &Store{jetstream: js, stream: stream, replicas: replicas, timeout: timeout}
	if err := store.Check(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// Check rejects changes to the served stream that weaken revocation durability.
// Check отклоняет изменения действующего потока, ослабляющие сохранность отзывов.
func (store *Store) Check(ctx context.Context) error {
	if store == nil || store.jetstream == nil || store.stream == nil {
		return errors.New("session revocation store is unavailable")
	}
	check, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	info, err := store.stream.Info(check)
	if err != nil {
		return errors.New("read session revocation stream")
	}
	if !compatible(info.Config, store.replicas) {
		return errors.New("session revocation stream contract mismatch")
	}
	return nil
}

// Revoke publishes one idempotent revocation and recovers uncertain ACK by readback.
// Revoke публикует один идемпотентный отзыв и проверяет чтением неопределённый ACK.
func (store *Store) Revoke(ctx context.Context, sessionID string) error {
	if store == nil || store.jetstream == nil || uuid.Validate(sessionID) != nil {
		return errors.New("session revocation input is invalid")
	}
	write, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	ack, err := store.jetstream.PublishMsg(write, &nats.Msg{Subject: subject(sessionID), Data: []byte(revokedValue)},
		jetstream.WithMsgID("session-revocation:"+sessionID), jetstream.WithExpectStream(StreamName), jetstream.WithRetryAttempts(0))
	if err != nil || ack == nil || ack.Stream != StreamName || ack.Sequence == 0 {
		revoked, readErr := store.Revoked(ctx, sessionID)
		if readErr == nil && revoked {
			return nil
		}
		return errors.New("persist session revocation")
	}
	return nil
}

// ConsumeOnce atomically closes a browser session using subject-level CAS.
// ConsumeOnce атомарно закрывает браузерный сеанс через CAS по subject.
// Only the first caller to persist a revocation for an empty subject wins.
// Побеждает только первый caller, записавший отзыв в пустой subject.
func (store *Store) ConsumeOnce(ctx context.Context, sessionID string) (bool, error) {
	if store == nil || store.jetstream == nil || uuid.Validate(sessionID) != nil {
		return false, errors.New("session consumption input is invalid")
	}
	write, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	message := &nats.Msg{Subject: subject(sessionID), Data: []byte(revokedValue), Header: nats.Header{}}
	message.Header.Set(jetstream.MsgIDHeader, "session-consumption:"+uuid.NewString())
	message.Header.Set(jetstream.ExpectedStreamHeader, StreamName)
	message.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(0, 10))
	ack, err := store.jetstream.PublishMsg(write, message, jetstream.WithRetryAttempts(0))
	if err == nil && ack != nil && ack.Stream == StreamName && ack.Sequence > 0 {
		return true, nil
	}
	revoked, readErr := store.Revoked(ctx, sessionID)
	if readErr != nil {
		return false, errors.New("read session consumption result")
	}
	if revoked {
		return false, nil
	}
	return false, errors.New("persist session consumption")
}

// Revoked reads and validates the exact durable marker for one session.
// Revoked читает и проверяет точный постоянный маркер одного сеанса.
func (store *Store) Revoked(ctx context.Context, sessionID string) (bool, error) {
	if store == nil || store.stream == nil || uuid.Validate(sessionID) != nil {
		return false, errors.New("session revocation input is invalid")
	}
	read, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	message, err := store.stream.GetLastMsgForSubject(read, subject(sessionID))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return false, nil
	}
	if err != nil {
		return false, availability.Wrap(errors.New("read session revocation"), err)
	}
	if message == nil || message.Subject != subject(sessionID) || message.Sequence == 0 || string(message.Data) != revokedValue {
		return false, availability.Invalid(errors.New("session revocation record is invalid"))
	}
	return true, nil
}

// subject derives the revocation subject from an already validated session identifier.
// subject формирует subject отзыва из уже проверенного идентификатора сеанса.
func subject(sessionID string) string { return subjectPrefix + sessionID }

// compatible rejects stream drift that could evict or forge revocation state.
// compatible отклоняет изменение потока, способное вытеснить или подменить состояние отзыва.
func compatible(actual jetstream.StreamConfig, replicas int) bool {
	expectedSubjects := []string{SubjectFilter}
	actualSubjects := slices.Clone(actual.Subjects)
	slices.Sort(actualSubjects)
	return actual.Name == StreamName && slices.Equal(actualSubjects, expectedSubjects) &&
		actual.Storage == jetstream.FileStorage && actual.Replicas == replicas &&
		actual.Retention == jetstream.LimitsPolicy && actual.Discard == jetstream.DiscardNew &&
		actual.MaxMsgSize == maximumValueBytes && actual.MaxMsgs == maximumRevocations &&
		actual.MaxBytes == maximumBytes && actual.MaxMsgsPerSubject == 1 &&
		actual.MaxAge == revocationMaxAge && actual.Duplicates == 2*time.Minute &&
		actual.DenyDelete && actual.DenyPurge && !actual.AllowRollup && actual.Mirror == nil &&
		len(actual.Sources) == 0 && actual.RePublish == nil && actual.SubjectTransform == nil
}
