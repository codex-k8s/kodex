// Package eventconsumer реализует exact JetStream -> PostgreSQL inbox boundary.
package eventconsumer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/libs/go/eventing/postgresinbox"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

const (
	subject       = "control_plane.runtime_configuration_changed"
	eventName     = subject
	consumerName  = "runtime-controller"
	consumerScope = "v1"
)

type Config struct {
	NATSURL, NATSTLSServerName, NATSCAFile, NATSCertificateFile, NATSPrivateKeyFile, NATSCredentialsFile string
	Stream, Durable                                                                                      string
	Replicas                                                                                             int
	PostgresDSNFile, PostgresTLSServerName, PostgresCAFile, PostgresPrincipal                            string
	InstanceID                                                                                           string
	FetchTimeout                                                                                         time.Duration
}

type Consumer struct {
	connection   *nats.Conn
	subscription *nats.Subscription
	processor    *postgresinbox.Processor
	pool         *pgxpool.Pool
	controlPlane *controlplane.Client
	effect       postgresinbox.EffectOperation
	consumer     postgresinbox.Consumer
	fetchTimeout time.Duration
	observer     *internalobservability.Metrics
	report       func(context.Context, error)
}

type changeReference struct {
	ProjectID       string `json:"projectId"`
	ResourceID      string `json:"resourceId"`
	ResourceKind    string `json:"resourceKind"`
	ResourceState   string `json:"resourceState"`
	ResourceVersion uint64 `json:"resourceVersion"`
}

type projectionInput struct {
	Envelope    eventing.Envelope `json:"envelope"`
	Snapshot    json.RawMessage   `json:"authoritative_snapshot"`
	EventSHA256 string            `json:"event_sha256"`
}

func Open(
	ctx context.Context,
	config Config,
	controlPlane *controlplane.Client,
	observer *internalobservability.Metrics,
	report func(context.Context, error),
) (*Consumer, error) {
	if controlPlane == nil || observer == nil || report == nil ||
		config.NATSURL == "" || config.NATSTLSServerName == "" ||
		config.Stream == "" || config.Durable == "" || config.Replicas < 1 || config.Replicas > 5 ||
		config.InstanceID == "" || config.PostgresPrincipal == "" ||
		config.FetchTimeout < time.Second || config.FetchTimeout > 10*time.Second {
		return nil, errors.New("runtime event consumer configuration is invalid")
	}
	pool, err := openPostgres(ctx, config)
	if err != nil {
		return nil, err
	}
	effect, err := postgresinbox.NewEffectOperation("apply_projection", "runtime_controller", "apply_projection")
	if err != nil {
		pool.Close()
		return nil, err
	}
	processor, err := postgresinbox.New(pool, postgresinbox.Config{
		Schema: "runtime_controller", InstanceID: config.InstanceID,
		LeaseDuration: 30 * time.Second, EffectTimeout: 20 * time.Second, FinalizeTimeout: 5 * time.Second,
		InitialBackoff: time.Second, MaximumBackoff: time.Minute, MaxAttempts: 8, MaxRepairs: 3,
		RetentionHorizon: 35 * 24 * time.Hour, CleanupBatchSize: 100,
	}, postgresinbox.WithEffectOperations(effect))
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := processor.Check(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := controlPlane.Check(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	tlsConfig, err := exactTLS(config.NATSTLSServerName, config.NATSCAFile, config.NATSCertificateFile, config.NATSPrivateKeyFile)
	if err != nil {
		pool.Close()
		return nil, err
	}
	connection, err := nats.Connect(config.NATSURL,
		nats.Name("mattercodex-runtime-controller"), nats.Secure(tlsConfig),
		nats.UserCredentials(config.NATSCredentialsFile), nats.Timeout(3*time.Second),
		nats.MaxReconnects(-1), nats.ReconnectWait(time.Second),
	)
	if err != nil {
		pool.Close()
		return nil, errors.New("connect runtime JetStream consumer")
	}
	jetStream, err := connection.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		connection.Close()
		pool.Close()
		return nil, errors.New("open runtime JetStream context")
	}
	streamInfo, err := jetStream.StreamInfo(config.Stream)
	if err != nil || streamInfo.Config.Replicas != config.Replicas ||
		!exactSubjects(streamInfo.Config.Subjects, []string{subject}) ||
		streamInfo.Config.Retention != nats.LimitsPolicy || streamInfo.Config.Storage != nats.FileStorage {
		connection.Close()
		pool.Close()
		return nil, errors.New("runtime JetStream stream contract is incompatible")
	}
	durable, err := jetStream.ConsumerInfo(config.Stream, config.Durable)
	if err != nil && !errors.Is(err, nats.ErrConsumerNotFound) {
		connection.Close()
		pool.Close()
		return nil, errors.New("read runtime JetStream consumer contract")
	}
	expected := &nats.ConsumerConfig{Durable: config.Durable, AckPolicy: nats.AckExplicitPolicy,
		FilterSubject: subject, DeliverPolicy: nats.DeliverAllPolicy, ReplayPolicy: nats.ReplayInstantPolicy,
		AckWait: 30 * time.Second, MaxDeliver: 8, MaxAckPending: 64}
	if durable == nil {
		durable, err = jetStream.AddConsumer(config.Stream, expected)
	}
	if err != nil || !consumerCompatible(durable.Config, *expected) {
		connection.Close()
		pool.Close()
		return nil, errors.New("runtime JetStream durable consumer is incompatible")
	}
	subscription, err := jetStream.PullSubscribe(subject, config.Durable, nats.Bind(config.Stream, config.Durable))
	if err != nil {
		connection.Close()
		pool.Close()
		return nil, errors.New("attach runtime JetStream durable consumer")
	}
	return &Consumer{connection: connection, subscription: subscription, processor: processor,
		pool: pool, controlPlane: controlPlane, effect: effect,
		consumer:     postgresinbox.Consumer{Name: consumerName, Scope: consumerScope},
		fetchTimeout: config.FetchTimeout, observer: observer, report: report}, nil
}

func (consumer *Consumer) Run(ctx context.Context) error {
	for {
		messages, err := consumer.subscription.Fetch(1, nats.MaxWait(consumer.fetchTimeout))
		if errors.Is(err, nats.ErrTimeout) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("fetch runtime JetStream message")
		}
		for _, message := range messages {
			if err := consumer.process(ctx, message); err != nil {
				return err
			}
		}
	}
}

