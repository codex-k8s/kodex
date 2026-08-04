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
	"strconv"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/libs/go/eventing/postgresinbox"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/observability"
	postgresprincipal "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/repository/postgres/principal"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

const (
	subject               = "control_plane.runtime_configuration_changed"
	eventName             = subject
	consumerName          = "runtime-controller"
	consumerScope         = "v1"
	streamMaxMessages     = int64(10_000_000)
	streamMaxBytes        = int64(32 << 30)
	streamMaxPerSubject   = int64(5_000_000)
	streamMaxMessageBytes = int32(256 << 10)
	consumerMaxDeliver    = 8
	consumerMaxAckPending = 64
)

var streamSubjects = []string{
	"control_plane.runtime_configuration_changed",
	"control_plane.schedule_changed",
}

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
	jetStream    nats.JetStreamContext
	subscription *nats.Subscription
	processor    *postgresinbox.Processor
	pool         *pgxpool.Pool
	controlPlane *controlplane.Client
	effect       postgresinbox.EffectOperation
	consumer     postgresinbox.Consumer
	fetchTimeout time.Duration
	observer     *internalobservability.Metrics
	report       func(context.Context, error)
	stream       string
	durable      string
	replicas     int
}

type operatorScope struct {
	organization string
	project      string
}

type operatorScopeKey struct{}

type inboxAuthorizer struct{}

func (inboxAuthorizer) AuthorizeOperator(ctx context.Context, target postgresinbox.OperatorTarget) (postgresinbox.OperatorAuthority, error) {
	authority := postgresinbox.OperatorAuthority{Actor: "runtime-controller"}
	if target.Action != postgresinbox.OperatorActionRecover {
		return authority, nil
	}
	scope, ok := ctx.Value(operatorScopeKey{}).(operatorScope)
	if !ok || uuid.Validate(scope.organization) != nil || uuid.Validate(scope.project) != nil {
		return postgresinbox.OperatorAuthority{}, errors.New("runtime inbox recovery authority is missing")
	}
	keyDigest := sha256.Sum256([]byte(target.IdempotencyKey))
	authority.Organization = scope.organization
	authority.Project = scope.project
	authority.Operation = "recover"
	authority.KeyHash = keyDigest
	return authority, nil
}

type inboxObserver struct {
	metrics *internalobservability.Metrics
}

func (observer inboxObserver) Observe(operation postgresinbox.Operation, outcome postgresinbox.Outcome) {
	observer.metrics.ObserveInboxOperation(string(operation), string(outcome))
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
	}, postgresinbox.WithEffectOperations(effect),
		postgresinbox.WithObserver(inboxObserver{metrics: observer}),
		postgresinbox.WithOperatorAuthorizer(inboxAuthorizer{}))
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
	if err != nil || !streamCompatible(streamInfo.Config, config.Replicas) {
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
		AckWait: 30 * time.Second, MaxDeliver: consumerMaxDeliver, MaxAckPending: consumerMaxAckPending}
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
	return &Consumer{connection: connection, jetStream: jetStream, subscription: subscription, processor: processor,
		pool: pool, controlPlane: controlPlane, effect: effect,
		consumer:     postgresinbox.Consumer{Name: consumerName, Scope: consumerScope},
		fetchTimeout: config.FetchTimeout, observer: observer, report: report,
		stream: config.Stream, durable: config.Durable, replicas: config.Replicas}, nil
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
	canonical, err := envelope.Marshal()
	if err != nil {
		return consumer.reject(ctx, message, err)
	}
	eventDigest := sha256.Sum256(canonical)
	operatorCtx := context.WithValue(ctx, operatorScopeKey{}, operatorScope{
		organization: envelope.OrganizationID,
		project:      change.ProjectID,
	})
	claim, result, err := consumer.processor.Acquire(operatorCtx, consumer.consumer, envelope)
	if err != nil {
		return consumer.rejectWithRecovery(operatorCtx, message, envelope, eventDigest, err)
	}
	if result.Durable {
		return consumer.applyBrokerResult(operatorCtx, message, envelope, eventDigest, result)
	}
	var snapshot json.RawMessage
	if change.ResourceKind == "RUNTIME_REVISION" {
		revision, err := consumer.controlPlane.GetRevision(ctx, change.ResourceID, change.ResourceVersion)
		if err != nil {
			return consumer.persistHydrationFailure(operatorCtx, message, envelope, eventDigest, claim, err)
		}
		snapshot, err = json.Marshal(revision)
		if err != nil {
			return consumer.persistHydrationFailure(operatorCtx, message, envelope, eventDigest, claim, err)
		}
	} else {
		snapshot, err = consumer.controlPlane.GetResourceSnapshot(ctx, change.ResourceID, change.ResourceKind, change.ResourceVersion)
		if err != nil {
			return consumer.persistHydrationFailure(operatorCtx, message, envelope, eventDigest, claim, err)
		}
	}
	effectRaw, err := json.Marshal(projectionInput{Envelope: envelope, Snapshot: snapshot, EventSHA256: fmt.Sprintf("%x", eventDigest)})
	if err != nil {
		return consumer.persistHydrationFailure(operatorCtx, message, envelope, eventDigest, claim, err)
	}
	result, err = consumer.processor.ApplyClaim(operatorCtx, claim, envelope,
		func(_ context.Context, transaction postgresinbox.EffectTx, _ postgresinbox.EventSnapshot) error {
			_, err := transaction.Call(consumer.effect, effectRaw)
			return err
		})
	if err != nil {
		return consumer.rejectWithRecovery(operatorCtx, message, envelope, eventDigest, err)
	}
	return consumer.applyBrokerResult(operatorCtx, message, envelope, eventDigest, result)
}

