package app

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/observability"
)

func TestTelemetryDisabledUsesCanonicalRuntime(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "local")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	for _, key := range []string{"SENTRY_DSN_FILE", "SENTRY_EXPECTED_HOST", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", "OTEL_EXPORTER_OTLP_CA_FILE"} {
		t.Setenv(key, "")
	}
	// Прежний constructor терял disabled и всегда требовал отсутствующий Sentry.
	_, err := observability.NewRuntime(t.Context(), observability.RuntimeConfig{ServiceName: "email-bridge", ServiceVersion: "test", Environment: "local", OTLPEndpoint: "otel-collector.observability.svc:4317", OTLPTLSServerName: "otel-collector.observability.svc.cluster.local", OTLPCAFile: "/missing", TraceSampleRatio: 0.1})
	if err == nil {
		t.Fatal("legacy incomplete configuration unexpectedly accepted")
	}
	runtime, err := newTelemetry(t.Context(), "test")
	if err != nil {
		t.Fatal("disabled canonical runtime failed")
	}
	metrics := observability.NewMetrics("email_bridge", "test", nil)
	response := httptest.NewRecorder()
	metrics.PrometheusHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatal("disabled exporters disabled Prometheus")
	}
	if err := stopTelemetry(t.Context(), runtime); err != nil {
		t.Fatal("disabled telemetry cleanup failed")
	}
}

func TestTelemetryEnabledRequiresCompletePinnedSettings(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "test")
	t.Setenv("OTEL_SDK_DISABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector.observability.svc:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", "otel-collector.observability.svc.cluster.local")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.1")
	t.Setenv("SENTRY_EXPECTED_HOST", "sentry-relay.observability.svc:8443")
	server := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := server.Certificate().Raw
	server.Close()
	directory := t.TempDir()
	ca, dsn := filepath.Join(directory, "ca.pem"), filepath.Join(directory, "sentry-dsn")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dsn, []byte("https://fixture-key@sentry-relay.observability.svc:8443/42"), 0400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CA_FILE", ca)
	t.Setenv("SENTRY_DSN_FILE", dsn)
	runtime, err := newTelemetry(t.Context(), "test")
	if err != nil {
		t.Fatal("complete enabled runtime rejected")
	}
	if err := stopTelemetry(t.Context(), runtime); err != nil {
		t.Fatal("enabled cleanup failed")
	}
	for _, tc := range []struct{ key, value string }{
		{"SENTRY_DSN_FILE", ""}, {"SENTRY_EXPECTED_HOST", "foreign.invalid"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:4317"}, {"OTEL_EXPORTER_OTLP_CA_FILE", "/missing"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := newTelemetry(t.Context(), "test"); err == nil {
				t.Fatal("incomplete or unpinned enabled configuration accepted")
			}
		})
	}
}

type shutdownFixture struct {
	t       *testing.T
	tracing context.Context
	flushed bool
}

func (f *shutdownFixture) ShutdownTracing(ctx context.Context) error {
	f.tracing = ctx
	if ctx.Err() != nil {
		f.t.Fatal("cleanup inherited lifecycle cancellation")
	}
	if _, ok := ctx.Deadline(); !ok {
		f.t.Fatal("unbounded tracing cleanup")
	}
	return context.DeadlineExceeded
}
func (f *shutdownFixture) FlushSentry(ctx context.Context) error {
	if ctx.Err() != nil || ctx == f.tracing {
		f.t.Fatal("Sentry inherited failed tracing budget")
	}
	if _, ok := ctx.Deadline(); !ok {
		f.t.Fatal("unbounded Sentry cleanup")
	}
	f.flushed = true
	return nil
}
func TestTelemetryShutdownUsesIndependentBudgets(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture := &shutdownFixture{t: t}
	if err := stopTelemetry(ctx, fixture); !errors.Is(err, context.DeadlineExceeded) || !fixture.flushed {
		t.Fatal("cleanup lost failure or skipped Sentry")
	}
}
