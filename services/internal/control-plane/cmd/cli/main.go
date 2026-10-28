package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/kodex/libs/go/eventing/sessionrevocation"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// migrations embeds the canonical SQL migration registry used by CLI up/status and gated upgrade fixtures.
// migrations встраивает канонический SQL migration registry для CLI up, status и явно разрешённых upgrade fixtures.
//
//go:embed migrations/*.sql
var migrations embed.FS

// main creates the CLI root context, dispatches arguments through run and exits nonzero after
// reporting a returned error.
// main создаёт корневой CLI context, передаёт аргументы в run и завершает процесс с ненулевым
// кодом после сообщения возвращённой ошибки.
func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:]); err != nil {
		reportCLIFailure(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches broker bootstrap or PostgreSQL check/up/status with a bounded initial connection probe.
// The migration itself retains the caller context and is never retried by connection diagnostics.
// run выбирает broker bootstrap либо PostgreSQL check/up/status с ограниченным начальным ping.
// Сама миграция сохраняет caller context и никогда не повторяется диагностикой соединения.
func run(ctx context.Context, args []string) error {
	if len(args) == 2 && args[0] == "broker" && args[1] == "bootstrap" {
		return bootstrapBroker(ctx)
	}
	if len(args) != 1 || (args[0] != "up" && args[0] != "status" && args[0] != "check") {
		return cliFailure(stageArguments, nil)
	}
	return runPostgres(ctx, args[0], os.Stdout, postgresCLIDependencies{
		open: openPostgresDatabase, migrate: applyPostgresMigrations,
	})
}

// bootstrapBroker ensures control-plane, session-revocation and browser-state streams in sequence
// with a separate retry budget per stream, limiting the legacy message-size upgrade to the
// control-plane bootstrap adapter.
// bootstrapBroker последовательно обеспечивает control-plane, session-revocation и browser-state
// streams с отдельным retry budget каждого stream, ограничивая legacy message-size upgrade
// control-plane bootstrap adapter.
func bootstrapBroker(ctx context.Context) error {
	config, timeout, err := loadBrokerBootstrapConfig()
	if err != nil {
		return err
	}
	for _, stream := range []natsjetstream.Config{config, sessionrevocation.StreamConfig(config), browserstate.StreamConfig(config)} {
		if err := bootstrapBrokerWithRetry(ctx, stream, timeout, brokerRetryPolicy{
			initial: 250 * time.Millisecond,
			maximum: 5 * time.Second,
		}, func(config natsjetstream.Config) (brokerPublisher, error) {
			publisher, err := natsjetstream.New(config)
			if err != nil {
				return nil, err
			}
			if config.Stream == "CONTROL_PLANE" {
				return &controlPlaneBootstrapPublisher{publisher}, nil
			}
			return publisher, nil
		}); err != nil {
			return err
		}
	}
	fmt.Println(brokerBootstrapCompleted)
	return nil
}

// controlPlaneBootstrapPublisher wraps the environment-owned bootstrap publisher so only this path
// accepts the exact previous control-plane message limit during forward upgrade.
// controlPlaneBootstrapPublisher оборачивает environment-owned bootstrap publisher, чтобы только
// этот путь принимал точный прежний control-plane message limit при forward upgrade.
type controlPlaneBootstrapPublisher struct{ *natsjetstream.Publisher }

// EnsureStream delegates bootstrap reconciliation with the exact64 KiB legacy limit rather than
// weakening the regular publisher stream checks.
// EnsureStream передаёт bootstrap reconciliation с точным legacy пределом64 KiB без ослабления
// обычных publisher stream checks.
func (publisher *controlPlaneBootstrapPublisher) EnsureStream(ctx context.Context) error {
	return publisher.EnsureStreamWithMessageLimitUpgrade(ctx, 64<<10)
}

// brokerPublisher defines the ensure/close lifecycle needed by one bootstrap attempt without
// exposing broker publish operations.
// brokerPublisher задаёт lifecycle ensure и close одной bootstrap attempt без предоставления
// broker publish operations.
type brokerPublisher interface {
	// EnsureStream checks or provisions the configured stream using the bounded bootstrap context.
	// EnsureStream проверяет либо подготавливает настроенный stream под ограниченным bootstrap
	// context.
	EnsureStream(context.Context) error
	// Close releases the constructed publisher after its single ensure attempt, including an ensure
	// failure.
	// Close освобождает созданный publisher после единственной ensure attempt, включая отказ ensure.
	Close() error
}

// brokerPublisherFactory constructs one attempt-specific publisher from a copied config whose
// connect timeout is capped by the remaining stream budget.
// brokerPublisherFactory создаёт publisher одной attempt по копии config с connect timeout,
// ограниченным остатком stream budget.
type brokerPublisherFactory func(natsjetstream.Config) (brokerPublisher, error)

// brokerRetryPolicy stores the positive initial delay and maximum exponential-backoff delay for
// transient connection retries.
// brokerRetryPolicy хранит положительную начальную задержку и предел exponential-backoff задержки
// повторов transient connection.
type brokerRetryPolicy struct {
	initial time.Duration
	maximum time.Duration
}

// minimumBrokerConnectTimeout prevents starting a new connect attempt with less than one hundred
// milliseconds left in its stream retry budget.
// minimumBrokerConnectTimeout запрещает начинать новую connect attempt при остатке stream retry
// budget меньше ста миллисекунд.
const minimumBrokerConnectTimeout = 100 * time.Millisecond

// brokerBootstrapCompleted holds the fixed success diagnostic for the pinned control-plane stream
// message size without interpolating credentials.
// brokerBootstrapCompleted хранит фиксированную диагностику успешного bootstrap закреплённого
// control-plane stream message size без подстановки credentials.
const brokerBootstrapCompleted = "control-plane broker bootstrap completed: stream=CONTROL_PLANE maximum_message_bytes=262144"

// loadBrokerBootstrapConfig parses the required replica/storage/retry settings and assembles the
// pinned stream configuration plus TLS/credential file locators without reading their contents.
// loadBrokerBootstrapConfig разбирает обязательные replica, storage и retry настройки и собирает
// закреплённую stream configuration и TLS и credential file locators без чтения их содержимого.
func loadBrokerBootstrapConfig() (natsjetstream.Config, time.Duration, error) {
	replicas, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CONTROL_PLANE_NATS_REPLICAS")))
	if err != nil {
		return natsjetstream.Config{}, 0, errors.New("CONTROL_PLANE_NATS_REPLICAS is invalid")
	}
	maximumBytes, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("CONTROL_PLANE_NATS_MAX_BYTES")), 10, 64)
	if err != nil {
		return natsjetstream.Config{}, 0, errors.New("CONTROL_PLANE_NATS_MAX_BYTES is invalid")
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(os.Getenv("CONTROL_PLANE_BROKER_BOOTSTRAP_TIMEOUT")))
	if err != nil || timeout < time.Second || timeout > 5*time.Minute {
		return natsjetstream.Config{}, 0, errors.New("CONTROL_PLANE_BROKER_BOOTSTRAP_TIMEOUT is invalid")
	}
	return natsjetstream.Config{
		URL: os.Getenv("CONTROL_PLANE_NATS_URL"), TLSServerName: os.Getenv("CONTROL_PLANE_NATS_TLS_SERVER_NAME"),
		CAFile: os.Getenv("CONTROL_PLANE_NATS_CA_FILE"), CertificateFile: os.Getenv("CONTROL_PLANE_NATS_CERTIFICATE_FILE"),
		PrivateKeyFile: os.Getenv("CONTROL_PLANE_NATS_PRIVATE_KEY_FILE"), CredentialsFile: os.Getenv("CONTROL_PLANE_NATS_CREDENTIALS_FILE"),
		Stream: "CONTROL_PLANE", Subjects: []string{"control_plane.run.*.*.events", "control_plane.platform.*.events"},
		Replicas: replicas, MaxMessageBytes: runtimecontract.MaximumControlPlaneStreamMessageBytes, MaxMessages: 10_000_000, MaxBytes: maximumBytes,
		MaxPerSubject: 1_000_000, MaxAge: 30 * 24 * time.Hour, DuplicateWindow: 2 * time.Minute, ConnectTimeout: 5 * time.Second,
	}, timeout, nil
}

