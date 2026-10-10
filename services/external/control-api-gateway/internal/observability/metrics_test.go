package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSessionStreamRouteUsesCanonicalLabel(t *testing.T) {
	t.Parallel()
	for path, expected := range map[string]string{
		"/api/v1/session/stream":             "realtime",
		"/api/v1/realtime":                   "unknown",
		"/api/v1/session/stream/":            "unknown",
		"/api/v1/session/stream/resource-id": "unknown",
		"/api/v1/session/stream-other":       "unknown",
	} {
		if actual := Route(path); actual != expected {
			t.Errorf("Route(%q) = %q, expected %q", path, actual, expected)
		}
	}
}

func TestSessionStreamHTTPMetricsHaveClosedRouteLabels(t *testing.T) {
	t.Parallel()
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
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/session/stream",
		"/api/v1/realtime",
		"/api/v1/session/stream/",
		"/api/v1/session/stream/resource-id",
		"/api/v1/session/stream-other",
	} {
		metrics.ObserveHTTP(Route(path), 101, time.Now())
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 2 {
		t.Fatalf("metric families = %d, expected 2", len(families))
	}
	for _, family := range families {
		if len(family.Metric) != 2 {
			t.Fatalf("%s series = %d, expected 2", family.GetName(), len(family.Metric))
		}
		seen := map[string]bool{}
		for _, metric := range family.Metric {
			route := ""
			for _, label := range metric.Label {
				switch label.GetName() {
				case "route":
					route = label.GetValue()
				case "status":
					if label.GetValue() != "101" {
						t.Fatalf("unexpected status label %q", label.GetValue())
					}
				default:
					t.Fatalf("unexpected label %q", label.GetName())
				}
			}
			if route != "realtime" && route != "unknown" || seen[route] {
				t.Fatalf("unexpected or duplicate route label %q", route)
			}
			seen[route] = true
			if metric.Counter != nil {
				expected := float64(1)
				if route == "unknown" {
					expected = 4
				}
				if metric.Counter.GetValue() != expected {
					t.Fatalf("%s counter = %v, expected %v", route, metric.Counter.GetValue(), expected)
				}
			}
		}
	}
}

func TestOwnerRoutesHaveClosedLabels(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"/api/v1/mattermost/teams":                      "workspaces",
		"/api/v1/role-definitions/commands":             "role_definitions",
		"/api/v1/agent-assignments/id/history":          "agents",
		"/api/v1/instruction-sets/id/compare":           "instructions",
		"/api/v1/provider-authorizations/id/new-code":   "providers",
		"/api/v1/integration-approvals/id/decision":     "integrations",
		"/api/v1/schedules/id/configuration":            "schedules",
		"/api/v1/runs/id/timeline":                      "runs",
		"/api/v1/incidents/id/commands":                 "incidents",
		"/api/v1/workspace-restores/id":                 "backups",
		"/api/v1/configuration-source/id":               "configuration_changes",
		"/api/v1/unregistered/sensitive/resource/value": "unknown",
	}
	for path, expected := range tests {
		if actual := Route(path); actual != expected {
			t.Errorf("Route(%q) = %q, expected %q", path, actual, expected)
		}
	}
}

func TestOwnerMetricsClosedValues(t *testing.T) {
	t.Parallel()
	for _, channel := range []string{"WORKSPACE_TEAMS", "PROVIDERS", "INTEGRATIONS", "APPROVALS", "BACKUPS", "HEALTH"} {
		if actual := normalizeChannel(channel); actual != channel {
			t.Errorf("normalizeChannel(%q) = %q", channel, actual)
		}
	}
	if actual := normalizeChannel("secret-channel"); actual != "UNKNOWN" {
		t.Fatalf("unknown channel label = %q", actual)
	}
	if actual := normalizeStatus(202); actual != "202" {
		t.Fatalf("accepted status label = %q", actual)
	}
}
