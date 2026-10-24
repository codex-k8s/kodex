package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// fakeBrokerPublisher records synthetic ensure/close counts and injected errors without connecting
// to a broker.
// fakeBrokerPublisher учитывает синтетические ensure и close counts и внедрённые ошибки без
// подключения к broker.
type fakeBrokerPublisher struct {
	ensureCalls int
	closeCalls  int
	ensureErr   error
	closeErr    error
}

// TestBrokerBootstrapPinsUnicodeRunEventStreamLimit checks bootstrap uses the same pinned256 KiB
// control-plane message limit as the runtime contract.
// TestBrokerBootstrapPinsUnicodeRunEventStreamLimit проверяет использование bootstrap того же
// закреплённого control-plane message limit256 KiB, что и runtime contract.
func TestBrokerBootstrapPinsUnicodeRunEventStreamLimit(t *testing.T) {
	t.Setenv("CONTROL_PLANE_NATS_REPLICAS", "1")
	t.Setenv("CONTROL_PLANE_NATS_MAX_BYTES", "1073741824")
	t.Setenv("CONTROL_PLANE_BROKER_BOOTSTRAP_TIMEOUT", "60s")
	config, _, err := loadBrokerBootstrapConfig()
	if err != nil || config.Stream != "CONTROL_PLANE" || config.MaxMessageBytes != runtimecontract.MaximumControlPlaneStreamMessageBytes {
		t.Fatal("bootstrap and runtime event size pins differ")
	}
	if runtimecontract.MaximumControlPlaneStreamMessageBytes != 256<<10 {
		t.Fatal("stream limit is not the pinned forward revision")
	}
}

// EnsureStream increments the fixture ensure count and returns its configured synthetic error
// without provisioning a stream.
// EnsureStream увеличивает fixture ensure count и возвращает заданную синтетическую ошибку без
// подготовки stream.
func (publisher *fakeBrokerPublisher) EnsureStream(context.Context) error {
	publisher.ensureCalls++
	return publisher.ensureErr
}

// Close increments the fixture close count and returns its configured synthetic close error.
// Close увеличивает fixture close count и возвращает заданную синтетическую ошибку close.
func (publisher *fakeBrokerPublisher) Close() error {
	publisher.closeCalls++
	return publisher.closeErr
}

// TestBootstrapBrokerRetriesTransientConnectInOneProcess checks transient construction failures
// retry within the shared stream budget, then ensure and close occur exactly once.
// TestBootstrapBrokerRetriesTransientConnectInOneProcess проверяет повтор transient construction
// failures в общем stream budget с последующими ровно одним ensure и close.
func TestBootstrapBrokerRetriesTransientConnectInOneProcess(t *testing.T) {
	t.Parallel()
	publisher := &fakeBrokerPublisher{}
	attempts := 0
	err := bootstrapBrokerWithRetry(
		context.Background(),
		natsjetstream.Config{ConnectTimeout: 5 * time.Second},
		500*time.Millisecond,
		brokerRetryPolicy{initial: time.Millisecond, maximum: 2 * time.Millisecond},
		func(config natsjetstream.Config) (brokerPublisher, error) {
			attempts++
			if config.ConnectTimeout <= 0 || config.ConnectTimeout > 500*time.Millisecond {
				t.Fatalf("attempt connect timeout is outside the shared budget: %s", config.ConnectTimeout)
			}
			if attempts < 3 {
				return nil, fmt.Errorf("%w: transient network policy materialization", natsjetstream.ErrConnect)
			}
			return publisher, nil
		},
	)
	if err != nil {
		t.Fatalf("bootstrap after transient failures: %v", err)
	}
	if attempts != 3 || publisher.ensureCalls != 1 || publisher.closeCalls != 1 {
		t.Fatalf("unexpected lifecycle: attempts=%d ensure=%d close=%d", attempts, publisher.ensureCalls, publisher.closeCalls)
	}
}

// TestBootstrapBrokerDoesNotRetryPermanentConfigurationError checks a permanent constructor
// configuration error returns after one attempt rather than retrying unsafe credentials.
// TestBootstrapBrokerDoesNotRetryPermanentConfigurationError проверяет возврат постоянной
// constructor configuration error после одной attempt без повторения небезопасных credentials.
func TestBootstrapBrokerDoesNotRetryPermanentConfigurationError(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := bootstrapBrokerWithRetry(
		context.Background(),
		natsjetstream.Config{ConnectTimeout: time.Second},
		time.Second,
		brokerRetryPolicy{initial: time.Millisecond, maximum: 2 * time.Millisecond},
		func(natsjetstream.Config) (brokerPublisher, error) {
			attempts++
			return nil, errors.New("NATS credential file is unsafe")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "construct NATS publisher") {
		t.Fatalf("expected permanent construction error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("permanent error was retried %d times", attempts)
	}
}

// TestBootstrapBrokerStopsAfterSharedTimeout checks repeated transient connection failure
// preserves ErrConnect classification and terminates within the bounded test budget.
// TestBootstrapBrokerStopsAfterSharedTimeout проверяет сохранение классификации ErrConnect и
// завершение повторных transient connection failures в ограниченном test budget.
func TestBootstrapBrokerStopsAfterSharedTimeout(t *testing.T) {
	t.Parallel()
	attempts := 0
	started := time.Now()
	err := bootstrapBrokerWithRetry(
		context.Background(),
		natsjetstream.Config{ConnectTimeout: time.Second},
		120*time.Millisecond,
		brokerRetryPolicy{initial: 10 * time.Millisecond, maximum: 20 * time.Millisecond},
		func(natsjetstream.Config) (brokerPublisher, error) {
			attempts++
			return nil, fmt.Errorf("%w: unavailable", natsjetstream.ErrConnect)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "bootstrap timeout") || !errors.Is(err, natsjetstream.ErrConnect) {
		t.Fatalf("expected classified bounded failure, got %v", err)
	}
	if attempts < 2 {
		t.Fatalf("transient connection was not retried: attempts=%d", attempts)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("retry exceeded bounded test budget: %s", elapsed)
	}
}

// TestBootstrapBrokerHonorsCanceledParentContext checks a cancelled parent prevents any publisher
// construction and preserves context cancellation in the result.
// TestBootstrapBrokerHonorsCanceledParentContext проверяет, что отменённый parent запрещает
// создание publisher и сохраняет context cancellation в результате.
func TestBootstrapBrokerHonorsCanceledParentContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := bootstrapBrokerWithRetry(
		ctx,
		natsjetstream.Config{ConnectTimeout: time.Second},
		time.Second,
		brokerRetryPolicy{initial: time.Millisecond, maximum: 2 * time.Millisecond},
		func(natsjetstream.Config) (brokerPublisher, error) {
			attempts++
			return nil, nil
		},
	)
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Fatalf("canceled parent was not honored: attempts=%d err=%v", attempts, err)
	}
}
