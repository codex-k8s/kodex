package observability

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// connectionLabels identifies the closed metric dimensions expected by tests.
// connectionLabels задаёт закрытые измерения метрики, ожидаемые тестами.
type connectionLabels struct {
	outcome string
	stage   string
	reason  string
}

// TestConnectionProxyLabels checks exact counts for proxy and tunnel outcomes.
// TestConnectionProxyLabels проверяет точные счётчики исходов прокси и туннеля.
func TestConnectionProxyLabels(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	// Матрица соответствует закрытым исходам HTTP proxy и WebSocket tunnel.
	events := []connectionLabels{
		{"rejected", "proxy", "malformed"},
		{"failed", "proxy", "certificate"},
		{"rejected", "proxy", "tls"},
		{"rejected", "proxy", "sni"},
		{"rejected", "proxy", "not_ready"},
		{"rejected", "proxy", "request"},
		{"rejected", "proxy", "policy"},
		{"failed", "proxy", "upstream"},
		{"rejected", "proxy", "policy"},
		{"failed", "proxy", "io"},
		{"completed", "proxy", "none"},
		{"completed", "proxy", "none"},
		{"rejected", "connect", "authority"},
	}
	want := make(map[connectionLabels]float64)
	for _, event := range events {
		metrics.Connection(event.outcome, event.stage, event.reason)
		want[event]++
	}
	if got := gatherConnectionCounters(t, registry); !reflect.DeepEqual(got, want) {
		t.Fatalf("connection counters = %v, want %v", got, want)
	}
}

// TestConnectionUnknownLabelsRemainBounded collapses untrusted labels to finite fallback values.
// TestConnectionUnknownLabelsRemainBounded сводит недоверенные метки к конечным запасным значениям.
func TestConnectionUnknownLabelsRemainBounded(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	const unknownEvents = 128
	for index := range unknownEvents {
		suffix := strconv.Itoa(index)
		metrics.Connection("outcome-"+suffix, "/path/"+suffix, "host-"+suffix+".example.invalid")
	}
	for _, value := range []string{"", "PROXY", " proxy", "proxy ", "proxy/path"} {
		metrics.Connection("failed", value, "io")
	}
	for _, value := range []string{"", "TLS", " tls", "tls ", "tls: detail", "request/path", "upstream.example.invalid"} {
		metrics.Connection("failed", "proxy", value)
	}
	metrics.Connection("FAILED", "proxy", "io")
	want := map[connectionLabels]float64{
		{"unknown", "unknown", "unknown"}: unknownEvents,
		{"failed", "unknown", "io"}:       5,
		{"failed", "proxy", "unknown"}:    7,
		{"unknown", "proxy", "io"}:        1,
	}
	if got := gatherConnectionCounters(t, registry); !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded connection counters = %v, want %v", got, want)
	}
}

// newTestMetrics registers collectors in an isolated registry for each test.
// newTestMetrics регистрирует коллекторы в отдельном реестре для каждого теста.
func newTestMetrics(t *testing.T) (*Metrics, *prometheus.Registry) {
	t.Helper()
	registry := prometheus.NewRegistry()
	metrics, err := New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if err := registry.Register(collector); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	return metrics, registry
}

// gatherConnectionCounters reads exact label tuples from the isolated registry.
// gatherConnectionCounters читает точные наборы меток из отдельного реестра.
func gatherConnectionCounters(t *testing.T, registry *prometheus.Registry) map[connectionLabels]float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	result := make(map[connectionLabels]float64)
	for _, family := range families {
		if family.GetName() != "kodex_egress_gateway_connection_attempts_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			if len(metric.GetLabel()) != 3 || metric.GetCounter() == nil {
				t.Fatalf("unexpected connection metric shape: %v", metric)
			}
			var labels connectionLabels
			for _, label := range metric.GetLabel() {
				switch label.GetName() {
				case "outcome":
					labels.outcome = label.GetValue()
				case "stage":
					labels.stage = label.GetValue()
				case "reason":
					labels.reason = label.GetValue()
				default:
					t.Fatalf("unexpected connection label: %q", label.GetName())
				}
			}
			if _, exists := result[labels]; exists {
				t.Fatalf("duplicate connection series: %v", labels)
			}
			result[labels] = metric.GetCounter().GetValue()
		}
	}
	return result
}
