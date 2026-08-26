// Package natsjetstream реализует eventing.Publisher поверх NATS JetStream.
package natsjetstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const maximumRuntimeFileBytes = 1 << 20

// ErrConnect классифицирует отказ initial connection, который допускает bounded retry вызывающей стороной.
var ErrConnect = errors.New("connect NATS JetStream")

// Config фиксирует точный поток окружения и идентичность TLS.
type Config struct {
	URL             string
	TLSServerName   string
	CAFile          string
	CertificateFile string
	PrivateKeyFile  string
	CredentialsFile string
	Stream          string
	Subjects        []string
	Replicas        int
	MaxMessageBytes int32
	MaxMessages     int64
	MaxBytes        int64
	MaxPerSubject   int64
	MaxAge          time.Duration
	DuplicateWindow time.Duration
	Discard         jetstream.DiscardPolicy
	ConnectTimeout  time.Duration
}

// Publisher владеет соединением NATS и синхронной публикацией JetStream.
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
	certificate, err := loadClientCertificate(
		config.CertificateFile,
		config.PrivateKeyFile,
	)
	if err != nil {
		return nil, err
	}
	connection, err := nats.Connect(
		config.URL,
		nats.Secure(&tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    pool,
			Certificates: []tls.Certificate{
				certificate,
			},
		}),
		nats.UserCredentials(config.CredentialsFile),
		nats.Timeout(config.ConnectTimeout),
		nats.NoEcho(),
		nats.MaxReconnects(10),
		nats.ReconnectWait(250*time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnect, err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		connection.Close()
		return nil, errors.New("construct NATS JetStream client")
	}
	return &Publisher{connection: connection, jetstream: js, config: config}, nil
}

// Check сверяет точный контракт потока и не создаёт ресурс.
func (publisher *Publisher) Check(ctx context.Context) error {
	stream, err := publisher.jetstream.Stream(ctx, publisher.config.Stream)
	if err != nil {
		return errors.New("read NATS JetStream stream")
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return errors.New("read NATS JetStream stream info")
	}
	if !streamCompatible(info.Config, publisher.config) {
		return errors.New("NATS JetStream stream contract mismatch")
	}
	return nil
}

// EnsureStream создаёт отсутствующий поток и закрыто отклоняет любое расхождение контракта.
func (publisher *Publisher) EnsureStream(ctx context.Context) error {
	if _, err := publisher.jetstream.Stream(ctx, publisher.config.Stream); err == nil {
		return publisher.Check(ctx)
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return errors.New("read NATS JetStream stream before bootstrap")
	}
	if _, err := publisher.jetstream.CreateStream(ctx, expectedStreamConfig(publisher.config)); err != nil {
		// Параллельный bootstrap мог создать тот же exact stream между read и create.
		if checkErr := publisher.Check(ctx); checkErr == nil {
			return nil
		}
		return errors.New("create NATS JetStream stream")
	}
	return publisher.Check(ctx)
}

func expectedStreamConfig(config Config) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:              config.Stream,
		Subjects:          slices.Clone(config.Subjects),
		Retention:         jetstream.LimitsPolicy,
		MaxMsgs:           config.MaxMessages,
		MaxBytes:          config.MaxBytes,
		MaxAge:            config.MaxAge,
		MaxMsgsPerSubject: config.MaxPerSubject,
		MaxMsgSize:        config.MaxMessageBytes,
		Storage:           jetstream.FileStorage,
		Replicas:          config.Replicas,
		Discard:           config.Discard,
		Duplicates:        config.DuplicateWindow,
		DenyDelete:        true,
		DenyPurge:         true,
	}
}

func streamCompatible(actual jetstream.StreamConfig, expected Config) bool {
	actualSubjects := slices.Clone(actual.Subjects)
	expectedSubjects := slices.Clone(expected.Subjects)
	slices.Sort(actualSubjects)
	slices.Sort(expectedSubjects)
	return actual.Name == expected.Stream &&
		slices.Equal(actualSubjects, expectedSubjects) &&
		actual.Storage == jetstream.FileStorage &&
		actual.Replicas == expected.Replicas &&
		actual.MaxMsgSize == expected.MaxMessageBytes &&
		actual.MaxMsgs == expected.MaxMessages &&
		actual.MaxBytes == expected.MaxBytes &&
		actual.MaxMsgsPerSubject == expected.MaxPerSubject &&
		actual.Retention == jetstream.LimitsPolicy &&
		actual.Discard == expected.Discard &&
		actual.MaxAge == expected.MaxAge &&
		actual.Duplicates == expected.DuplicateWindow &&
		actual.DenyDelete && actual.DenyPurge && !actual.AllowRollup &&
		actual.Mirror == nil && len(actual.Sources) == 0 &&
		actual.RePublish == nil && actual.SubjectTransform == nil
}

// Publish отправляет канонический конверт и проверяет подтверждение точного потока.
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

