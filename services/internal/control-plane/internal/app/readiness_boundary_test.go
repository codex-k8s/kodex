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

type readinessBoundaryDependency struct {
	err    error
	cancel context.CancelFunc
	calls  int
}

func (dependency *readinessBoundaryDependency) Check(context.Context) error {
	dependency.calls++
	if dependency.cancel != nil {
		dependency.cancel()
	}
	return dependency.err
}
func (dependency *readinessBoundaryDependency) Ready(ctx context.Context) error {
	return dependency.Check(ctx)
}
func (dependency *readinessBoundaryDependency) CheckOutbox(ctx context.Context) error {
	return dependency.Check(ctx)
}

func TestOwnedReadinessKeepsEndpointWithoutCatalogButRejectsInfrastructureFailure(t *testing.T) {
	for _, failed := range []string{"none", "owner", "outbox", "publisher", "email", "cleanup_claim"} {
		t.Run(failed, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dependencies := map[string]*readinessBoundaryDependency{}
			for _, name := range []string{"owner", "outbox", "publisher", "email"} {
				dependencies[name] = &readinessBoundaryDependency{}
				if name == failed {
					dependencies[name].err = errors.New("owned infrastructure unavailable")
				}
			}
			// Последняя проверка завершает один настоящий цикл без таймеров/гонок.
			dependencies["email"].cancel = cancel
			cleanup := serviceruntime.NewReadiness()
			cleanup.Set(failed != "cleanup_claim", "claim")
			readiness := serviceruntime.NewReadiness()
			// Начинаем с противоположного состояния, чтобы доказать сам переход.
			readiness.Set(failed != "none", "previous")
			worker := monitorReadiness(dependencies["owner"], dependencies["outbox"], dependencies["publisher"], dependencies["email"], cleanup, readiness, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{ReadinessInterval: time.Hour, ReadinessTimeout: time.Second})
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
