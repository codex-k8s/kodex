package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

var errStopReadinessMonitor = errors.New("stop readiness monitor")

type authorityCheckerFunc func(context.Context) error

func (check authorityCheckerFunc) CheckLocalAuthority(ctx context.Context) error {
	return check(ctx)
}

type infrastructureCheckerFunc func(context.Context) error

func (check infrastructureCheckerFunc) Check(ctx context.Context) error {
	return check(ctx)
}

func TestReadinessMonitorSeparatesRPCAndInfrastructureBudgets(t *testing.T) {
	t.Parallel()
	var authorityBudget, infrastructureBudget time.Duration
	state := readinessTestState()
	config := Config{
		RPCDeadline:       5 * time.Second,
		ReadinessInterval: 30 * time.Second,
		ReadinessTimeout:  3 * time.Minute,
	}
	worker := monitorLocalReadinessWithWait(
		authorityCheckerFunc(func(ctx context.Context) error {
			authorityBudget = remainingBudget(t, ctx)
			return nil
		}),
		infrastructureCheckerFunc(func(ctx context.Context) error {
			infrastructureBudget = remainingBudget(t, ctx)
			return nil
		}),
		state,
		config,
		func(_ context.Context, interval time.Duration) error {
			if interval != config.ReadinessInterval {
				t.Fatalf("readiness interval = %s, want %s", interval, config.ReadinessInterval)
			}
			return errStopReadinessMonitor
		},
	)

	if err := worker(context.Background()); !errors.Is(err, errStopReadinessMonitor) {
		t.Fatalf("monitor error = %v, want %v", err, errStopReadinessMonitor)
	}
	if authorityBudget < 4*time.Second || authorityBudget > config.RPCDeadline {
		t.Fatalf("authority budget = %s, want approximately %s", authorityBudget, config.RPCDeadline)
	}
	if infrastructureBudget < 179*time.Second || infrastructureBudget > config.ReadinessTimeout {
		t.Fatalf("infrastructure budget = %s, want approximately %s", infrastructureBudget, config.ReadinessTimeout)
	}
	ready, _ := state.readiness.Ready()
	if !ready {
		t.Fatal("readiness was not restored after both checks succeeded")
	}
}

func TestReadinessMonitorSkipsBuildKitWhenLocalAuthorityIsUnavailable(t *testing.T) {
	t.Parallel()
	infrastructureCalls := 0
	state := readinessTestState()
	worker := monitorLocalReadinessWithWait(
		authorityCheckerFunc(func(context.Context) error { return errors.New("authority unavailable") }),
		infrastructureCheckerFunc(func(context.Context) error {
			infrastructureCalls++
			return nil
		}),
		state,
		Config{RPCDeadline: time.Second, ReadinessInterval: time.Second, ReadinessTimeout: 3 * time.Minute},
		func(context.Context, time.Duration) error { return errStopReadinessMonitor },
	)

	if err := worker(context.Background()); !errors.Is(err, errStopReadinessMonitor) {
		t.Fatalf("monitor error = %v, want %v", err, errStopReadinessMonitor)
	}
	if infrastructureCalls != 0 {
		t.Fatalf("infrastructure calls = %d, want 0", infrastructureCalls)
	}
	ready, _ := state.readiness.Ready()
	if ready {
		t.Fatal("readiness stayed open while local authority was unavailable")
	}
}

func TestLoadConfigEnforcesColdPathReadinessBudget(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256", strings.Repeat("a", 64))
	config, err := loadConfig()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if config.ReadinessTimeout != 3*time.Minute {
		t.Fatalf("infrastructure readiness timeout = %s, want %s", config.ReadinessTimeout, 3*time.Minute)
	}

	t.Setenv("ROLE_IMAGE_BUILDER_INFRASTRUCTURE_READINESS_TIMEOUT", "179s")
	if _, err := loadConfig(); err == nil {
		t.Fatal("configuration accepted an infrastructure readiness timeout below the cold-path budget")
	}
}

func readinessTestState() *runtimeState {
	return &runtimeState{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:   sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{}),
		readiness: serviceruntime.NewReadiness(),
	}
}

func remainingBudget(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("check context has no deadline")
	}
	return time.Until(deadline)
}