// PublishRaw публикует уже проверенный контрактом bounded payload в конкретный
// subject, входящий в закрытый набор stream filters. Метод нужен для AsyncAPI
// envelope, чья JSON-схема отличается от общего eventing.Envelope.
func (publisher *Publisher) PublishRaw(
	ctx context.Context,
	subject string,
	messageID string,
	payload []byte,
) (eventing.PublishReceipt, error) {
	if subject == "" || strings.ContainsAny(subject, "*> \t\r\n") ||
		messageID == "" || len(payload) == 0 || len(payload) > int(publisher.config.MaxMessageBytes) {
		return eventing.PublishReceipt{}, errors.New("raw NATS message is invalid")
	}
	allowed := false
	for _, filter := range publisher.config.Subjects {
		if subjectMatchesFilter(subject, filter) {
			allowed = true
			break
		}
	}
	if !allowed {
		return eventing.PublishReceipt{}, errors.New("raw NATS subject is not registered")
	}
	ack, err := publisher.jetstream.PublishMsg(
		ctx,
		&nats.Msg{Subject: subject, Data: payload},
		jetstream.WithMsgID(messageID),
		jetstream.WithExpectStream(publisher.config.Stream),
		jetstream.WithRetryAttempts(0),
	)
	if err != nil || ack == nil || ack.Stream != publisher.config.Stream || ack.Sequence == 0 {
		return eventing.PublishReceipt{}, errors.New("publish raw NATS JetStream event")
	}
	return eventing.PublishReceipt{Stream: ack.Stream, Sequence: ack.Sequence, Duplicate: ack.Duplicate}, nil
}

func subjectMatchesFilter(subject, filter string) bool {
	subjectTokens, filterTokens := strings.Split(subject, "."), strings.Split(filter, ".")
	if len(subjectTokens) != len(filterTokens) {
		return false
	}
	for index, token := range filterTokens {
		if token != "*" && token != subjectTokens[index] {
			return false
		}
	}
	return true
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
		!filepath.IsAbs(config.CertificateFile) ||
		!filepath.IsAbs(config.PrivateKeyFile) ||
		!filepath.IsAbs(config.CredentialsFile) ||
		config.Stream == "" || len(config.Subjects) == 0 ||
		config.Replicas < 1 || config.MaxMessageBytes < 1024 ||
		config.MaxMessages < 1 ||
		config.MaxBytes < int64(config.MaxMessageBytes) ||
		config.MaxPerSubject < 1 ||
		config.MaxPerSubject > config.MaxMessages ||
		config.MaxAge < time.Hour || config.MaxAge > 30*24*time.Hour ||
		config.DuplicateWindow < time.Minute ||
		config.DuplicateWindow > config.MaxAge ||
		(config.Discard != jetstream.DiscardOld && config.Discard != jetstream.DiscardNew) ||
		config.ConnectTimeout < 100*time.Millisecond ||
		config.ConnectTimeout > 10*time.Second {
		return errors.New("NATS JetStream configuration is invalid")
	}
	credentials, err := securefile.Read(config.CredentialsFile, maximumRuntimeFileBytes)
	if err != nil {
		return errors.New("NATS credential file is unsafe")
	}
	clear(credentials)
	seen := make(map[string]struct{}, len(config.Subjects))
	for _, subject := range config.Subjects {
		if !validSubjectFilter(subject) {
			return errors.New("NATS JetStream subject is invalid")
		}
		if _, duplicate := seen[subject]; duplicate {
			return errors.New("NATS JetStream subject is duplicated")
		}
		seen[subject] = struct{}{}
	}
	return nil
}

func validSubjectFilter(subject string) bool {
	if subject == "" || strings.TrimSpace(subject) != subject || strings.ContainsAny(subject, " \t\r\n") {
		return false
	}
	tokens := strings.Split(subject, ".")
	for index, token := range tokens {
		if token == "" || token == ">" || strings.Contains(token, ">") {
			return false
		}
		if token == "*" {
			continue
		}
		if strings.Contains(token, "*") || (index == len(tokens)-1 && token == ">") {
			return false
		}
	}
	return true
}

func loadClientCertificate(certificateFile string, privateKeyFile string) (tls.Certificate, error) {
	certificateRaw, err := securefile.Read(certificateFile, maximumRuntimeFileBytes)
	if err != nil {
		return tls.Certificate{}, errors.New("NATS client identity file is unsafe")
	}
	defer clear(certificateRaw)
	privateKeyRaw, err := securefile.Read(privateKeyFile, maximumRuntimeFileBytes)
	if err != nil {
		return tls.Certificate{}, errors.New("NATS client identity file is unsafe")
	}
	defer clear(privateKeyRaw)
	certificate, err := tls.X509KeyPair(certificateRaw, privateKeyRaw)
	if err != nil {
		return tls.Certificate{}, errors.New("load NATS client identity")
	}
	return certificate, nil
}

func loadCertificatePool(path string) (*x509.CertPool, error) {
	raw, err := securefile.Read(path, maximumRuntimeFileBytes)
	if err != nil {
		return nil, errors.New("NATS CA file is unsafe")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse NATS CA file")
	}
	return pool, nil
}
