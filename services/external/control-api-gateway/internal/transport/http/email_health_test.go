package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/usertext"
	"strings"
	"testing"
)

func TestEmailHealthPublicReadback(t *testing.T) {
	texts, err := usertext.New()
	if err != nil {
		t.Fatal(err)
	}
	reason := api.ProtocolReadinessReasonAuthRejected
	summary, err := api.HealthSummary("not_ready", &api.ProtocolReadiness{Smtp: "ready", Imap: "not_configured", Pop3: "not_ready", Pop3Reason: &reason})
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"ru", "en"} {
		value, err := ProtoMap(&cp.IntegrationConnection{LastTestOutcome: summary})
		if err != nil {
			t.Fatal(err)
		}
		LocalizeSafeErrors(value, func(k string) string { return texts.Localize(locale, k, nil) })
		raw, _ := json.Marshal(value)
		body := string(raw)
		for _, token := range []string{"SMTP:", "POP3:", "IMAP:"} {
			if !strings.Contains(body, token) {
				t.Fatal("protocol omitted")
			}
		}
		if strings.Contains(body, "email-health:") || strings.Contains(body, "EMAIL_HEALTH_") {
			t.Fatal("localization missing")
		}
	}
	value := map[string]any{"lastTestOutcome": api.HealthSummaryPrefix + `{"host":"fixture-private-text"}`}
	LocalizeSafeErrors(value, func(k string) string { return texts.Localize("en", k, nil) })
	if strings.Contains(value["lastTestOutcome"].(string), "fixture-private-text") {
		t.Fatal("raw report leaked")
	}
	legacy := map[string]any{"lastTestOutcome": "i18n:INTEGRATION_UNAVAILABLE"}
	LocalizeSafeErrors(legacy, func(k string) string { return texts.Localize("en", k, nil) })
	if strings.Contains(legacy["lastTestOutcome"].(string), "i18n:") {
		t.Fatal("legacy reader lost")
	}
}
