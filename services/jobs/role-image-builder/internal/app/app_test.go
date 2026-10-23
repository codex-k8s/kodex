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

// errStopReadinessMonitor terminates injected test waits deterministically without sleeping or changing production cancellation rules.
// errStopReadinessMonitor детерминированно завершает подставленные тестовые ожидания без сна или изменения правил отмены production.
var errStopReadinessMonitor = errors.New("stop readiness monitor")

// infrastructureCheckerFunc replaces the external infrastructure probe with a context-observing unit callback.
// infrastructureCheckerFunc заменяет внешнюю проверку инфраструктуры unit-обработчиком, наблюдающим контекст.
type infrastructureCheckerFunc func(context.Context) error

// Check delegates the exact supplied context and preserves the callback's error without real registry/BuildKit access.
// Check передаёт точный контекст и сохраняет ошибку обработчика без доступа к настоящим реестру или BuildKit.
func (check infrastructureCheckerFunc) Check(ctx context.Context) error {
	return check(ctx)
}

// TestStartupInfrastructureAllowsColdCheckBeyondInitializationBudget uses virtual time to exhaust short initialization but complete a fresh cold check.
// It verifies the separate readiness budget and cancellation of the successful probe's child context, not live image pulling.
// TestStartupInfrastructureAllowsColdCheckBeyondInitializationBudget виртуальным временем исчерпывает короткую инициализацию, но завершает новую холодную проверку.
// Проверяются отдельный бюджет готовности и отмена дочернего контекста успешной проверки, а не получение настоящего образа.
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

// TestStartupInfrastructureCancelsAndWaitsForCheckCleanup covers own timeout, lifecycle cancellation and an earlier parent deadline in virtual time.
// Each case delays synthetic cleanup after cancellation and requires synchronous join before the helper returns the original context error.
// TestStartupInfrastructureCancelsAndWaitsForCheckCleanup проверяет свой таймаут, отмену lifecycle и более ранний срок родителя виртуальным временем.
// В каждом случае искусственная очистка задерживается после отмены; помощник обязан синхронно дождаться её и вернуть исходную ошибку контекста.
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

// TestStartupInfrastructurePreservesFailureAndReleasesContext checks exact error propagation and release of the allocated probe context on failure.
// TestStartupInfrastructurePreservesFailureAndReleasesContext проверяет точную передачу ошибки и освобождение выделенного контекста проверки при сбое.
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

// TestReadinessMonitorUsesInfrastructureBudget checks the long infrastructure deadline rather than the short RPC budget and restores readiness.
// A supplied waiter captures the normal interval and stops after one successful synthetic probe without real sleeping or networking.
// TestReadinessMonitorUsesInfrastructureBudget проверяет длинный срок инфраструктуры вместо короткого RPC-бюджета и восстановление готовности.
// Подставленное ожидание фиксирует обычный интервал и завершает одну успешную искусственную проверку без настоящего сна или сети.
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

// TestReadinessMonitorClosesForBuildKitFailure supplies a synthetic infrastructure error and requires readiness to close before the injected stop.
// TestReadinessMonitorClosesForBuildKitFailure задаёт искусственную ошибку инфраструктуры и требует закрытия готовности до подставленной остановки.
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

// TestLoadConfigEnforcesColdPathReadinessBudget checks defaults and exact accepted/rejected initialization/readiness bounds via synthetic environment.
// It preserves the seven-minute maximum combined budget without starting the application, subprocesses or a real startup probe.
// TestLoadConfigEnforcesColdPathReadinessBudget проверяет умолчания и точные принимаемые и отклоняемые пределы запуска и готовности искусственным окружением.
// Сохраняется максимальный общий бюджет семь минут без запуска приложения, дочерних процессов или настоящей startup probe.
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

// readinessTestState creates isolated in-memory readiness/metrics and a discard logger without worker, RPC or telemetry runtime initialization.
// readinessTestState создаёт изолированные готовность и метрики в памяти и отбрасывающий logger без инициализации workers, RPC или telemetry runtime.
func readinessTestState() *runtimeState {
	return &runtimeState{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:   sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{}),
		readiness: serviceruntime.NewReadiness(),
	}
}

// remainingBudget requires a context deadline and returns its remaining duration using the current real or synctest clock.
// remainingBudget требует срок контекста и возвращает оставшуюся длительность по текущим настоящим либо synctest-часам.
func remainingBudget(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("check context has no deadline")
	}
	return time.Until(deadline)
}
