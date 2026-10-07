package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

var errStopReadinessMonitor = errors.New("stop readiness monitor")

type infrastructureCheckerFunc func(context.Context) error

func (check infrastructureCheckerFunc) Check(ctx context.Context) error {
	return check(ctx)
}

func TestStartupInfrastructureAllowsColdCheckBeyondInitializationBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := Config{StartupTimeout: 30 * time.Second, ReadinessTimeout: 3 * time.Minute}
		lifecycle := t.Context()
		startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
		defer cancelStartup()
		check := infrastructureCheckerFunc(func(ctx context.Context) error {
			time.Sleep(config.StartupTimeout + time.Second)
			return ctx.Err()
		})
		if err := check.Check(startup); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("legacy startup check error = %v, want DeadlineExceeded", err)
		}
		var infrastructure context.Context
		if err := checkStartupInfrastructure(lifecycle, infrastructureCheckerFunc(func(ctx context.Context) error {
			infrastructure = ctx
			if budget := remainingBudget(t, ctx); budget != config.ReadinessTimeout {
				t.Fatalf("startup infrastructure budget = %s, want %s", budget, config.ReadinessTimeout)
			}
			return check.Check(ctx)
		}), config); err != nil {
			t.Fatalf("cold startup check inherited the exhausted initialization budget: %v", err)
		}
		if !errors.Is(infrastructure.Err(), context.Canceled) {
			t.Fatal("successful infrastructure check retained its context timer")
		}
	})
}

func TestStartupInfrastructureCancelsAndWaitsForCheckCleanup(t *testing.T) {
	for _, test := range []struct {
		name           string
		parentDeadline time.Duration
		cancelAfter    time.Duration
		elapsed        time.Duration
		want           error
	}{
		{name: "own_timeout", elapsed: 3*time.Minute + 2*time.Second, want: context.DeadlineExceeded},
		{name: "lifecycle_cancel", cancelAfter: 5 * time.Second, elapsed: 7 * time.Second, want: context.Canceled},
		{name: "earlier_parent_deadline", parentDeadline: time.Second, elapsed: 3 * time.Second, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				lifecycle, cancelLifecycle := context.WithCancel(t.Context())
				defer cancelLifecycle()
				if test.parentDeadline != 0 {
					var cancelDeadline context.CancelFunc
					lifecycle, cancelDeadline = context.WithTimeout(lifecycle, test.parentDeadline)
					defer cancelDeadline()
				}
				if test.cancelAfter != 0 {
					go func() {
						time.Sleep(test.cancelAfter)
						cancelLifecycle()
					}()
				}
				joined := false
				started := time.Now()
				err := checkStartupInfrastructure(lifecycle, infrastructureCheckerFunc(func(ctx context.Context) error {
					<-ctx.Done()
					// Проверка возвращается только после собственного cancel/join cleanup.
					time.Sleep(2 * time.Second)
					joined = true
					return ctx.Err()
				}), Config{StartupTimeout: 30 * time.Second, ReadinessTimeout: 3 * time.Minute})
				if !errors.Is(err, test.want) || !joined || time.Since(started) != test.elapsed {
					t.Fatalf("startup check = %v, joined = %t, elapsed = %s", err, joined, time.Since(started))
				}
			})
		})
	}
}

func TestStartupInfrastructurePreservesFailureAndReleasesContext(t *testing.T) {
	checkFailure := errors.New("synthetic infrastructure failure")
	var infrastructure context.Context
	err := checkStartupInfrastructure(t.Context(), infrastructureCheckerFunc(func(ctx context.Context) error {
		infrastructure = ctx
		return checkFailure
	}), Config{ReadinessTimeout: 3 * time.Minute})
	if !errors.Is(err, checkFailure) || !errors.Is(infrastructure.Err(), context.Canceled) {
		t.Fatalf("startup failure = %v, infrastructure context = %v", err, infrastructure.Err())
	}
}

func TestReadinessMonitorUsesInfrastructureBudget(t *testing.T) {
	t.Parallel()
	var infrastructureBudget time.Duration
	state := readinessTestState()
	config := Config{
		RPCDeadline:       5 * time.Second,
		ReadinessInterval: 30 * time.Second,
		ReadinessTimeout:  3 * time.Minute,
	}
	worker := monitorLocalReadinessWithWait(
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
	if infrastructureBudget < 179*time.Second || infrastructureBudget > config.ReadinessTimeout {
		t.Fatalf("infrastructure budget = %s, want approximately %s", infrastructureBudget, config.ReadinessTimeout)
	}
	ready, _ := state.readiness.Ready()
	if !ready {
		t.Fatal("readiness was not restored after both checks succeeded")
	}
}

func TestReadinessMonitorClosesForBuildKitFailure(t *testing.T) {
	t.Parallel()
	state := readinessTestState()
	worker := monitorLocalReadinessWithWait(
		infrastructureCheckerFunc(func(context.Context) error {
			return errors.New("buildkit unavailable")
		}),
		state,
		Config{RPCDeadline: time.Second, ReadinessInterval: time.Second, ReadinessTimeout: 3 * time.Minute},
		func(context.Context, time.Duration) error { return errStopReadinessMonitor },
	)

	if err := worker(context.Background()); !errors.Is(err, errStopReadinessMonitor) {
		t.Fatalf("monitor error = %v, want %v", err, errStopReadinessMonitor)
	}
	ready, _ := state.readiness.Ready()
	if ready {
		t.Fatal("readiness stayed open while BuildKit was unavailable")
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
	if config.StartupTimeout != 30*time.Second {
		t.Fatalf("initialization startup timeout = %s, want 30s", config.StartupTimeout)
	}
	t.Setenv("ROLE_IMAGE_BUILDER_STARTUP_TIMEOUT", "120s")
	t.Setenv("ROLE_IMAGE_BUILDER_INFRASTRUCTURE_READINESS_TIMEOUT", "300s")
	maximum, err := loadConfig()
	if err != nil || maximum.StartupTimeout+maximum.ReadinessTimeout != 7*time.Minute {
		t.Fatalf("maximum bounded startup configuration = %s + %s, error = %v", maximum.StartupTimeout, maximum.ReadinessTimeout, err)
	}
	t.Setenv("ROLE_IMAGE_BUILDER_STARTUP_TIMEOUT", "121s")
	if _, err := loadConfig(); err == nil {
		t.Fatal("configuration accepted initialization above the startup probe contract")
	}
	t.Setenv("ROLE_IMAGE_BUILDER_STARTUP_TIMEOUT", "120s")
	t.Setenv("ROLE_IMAGE_BUILDER_INFRASTRUCTURE_READINESS_TIMEOUT", "301s")
	if _, err := loadConfig(); err == nil {
		t.Fatal("configuration accepted infrastructure readiness above the startup probe contract")
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
