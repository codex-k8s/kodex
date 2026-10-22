package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// TestTelemetryShutdownOperationsPreserveProfile checks descriptor order, resource identity, caller-slice isolation and exact positive/zero/negative timeout forwarding. Construction must not execute a callback; cached once-only telemetry errors prove the returned bound callbacks still target the same wrapper without creating an SDK runtime, network client or background worker.
// TestTelemetryShutdownOperationsPreserveProfile проверяет порядок описаний, идентичность ресурса, изоляцию среза вызывающего и точную передачу положительного, нулевого и отрицательного срока. Создание не должно вызывать обработчики; сохранённые однократные ошибки телеметрии подтверждают привязку возвращённых методов к той же обёртке без создания SDK-runtime, сетевого клиента или фонового исполнителя.
func TestTelemetryShutdownOperationsPreserveProfile(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second, 3 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			telemetry := &appTelemetry{}
			resourceError := errors.New("resource cleanup fixture")
			resourceCalls := 0
			storage := make([]serviceruntime.ShutdownOperation, 3)
			storage[0] = serviceruntime.ShutdownOperation{
				Name: "owned-resource", Timeout: time.Second,
				Run: func(context.Context) error {
					resourceCalls++
					return resourceError
				},
			}
			storage[1].Name, storage[2].Name = "spare-one", "spare-two"
			operations := telemetry.shutdownOperations(timeout, storage[:1]...)
			if len(operations) != 3 || resourceCalls != 0 ||
				storage[1].Name != "spare-one" || storage[2].Name != "spare-two" {
				t.Fatal("shutdown construction executed or aliased caller storage")
			}
			if operations[0].Name != "owned-resource" || operations[0].Timeout != time.Second ||
				operations[1].Name != "otel-tracing" || operations[1].Timeout != timeout ||
				operations[2].Name != "sentry-flush" || operations[2].Timeout != timeout {
				t.Fatal("shutdown order, diagnostic names or timeout changed")
			}
			traceError := errors.New("trace cleanup fixture")
			sentryError := errors.New("sentry cleanup fixture")
			telemetry.traceOnce.Do(func() { telemetry.traceErr = traceError })
			telemetry.sentryOnce.Do(func() { telemetry.sentryErr = sentryError })
			for i, want := range []error{resourceError, traceError, sentryError} {
				if !errors.Is(operations[i].Run(t.Context()), want) {
					t.Fatal("shutdown callback target or cached error changed")
				}
			}
			if resourceCalls != 1 {
				t.Fatal("resource cleanup callback count changed")
			}
			operations[0].Name = "modified-result"
			if storage[0].Name != "owned-resource" {
				t.Fatal("shutdown result shares caller backing storage")
			}
		})
	}
}
