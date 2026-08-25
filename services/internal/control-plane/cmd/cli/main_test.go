package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/natsjetstream"
)

type fakeBrokerPublisher struct {
	ensureCalls int
	closeCalls  int
	ensureErr   error
	closeErr    error
}

func (publisher *fakeBrokerPublisher) EnsureStream(context.Context) error {
	publisher.ensureCalls++
	return publisher.ensureErr
}

func (publisher *fakeBrokerPublisher) Close() error {
	publisher.closeCalls++
	return publisher.closeErr
}

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
