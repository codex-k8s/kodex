// Package sessionrevocation хранит авторитетный отзыв browser sessions в NATS JetStream.
package sessionrevocation

import (
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
	StreamName    = "CONTROL_API_SESSION_REVOCATIONS"
	SubjectFilter = "kodex.control_api.session_revocation.*"
	subjectPrefix = "kodex.control_api.session_revocation."
	revokedValue  = "revoked-v1"
)

const (
	maximumRevocations = 100_000
	maximumBytes       = 16 << 20
	maximumValueBytes  = 1024
	revocationMaxAge   = 2 * time.Hour
)

type streamAccess interface {
	Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error)
	GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error)
}

type jetStreamAccess interface {
	Stream(context.Context, string) (jetstream.Stream, error)
	PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type Store struct {
	jetstream jetStreamAccess
	stream    streamAccess
	replicas  int
	timeout   time.Duration
}

// StreamConfig возвращает один канонический контракт bootstrap/readback.
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

// ConsumeOnce атомарно закрывает browser session. Только первый caller,
// записавший revocation при отсутствии прежней записи, получает won=true.
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
		return false, errors.New("read session revocation")
	}
	if message == nil || message.Subject != subject(sessionID) || message.Sequence == 0 || string(message.Data) != revokedValue {
		return false, errors.New("session revocation record is invalid")
	}
	return true, nil
}

func subject(sessionID string) string { return subjectPrefix + sessionID }

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