func (consumer *Consumer) persistHydrationFailure(
	ctx context.Context,
	message *nats.Msg,
	envelope eventing.Envelope,
	eventDigest [sha256.Size]byte,
	claim postgresinbox.Claim,
	cause error,
) error {
	result, err := consumer.processor.ApplyClaim(ctx, claim, envelope,
		func(context.Context, postgresinbox.EffectTx, postgresinbox.EventSnapshot) error {
			return postgresinbox.NewEffectFailure("hydrate_failed", true, cause)
		})
	if err != nil {
		return consumer.rejectWithRecovery(ctx, message, envelope, eventDigest, err)
	}
	return consumer.applyBrokerResult(ctx, message, envelope, eventDigest, result)
}

func (consumer *Consumer) applyBrokerResult(
	ctx context.Context,
	message *nats.Msg,
	envelope eventing.Envelope,
	eventDigest [sha256.Size]byte,
	result postgresinbox.Result,
) error {
	if result.Durable && result.Action == postgresinbox.BrokerActionACK {
		if err := message.Ack(); err != nil {
			return errors.New("ack runtime JetStream message")
		}
		consumer.observer.Observe("event_consume", "consumed")
		return nil
	}
	return consumer.rejectWithRecovery(ctx, message, envelope, eventDigest,
		errors.New("runtime inbox result requires retry"))
}

