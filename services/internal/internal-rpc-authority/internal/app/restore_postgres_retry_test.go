package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWaitForPostgresConnectivitySurvivesNetworkPolicyMaterialization supplies two fake connection refusals followed by success and requires exactly three calls with millisecond backoff. It simulates delayed connectivity without applying a NetworkPolicy or opening PostgreSQL despite the scenario name.
// TestWaitForPostgresConnectivitySurvivesNetworkPolicyMaterialization задаёт два фиктивных отказа подключения и последующий успех и требует ровно три вызова с миллисекундными задержками. Несмотря на название сценария, задержка подключения моделируется без применения NetworkPolicy или открытия PostgreSQL.
func TestWaitForPostgresConnectivitySurvivesNetworkPolicyMaterialization(t *testing.T) {
	t.Parallel()
	calls := 0
	err := waitForPostgresConnectivity(
		context.Background(),
		func(context.Context) error {
			calls++
			if calls < 3 {
				return errors.New("connection refused")
			}
			return nil
		},
		time.Millisecond,
		2*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("wait for PostgreSQL connectivity: %v", err)
	}
	if calls != 3 {
		t.Fatalf("unexpected PostgreSQL attempts: got=%d want=3", calls)
	}
}

// TestWaitForPostgresConnectivityHonorsStartupDeadline cancels the context inside the first failing ping and requires context.Canceled plus one call only. It tests cancellation propagation, not elapsed deadline time or a live database failure.
// TestWaitForPostgresConnectivityHonorsStartupDeadline отменяет контекст внутри первого неудачного ping и требует context.Canceled и единственный вызов. Проверяется распространение отмены, а не истёкший срок или отказ живой БД.
func TestWaitForPostgresConnectivityHonorsStartupDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := waitForPostgresConnectivity(
		ctx,
		func(context.Context) error {
			calls++
			cancel()
			return errors.New("connection refused")
		},
		time.Millisecond,
		2*time.Millisecond,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("startup deadline was not preserved: %v", err)
	}
	if calls != 1 {
		t.Fatalf("connectivity check continued after cancellation: calls=%d", calls)
	}
}
