package observability

import "testing"

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