func (consumer *Consumer) rejectWithRecovery(
	ctx context.Context,
	message *nats.Msg,
	envelope eventing.Envelope,
	eventDigest [sha256.Size]byte,
	cause error,
) error {
	metadata, metadataErr := message.Metadata()
	if metadataErr == nil && metadata.NumDelivered >= consumerMaxDeliver {
		decision, readErr := consumer.processor.ReadDeliveryOutcome(ctx, postgresinbox.DeliveryOutcomeRequest{
			Consumer: consumer.consumer, EventID: envelope.EventID, EventDigest: eventDigest,
		})
		if readErr == nil && decision.Durable && decision.Directive == postgresinbox.RecoveryACKEligible &&
			decision.Action == postgresinbox.BrokerActionACK {
			if err := message.Ack(); err != nil {
				return errors.New("ack recovered runtime JetStream message")
			}
			consumer.observer.Observe("event_consume", "recovered")
			return nil
		}
		blockage, blockageErr := consumer.processor.GetBlockage(ctx, consumer.consumer, envelope.EventID)
		if blockageErr == nil {
			evidence := sha256.Sum256([]byte(envelope.EventID + ":max-deliver:" + strconv.FormatUint(metadata.NumDelivered, 10)))
			key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-inbox-recover:"+envelope.EventID+":"+strconv.FormatUint(uint64(blockage.LeaseGeneration), 10))).String()
			_, recoverErr := consumer.processor.Recover(ctx, postgresinbox.RecoveryRequest{
				Consumer: consumer.consumer, IdempotencyKey: key,
				EventID: envelope.EventID, EventDigest: eventDigest,
				ExpectedGeneration: blockage.LeaseGeneration, ExpectedFence: blockage.LeaseFence,
				Reason: "broker_max_deliver_exhausted", EvidenceDigest: evidence,
			})
			if recoverErr != nil {
				cause = errors.Join(cause, recoverErr)
			} else {
				recovered, outcomeErr := consumer.processor.ReadDeliveryOutcome(ctx, postgresinbox.DeliveryOutcomeRequest{
					Consumer: consumer.consumer, EventID: envelope.EventID, EventDigest: eventDigest,
				})
				if outcomeErr == nil && recovered.Durable && recovered.Directive == postgresinbox.RecoveryACKEligible &&
					recovered.Action == postgresinbox.BrokerActionACK {
					if err := message.Ack(); err != nil {
						return errors.New("ack repaired runtime JetStream message")
					}
					consumer.observer.Observe("event_consume", "recovered")
					return nil
				}
				cause = errors.Join(cause, outcomeErr)
			}
		} else {
			cause = errors.Join(cause, blockageErr, readErr)
		}
	}
	return consumer.reject(ctx, message, cause)
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
	streamInfo, err := consumer.jetStream.StreamInfo(consumer.stream)
	if err != nil || !streamCompatible(streamInfo.Config, consumer.replicas) {
		return errors.New("runtime JetStream stream readiness contract is incompatible")
	}
	durable, err := consumer.jetStream.ConsumerInfo(consumer.stream, consumer.durable)
	expected := nats.ConsumerConfig{Durable: consumer.durable, AckPolicy: nats.AckExplicitPolicy,
		FilterSubject: subject, DeliverPolicy: nats.DeliverAllPolicy, ReplayPolicy: nats.ReplayInstantPolicy,
		AckWait: 30 * time.Second, MaxDeliver: consumerMaxDeliver, MaxAckPending: consumerMaxAckPending}
	if err != nil || durable == nil || !consumerCompatible(durable.Config, expected) {
		return errors.New("runtime JetStream durable readiness contract is incompatible")
	}
	consumer.observer.SetInboxSnapshot(float64(durable.NumPending), float64(durable.NumAckPending), float64(durable.NumRedelivered), 0, 0)
	page, err := consumer.processor.ListBlockages(ctx, consumer.consumer, postgresinbox.BlockageListRequest{Limit: 100})
	if err != nil {
		return errors.New("runtime PostgreSQL inbox blockage read is unavailable")
	}
	blocked, deadLetter := 0, 0
	for _, item := range page.Items {
		blocked++
		if item.State == postgresinbox.BlockageStateDeadLetter {
			deadLetter++
		}
	}
	consumer.observer.SetInboxSnapshot(float64(durable.NumPending), float64(durable.NumAckPending), float64(durable.NumRedelivered), float64(blocked), float64(deadLetter))
	if page.Next != nil || deadLetter > 0 || durable.NumAckPending >= consumerMaxAckPending {
		return errors.New("runtime durable consumer backlog requires recovery")
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
	if err := postgresprincipal.Check(checkCtx, pool, config.PostgresPrincipal); err != nil {
		pool.Close()
		return nil, errors.New("runtime-controller PostgreSQL principal mismatch")
	}
	return pool, nil
}

func streamCompatible(config nats.StreamConfig, replicas int) bool {
	return config.Replicas == replicas && exactSubjects(config.Subjects, streamSubjects) &&
		config.Retention == nats.LimitsPolicy && config.Storage == nats.FileStorage &&
		config.Discard == nats.DiscardOld && config.MaxMsgs == streamMaxMessages &&
		config.MaxBytes == streamMaxBytes && config.MaxMsgsPerSubject == streamMaxPerSubject &&
		config.MaxMsgSize == streamMaxMessageBytes && config.MaxAge == 30*24*time.Hour &&
		config.Duplicates == 2*time.Minute && !config.AllowRollup && config.DenyDelete &&
		config.DenyPurge && config.Mirror == nil && len(config.Sources) == 0 &&
		config.RePublish == nil && config.SubjectTransform == nil
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
