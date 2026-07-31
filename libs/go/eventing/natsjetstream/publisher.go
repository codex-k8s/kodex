// Package natsjetstream реализует eventing.Publisher поверх NATS JetStream.
package natsjetstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const maximumRuntimeFileBytes = 1 << 20

// Config фиксирует exact environment-owned stream и TLS identity.
type Config struct {
	URL             string
	TLSServerName   string
	CAFile          string
	CredentialsFile string
	Stream          string
	Subjects        []string
	Replicas        int
	MaxMessageBytes int32
	MaxAge          time.Duration
	DuplicateWindow time.Duration
	ConnectTimeout  time.Duration
}

// Publisher владеет NATS connection и synchronous JetStream publish.
type Publisher struct {
	connection *nats.Conn
	jetstream  jetstream.JetStream
	config     Config
}

// New создаёт TLS-only publisher без изменения stream.
func New(config Config) (*Publisher, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	pool, err := loadCertificatePool(config.CAFile)
	if err != nil {
		return nil, err
	}
	connection, err := nats.Connect(
		config.URL,
		nats.Secure(&tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    pool,
		}),
		nats.UserCredentials(config.CredentialsFile),
		nats.Timeout(config.ConnectTimeout),
		nats.NoEcho(),
		nats.MaxReconnects(10),
		nats.ReconnectWait(250*time.Millisecond),
	)
	if err != nil {
		return nil, errors.New("connect NATS JetStream")
	}
	js, err := jetstream.New(connection)
	if err != nil {
		connection.Close()
		return nil, errors.New("construct NATS JetStream client")
	}
	return &Publisher{connection: connection, jetstream: js, config: config}, nil
}

// Check сверяет exact stream contract и не создаёт ресурс.
func (publisher *Publisher) Check(ctx context.Context) error {
	stream, err := publisher.jetstream.Stream(ctx, publisher.config.Stream)
	if err != nil {
		return errors.New("read NATS JetStream stream")
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return errors.New("read NATS JetStream stream info")
	}
	actualSubjects := slices.Clone(info.Config.Subjects)
	expectedSubjects := slices.Clone(publisher.config.Subjects)
	slices.Sort(actualSubjects)
	slices.Sort(expectedSubjects)
	if info.Config.Name != publisher.config.Stream ||
		!slices.Equal(actualSubjects, expectedSubjects) ||
		info.Config.Storage != jetstream.FileStorage ||
		info.Config.Replicas != publisher.config.Replicas ||
		info.Config.MaxMsgSize != publisher.config.MaxMessageBytes ||
		info.Config.Retention != jetstream.LimitsPolicy ||
		info.Config.Discard != jetstream.DiscardOld ||
		info.Config.MaxAge != publisher.config.MaxAge ||
		info.Config.Duplicates != publisher.config.DuplicateWindow ||
		!info.Config.DenyDelete ||
		!info.Config.DenyPurge ||
		info.Config.AllowRollup ||
		info.Config.Mirror != nil ||
		len(info.Config.Sources) != 0 ||
		info.Config.RePublish != nil ||
		info.Config.SubjectTransform != nil {
		return errors.New("NATS JetStream stream contract mismatch")
	}
	return nil
}

// Publish отправляет canonical envelope и проверяет exact stream ack.
func (publisher *Publisher) Publish(
	ctx context.Context,
	envelope eventing.Envelope,
) (eventing.PublishReceipt, error) {
	payload, err := envelope.Marshal()
	if err != nil {
		return eventing.PublishReceipt{}, err
	}
	ack, err := publisher.jetstream.PublishMsg(
		ctx,
		&nats.Msg{Subject: envelope.EventName, Data: payload},
		jetstream.WithMsgID(envelope.EventID),
		jetstream.WithExpectStream(publisher.config.Stream),
		jetstream.WithRetryAttempts(0),
	)
	if err != nil || ack == nil || ack.Stream != publisher.config.Stream || ack.Sequence == 0 {
		return eventing.PublishReceipt{}, errors.New("publish NATS JetStream event")
	}
	return eventing.PublishReceipt{
		Stream:    ack.Stream,
		Sequence:  ack.Sequence,
		Duplicate: ack.Duplicate,
	}, nil
}

// Close ограниченно очищает и закрывает connection.
func (publisher *Publisher) Close() error {
	if publisher == nil || publisher.connection == nil {
		return nil
	}
	if err := publisher.connection.Drain(); err != nil {
		publisher.connection.Close()
		return errors.New("drain NATS connection")
	}
	publisher.connection.Close()
	return nil
}

func validateConfig(config Config) error {
	if config.URL == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.CredentialsFile) ||
		config.Stream == "" || len(config.Subjects) == 0 ||
		config.Replicas < 1 || config.MaxMessageBytes < 1024 ||
		config.MaxAge < time.Hour || config.MaxAge > 30*24*time.Hour ||
		config.DuplicateWindow < time.Minute ||
		config.DuplicateWindow > config.MaxAge ||
		config.ConnectTimeout < 100*time.Millisecond ||
		config.ConnectTimeout > 10*time.Second {
		return errors.New("NATS JetStream configuration is invalid")
	}
	credentials, err := os.Stat(config.CredentialsFile)
	if err != nil || !credentials.Mode().IsRegular() ||
		credentials.Size() <= 0 || credentials.Size() > maximumRuntimeFileBytes ||
		credentials.Mode().Perm()&0o007 != 0 {
		return errors.New("NATS credential file is unsafe")
	}
	seen := make(map[string]struct{}, len(config.Subjects))
	for _, subject := range config.Subjects {
		if subject == "" || strings.TrimSpace(subject) != subject ||
			strings.ContainsAny(subject, "*> \t\r\n") {
			return errors.New("NATS JetStream subject is invalid")
		}
		if _, duplicate := seen[subject]; duplicate {
			return errors.New("NATS JetStream subject is duplicated")
		}
		seen[subject] = struct{}{}
	}
	return nil
}

func loadCertificatePool(path string) (*x509.CertPool, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximumRuntimeFileBytes || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("NATS CA file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read NATS CA file")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse NATS CA file")
	}
	return pool, nil
}
