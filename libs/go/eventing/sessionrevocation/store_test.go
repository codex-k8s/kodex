package sessionrevocation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type fakeStream struct {
	info    *jetstream.StreamInfo
	message *jetstream.RawStreamMsg
	err     error
}

func (stream *fakeStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	return stream.info, stream.err
}

func (stream *fakeStream) GetLastMsgForSubject(context.Context, string) (*jetstream.RawStreamMsg, error) {
	return stream.message, stream.err
}

type fakeJetStream struct {
	ack     *jetstream.PubAck
	err     error
	subject string
	payload []byte
	header  nats.Header
}

func (stream *fakeJetStream) Stream(context.Context, string) (jetstream.Stream, error) {
	return nil, errors.New("unexpected stream lookup")
}

func (stream *fakeJetStream) PublishMsg(_ context.Context, message *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	stream.subject = message.Subject
	stream.payload = append([]byte(nil), message.Data...)
	stream.header = nats.Header{}
	for key, values := range message.Header {
		stream.header[key] = append([]string(nil), values...)
	}
	return stream.ack, stream.err
}

func TestRevocationStreamContractIsExact(t *testing.T) {
	t.Parallel()
	connection := natsjetstream.Config{URL: "tls://nats.example.test:4222", Replicas: 1}
	config := StreamConfig(connection)
	if config.Stream != StreamName || config.URL != connection.URL || config.MaxPerSubject != 1 ||
		config.MaxAge != 2*time.Hour || config.MaxBytes != 16<<20 {
		t.Fatalf("unexpected revocation stream config: %#v", config)
	}
	actual := jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{SubjectFilter}, Storage: jetstream.FileStorage,
		Replicas: 1, Retention: jetstream.LimitsPolicy, Discard: jetstream.DiscardNew,
		MaxMsgSize: maximumValueBytes, MaxMsgs: maximumRevocations, MaxBytes: maximumBytes,
		MaxMsgsPerSubject: 1, MaxAge: revocationMaxAge, Duplicates: 2 * time.Minute,
		DenyDelete: true, DenyPurge: true,
	}
	if !compatible(actual, 1) {
		t.Fatal("canonical revocation stream was rejected")
	}
	actual.MaxMsgsPerSubject = 2
	if compatible(actual, 1) {
		t.Fatal("revocation stream with history was accepted")
	}
}

func TestRevokeAndReadUseAuthoritativeStream(t *testing.T) {
	t.Parallel()
	sessionID := "4e5785f6-6bc6-4e8c-b6a1-ec4f8d2ab238"
	jetStream := &fakeJetStream{ack: &jetstream.PubAck{Stream: StreamName, Sequence: 9}}
	stream := &fakeStream{}
	store := &Store{jetstream: jetStream, stream: stream, replicas: 1, timeout: time.Second}
	if err := store.Revoke(context.Background(), sessionID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if jetStream.subject != subject(sessionID) || string(jetStream.payload) != revokedValue {
		t.Fatalf("unexpected revocation record: %q/%q", jetStream.subject, jetStream.payload)
	}
	stream.message = &jetstream.RawStreamMsg{Subject: subject(sessionID), Sequence: 9, Data: []byte(revokedValue)}
	if revoked, err := store.Revoked(context.Background(), sessionID); err != nil || !revoked {
		t.Fatalf("read revocation = %t/%v", revoked, err)
	}
}

func TestRevocationReadFailsClosed(t *testing.T) {
	t.Parallel()
	sessionID := "4e5785f6-6bc6-4e8c-b6a1-ec4f8d2ab238"
	store := &Store{jetstream: &fakeJetStream{}, stream: &fakeStream{err: errors.New("NATS unavailable")}, replicas: 1, timeout: time.Second}
	if revoked, err := store.Revoked(context.Background(), sessionID); err == nil || revoked {
		t.Fatalf("unavailable store = %t/%v", revoked, err)
	}
	store.stream = &fakeStream{message: &jetstream.RawStreamMsg{Subject: subject(sessionID), Sequence: 10, Data: []byte("corrupt")}}
	if revoked, err := store.Revoked(context.Background(), sessionID); err == nil || revoked {
		t.Fatalf("corrupt record = %t/%v", revoked, err)
	}
}

func TestUnknownSessionIsNotRevoked(t *testing.T) {
	t.Parallel()
	store := &Store{jetstream: &fakeJetStream{}, stream: &fakeStream{err: jetstream.ErrMsgNotFound}, replicas: 1, timeout: time.Second}
	if revoked, err := store.Revoked(context.Background(), "4e5785f6-6bc6-4e8c-b6a1-ec4f8d2ab238"); err != nil || revoked {
		t.Fatalf("unknown session = %t/%v", revoked, err)
	}
}

func TestConsumeOnceUsesSubjectCASAndRejectsReplay(t *testing.T) {
	t.Parallel()
	sessionID := "4e5785f6-6bc6-4e8c-b6a1-ec4f8d2ab238"
	jetStream := &fakeJetStream{ack: &jetstream.PubAck{Stream: StreamName, Sequence: 11}}
	stream := &fakeStream{}
	store := &Store{jetstream: jetStream, stream: stream, replicas: 1, timeout: time.Second}
	won, err := store.ConsumeOnce(context.Background(), sessionID)
	if err != nil || !won {
		t.Fatalf("first consume = %t/%v", won, err)
	}
	if jetStream.header.Get(jetstream.ExpectedStreamHeader) != StreamName ||
		jetStream.header.Get(jetstream.ExpectedLastSubjSeqHeader) != "0" ||
		jetStream.header.Get(jetstream.MsgIDHeader) == "" {
		t.Fatalf("consume CAS headers are incomplete: %v", jetStream.header)
	}

	jetStream.ack = nil
	jetStream.err = errors.New("wrong last sequence")
	stream.message = &jetstream.RawStreamMsg{Subject: subject(sessionID), Sequence: 11, Data: []byte(revokedValue)}
	won, err = store.ConsumeOnce(context.Background(), sessionID)
	if err != nil || won {
		t.Fatalf("replayed consume = %t/%v", won, err)
	}
}
