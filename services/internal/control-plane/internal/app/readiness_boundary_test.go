package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// readinessBoundaryDependency injects one owned-infrastructure readiness error, counts calls and
// can cancel the loop after the final dependency check.
// readinessBoundaryDependency задаёт одну ошибку readiness owned infrastructure, считает вызовы и
// может отменить цикл после проверки последней зависимости.
type readinessBoundaryDependency struct {
	err    error
	cancel context.CancelFunc
	calls  int
}

// Check counts the synthetic dependency probe, optionally cancels the caller and returns the
// configured owned-infrastructure failure.
// Check считает синтетическую проверку зависимости, при необходимости отменяет caller и возвращает
// заданный отказ owned infrastructure.
func (dependency *readinessBoundaryDependency) Check(context.Context) error {
	dependency.calls++
	if dependency.cancel != nil {
		dependency.cancel()
	}
	return dependency.err
}

// Ready delegates synthetic service-owner readiness to the same counted dependency probe used by
// the bounded-loop fixture.
// Ready передаёт синтетическую readiness service владельца той же считаемой проверке зависимости
// ограниченного цикла оснастки.
func (dependency *readinessBoundaryDependency) Ready(ctx context.Context) error {
	return dependency.Check(ctx)
}

// CheckOutbox delegates synthetic owned-outbox readiness to the counted fixture probe without
// reading a real database or event queue.
// CheckOutbox передаёт синтетическую readiness owned outbox считаемой проверке оснастки без чтения
// реальных базы либо очереди событий.
func (dependency *readinessBoundaryDependency) CheckOutbox(ctx context.Context) error {
	return dependency.Check(ctx)
}

// TestReadinessDependsOnlyOnPostgreSQLAndNATS checks one deterministic readiness cycle evaluates
// service PostgreSQL, outbox and NATS exactly once, updates the prior opposite state and returns
// cancellation without optional dependencies.
// TestReadinessDependsOnlyOnPostgreSQLAndNATS проверяет ровно однократные service PostgreSQL,
// outbox и NATS проверки одним детерминированным readiness циклом, смену прежнего противоположного
// состояния и возврат отмены без optional dependencies.
func TestReadinessDependsOnlyOnPostgreSQLAndNATS(t *testing.T) {
	for _, failed := range []string{"none", "postgresql", "outbox", "nats"} {
		t.Run(failed, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dependencies := map[string]*readinessBoundaryDependency{}
			for _, name := range []string{"postgresql", "outbox", "nats"} {
				dependencies[name] = &readinessBoundaryDependency{}
				if name == failed {
					dependencies[name].err = errors.New("owned infrastructure unavailable")
				}
			}
			// Последняя проверка завершает один настоящий цикл без таймеров/гонок.
			dependencies["nats"].cancel = cancel
			readiness := serviceruntime.NewReadiness()
			// Начинаем с противоположного состояния, чтобы доказать сам переход.
			readiness.Set(failed != "none", "previous")
			worker := monitorReadiness(dependencies["postgresql"], dependencies["outbox"], dependencies["nats"], readiness, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{ReadinessInterval: time.Hour, ReadinessTimeout: time.Second})
			if err := worker(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			ready, reason := readiness.Ready()
			if ready != (failed == "none") {
				t.Fatalf("unexpected readiness: %v %s", ready, reason)
			}
			if failed != "none" && reason == "ready" {
				t.Fatal("infrastructure failure hidden")
			}
			for name, dependency := range dependencies {
				if dependency.calls != 1 {
					t.Fatalf("dependency %s was not checked", name)
				}
			}
		})
	}
}
