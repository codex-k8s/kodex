package grpc

import "testing"

func TestTemplateAvailabilityClosedReasons(t *testing.T) {
	for _, reason := range []string{"AVAILABLE", "PROJECT_CONTEXT_REQUIRED", "AGENT_CONTEXT_REQUIRED", "RUNTIME_CONTEXT_REQUIRED", "NOT_MATERIALIZED", "PERMISSION_REQUIRED", "CAPABILITY_REQUIRED", "REVISION_NOT_SAVED"} {
		got, err := castTemplateAvailabilityReason(reason, reason == "AVAILABLE")
		if err != nil || got == 0 {
			t.Fatalf("known reason %s rejected: %v", reason, err)
		}
		if _, err = castTemplateAvailabilityReason(reason, reason != "AVAILABLE"); err == nil {
			t.Fatalf("inconsistent reason %s accepted", reason)
		}
	}
	for _, reason := range []string{"", "UNSPECIFIED", "UNKNOWN"} {
		if _, err := castTemplateAvailabilityReason(reason, false); err == nil {
			t.Fatalf("unknown reason %s accepted", reason)
		}
	}
}