func (consumer *Consumer) process(ctx context.Context, message *nats.Msg) error {
	envelope, change, err := decode(message.Data)
	if err != nil {
		return consumer.reject(ctx, message, err)
	}
	var snapshot json.RawMessage
	if change.ResourceKind == "RUNTIME_REVISION" {
		revision, err := consumer.controlPlane.GetRevision(ctx, change.ResourceID, change.ResourceVersion)
		if err != nil {
			return consumer.reject(ctx, message, err)
		}
		snapshot, err = json.Marshal(revision)
		if err != nil {
			return consumer.reject(ctx, message, err)
		}
	} else {
		snapshot, err = consumer.controlPlane.GetResourceSnapshot(ctx, change.ResourceID, change.ResourceKind, change.ResourceVersion)
		if err != nil {
			return consumer.reject(ctx, message, err)
		}
	}
	canonical, err := envelope.Marshal()
	if err != nil {
		return consumer.reject(ctx, message, err)
	}
	eventDigest := sha256.Sum256(canonical)
	effectRaw, err := json.Marshal(projectionInput{Envelope: envelope, Snapshot: snapshot, EventSHA256: fmt.Sprintf("%x", eventDigest)})
	if err != nil {
		return consumer.reject(ctx, message, err)
	}
	result, err := consumer.processor.Process(ctx, consumer.consumer, envelope,
		func(_ context.Context, transaction postgresinbox.EffectTx, _ postgresinbox.EventSnapshot) error {
			_, err := transaction.Call(consumer.effect, effectRaw)
			return err
		})
	if err != nil {
		return consumer.reject(ctx, message, err)
	}
	if result.Durable && result.Action == postgresinbox.BrokerActionACK {
		if err := message.Ack(); err != nil {
			return errors.New("ack runtime JetStream message")
		}
		consumer.observer.Observe("event_consume", "consumed")
		return nil
	}
	return consumer.reject(ctx, message, errors.New("runtime inbox result is not a durable ACK"))
}

func (consumer *Consumer) reject(ctx context.Context, message *nats.Msg, cause error) error {
	consumer.observer.Observe("event_consume", "error")
	consumer.report(ctx, cause)
	if err := message.Nak(); err != nil {
		return errors.New("nack rejected runtime JetStream message")
	}
	return nil
}

func (consumer *Consumer) Check(ctx context.Context) error {
	if err := consumer.processor.Check(ctx); err != nil {
		return err
	}
	if !consumer.connection.IsConnected() || !consumer.subscription.IsValid() {
		return errors.New("runtime JetStream consumer is not ready")
	}
	return consumer.controlPlane.Check(ctx)
}

func (consumer *Consumer) Close(ctx context.Context) error {
	if consumer == nil {
		return nil
	}
	consumer.processor.Cancel()
	joinErr := consumer.processor.Join(ctx)
	unsubscribeErr := consumer.subscription.Unsubscribe()
	consumer.connection.Close()
	consumer.pool.Close()
	return errors.Join(joinErr, unsubscribeErr)
}

