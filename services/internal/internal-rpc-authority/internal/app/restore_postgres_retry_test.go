package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