// bootstrapBrokerWithRetry retries only constructor ErrConnect within one stream deadline, capping
// connect time/backoff; ensure and close failures return immediately after the publisher is
// closed.
// bootstrapBrokerWithRetry повторяет только constructor ErrConnect в одном stream deadline,
// ограничивая connect time и backoff; ошибки ensure и close возвращаются сразу после закрытия
// publisher.
func bootstrapBrokerWithRetry(
	ctx context.Context,
	config natsjetstream.Config,
	timeout time.Duration,
	policy brokerRetryPolicy,
	factory brokerPublisherFactory,
) error {
	if timeout <= 0 || policy.initial <= 0 || policy.maximum < policy.initial || factory == nil {
		return errors.New("NATS bootstrap retry policy is invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := bounded.Deadline()

	delay := policy.initial
	var lastErr error
	for {
		if err := bounded.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("connect NATS within bootstrap timeout: %w", lastErr)
			}
			return fmt.Errorf("NATS bootstrap context ended: %w", err)
		}
		remaining := time.Until(deadline)
		if remaining < minimumBrokerConnectTimeout {
			if lastErr != nil {
				return fmt.Errorf("connect NATS within bootstrap timeout: %w", lastErr)
			}
			return errors.New("NATS bootstrap timeout expired before connection attempt")
		}
		attempt := config
		attempt.ConnectTimeout = min(config.ConnectTimeout, remaining)
		publisher, err := factory(attempt)
		if err == nil {
			ensureErr := publisher.EnsureStream(bounded)
			closeErr := publisher.Close()
			if ensureErr != nil {
				return fmt.Errorf("bootstrap NATS stream: %w", ensureErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close NATS publisher after bootstrap: %w", closeErr)
			}
			return nil
		}
		if !errors.Is(err, natsjetstream.ErrConnect) {
			return fmt.Errorf("construct NATS publisher: %w", err)
		}
		lastErr = err

		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-bounded.Done():
			timer.Stop()
			return fmt.Errorf("connect NATS within bootstrap timeout: %w", err)
		case <-timer.C:
		}
		if delay < policy.maximum {
			delay = min(delay*2, policy.maximum)
		}
	}
}