func decode(raw []byte) (eventing.Envelope, changeReference, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope eventing.Envelope
	if decoder.Decode(&envelope) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || envelope.Validate() != nil ||
		envelope.EventName != eventName || envelope.EventVersion != 1 || envelope.SchemaVersion != 1 ||
		uuid.Validate(envelope.OrganizationID) != nil {
		return eventing.Envelope{}, changeReference{}, errors.New("runtime event envelope is invalid")
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	dataDecoder.DisallowUnknownFields()
	var change changeReference
	if dataDecoder.Decode(&change) != nil || !errors.Is(dataDecoder.Decode(&struct{}{}), io.EOF) ||
		uuid.Validate(change.ProjectID) != nil || uuid.Validate(change.ResourceID) != nil ||
		!validRuntimeConfigurationKind(change.ResourceKind) || !validLifecycleState(change.ResourceState) ||
		change.ResourceVersion == 0 ||
		envelope.AggregateID != change.ResourceID || envelope.AggregateVersion != change.ResourceVersion ||
		envelope.AggregateType != change.ResourceKind {
		return eventing.Envelope{}, changeReference{}, errors.New("runtime change reference is invalid")
	}
	return envelope, change, nil
}

func validRuntimeConfigurationKind(value string) bool {
	switch value {
	case "PROJECT", "TEAM", "CHAT", "ROLE", "PROMPT_PROFILE", "CREDENTIAL_BINDING",
		"REPOSITORY_WORKSPACE", "INTEGRATION", "RUNTIME_REVISION", "SESSION", "TURN":
		return true
	default:
		return false
	}
}

func validLifecycleState(value string) bool {
	switch value {
	case "ACTIVE", "PAUSED", "ARCHIVED", "DELETION_PENDING", "DELETED", "QUEUED",
		"CLAIMED", "RUNNING", "WAITING_OWNER", "WAITING_EXTERNAL", "SUCCEEDED",
		"FAILED", "CANCELLED", "EXPIRED", "BLOCKED":
		return true
	default:
		return false
	}
}

func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	dsn, err := readBounded(config.PostgresDSNFile, 16<<10)
	if err != nil {
		return nil, err
	}
	poolConfig, err := pgxpool.ParseConfig(string(dsn))
	if err != nil {
		return nil, errors.New("parse runtime-controller PostgreSQL DSN")
	}
	if poolConfig.ConnConfig.Host != config.PostgresTLSServerName ||
		len(poolConfig.ConnConfig.Fallbacks) != 0 {
		return nil, errors.New("runtime-controller PostgreSQL endpoint mismatch")
	}
	tlsConfig, err := exactTLS(config.PostgresTLSServerName, config.PostgresCAFile, "", "")
	if err != nil {
		return nil, err
	}
	poolConfig.ConnConfig.TLSConfig = tlsConfig
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "runtime-controller-inbox"
	poolConfig.ConnConfig.RuntimeParams["search_path"] = "pg_catalog,runtime_controller,pg_temp"
	poolConfig.MaxConns = 8
	poolConfig.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open runtime-controller PostgreSQL pool")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var sessionUser string
	if err := pool.QueryRow(checkCtx, "SELECT session_user").Scan(&sessionUser); err != nil || sessionUser != config.PostgresPrincipal {
		pool.Close()
		return nil, errors.New("runtime-controller PostgreSQL principal mismatch")
	}
	return pool, nil
}

func exactTLS(serverName, caFile, certificateFile, privateKeyFile string) (*tls.Config, error) {
	caRaw, err := readBounded(caFile, 1<<20)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse runtime dependency CA")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots}
	if certificateFile != "" || privateKeyFile != "" {
		certificate, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
		if err != nil {
			return nil, errors.New("load runtime dependency client identity")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func readBounded(path string, maximum int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("runtime dependency file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read runtime dependency file")
	}
	return raw, nil
}

func exactSubjects(actual, expected []string) bool {
	left, right := append([]string(nil), actual...), append([]string(nil), expected...)
	sort.Strings(left)
	sort.Strings(right)
	return fmt.Sprint(left) == fmt.Sprint(right)
}

func consumerCompatible(actual, expected nats.ConsumerConfig) bool {
	return actual.Durable == expected.Durable && actual.AckPolicy == expected.AckPolicy &&
		actual.FilterSubject == expected.FilterSubject && actual.DeliverPolicy == expected.DeliverPolicy &&
		actual.ReplayPolicy == expected.ReplayPolicy && actual.AckWait == expected.AckWait &&
		actual.MaxDeliver == expected.MaxDeliver && actual.MaxAckPending == expected.MaxAckPending
}
