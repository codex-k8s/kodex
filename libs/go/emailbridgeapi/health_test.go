package emailbridgeapi

import (
	"strings"
	"testing"
)

func TestHealthSummaryClosedAndLegacy(t *testing.T) {
	legacy := &ProtocolReadiness{Smtp: "ready", Imap: "not_configured", Pop3: "not_ready"}
	summary, err := HealthSummary("not_ready", legacy)
	if err != nil {
		t.Fatal(err)
	}
	h, err := ParseHealthSummary(summary)
	if err != nil || *h.Protocols.Pop3Reason != ProtocolReadinessReasonUnavailable {
		t.Fatal("legacy fallback")
	}
	reason := ProtocolReadinessReasonAuthRejected
	legacy.Pop3Reason = &reason
	summary, err = HealthSummary("not_ready", legacy)
	if err != nil {
		t.Fatal(err)
	}
	h, err = ParseHealthSummary(summary)
	if err != nil || !h.CredentialInvalid() {
		t.Fatal("auth category lost")
	}
	if got := LocalizeHealthSummary(summary, func(k string) string { return k }); got != "SMTP: EMAIL_HEALTH_READY; IMAP: EMAIL_HEALTH_NOT_CONFIGURED; POP3: EMAIL_HEALTH_AUTH_REJECTED" {
		t.Fatal(got)
	}
	for _, bad := range []string{summary + " ", strings.Replace(summary, "auth_rejected", "raw-fixture-secret", 1), strings.Replace(summary, `"status":`, `"host":"example.invalid","status":`, 1), strings.Replace(summary, `"status":"not_ready"`, `"status":"ready"`, 1), strings.Replace(summary, `"smtp_reason":"none"`, `"smtp_reason":"auth_rejected"`, 1)} {
		if _, err = ParseHealthSummary(bad); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
	for _, reason := range []ProtocolReadinessReason{ProtocolReadinessReasonCredentialUnavailable, ProtocolReadinessReasonTLSUnavailable, ProtocolReadinessReasonNetworkUnavailable, ProtocolReadinessReasonResponseInvalid, ProtocolReadinessReasonScanLimit, ProtocolReadinessReasonConfigurationInvalid} {
		legacy.Pop3Reason = &reason
		summary, err := HealthSummary("not_ready", legacy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ParseHealthSummary(summary); err != nil {
			t.Fatal(err)
		}
	}
}
