package integration

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEmailHealthDiagnosticReader(t *testing.T) {
	for _, tc := range []struct{ name, body, code, summary string }{
		{"old-ready", `{"status":"ready"}`, "", "i18n:INTEGRATION_TEST_SUCCEEDED"},
		{"old-unavailable", `{"status":"not_ready"}`, "INTEGRATION_UNAVAILABLE", ""},
		{"old-report", `{"status":"not_ready","protocol_readiness":{"smtp":"ready","imap":"not_configured","pop3":"not_ready"}}`, api.HealthNotReadyCode, "unavailable"},
		{"new-report", `{"status":"not_ready","protocol_readiness":{"smtp":"ready","imap":"not_configured","pop3":"not_ready","pop3_reason":"auth_rejected"}}`, api.HealthNotReadyCode, "auth_rejected"},
		{"unknown-reason", `{"status":"not_ready","protocol_readiness":{"smtp":"ready","imap":"not_configured","pop3":"not_ready","pop3_reason":"fixture-private-text"}}`, "INTEGRATION_RESPONSE_INVALID", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := testAdapter(t)
			calls := 0
			emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, tc.body) })
			request := invocationRequest(t, adapter.definitions["email"], "email.delivery.health.read", map[string]any{}, nil)
			summary, err := adapter.Test(t.Context(), request)
			_, code := Outcome(err)
			if code != tc.code || calls != 1 {
				t.Fatalf("unexpected outcome %s calls%d", code, calls)
			}
			if tc.summary != "" && !strings.Contains(summary, tc.summary) {
				t.Fatal("summary lost")
			}
			if strings.Contains(summary, "fixture-private-text") {
				t.Fatal("private detail leaked")
			}
			if code == api.HealthNotReadyCode {
				if _, err := api.ParseHealthSummary(summary); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
