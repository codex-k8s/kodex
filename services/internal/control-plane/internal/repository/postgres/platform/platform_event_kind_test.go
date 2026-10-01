package platform

import "testing"

func TestPlatformEventKindsMatchContract(t *testing.T) {
	want := map[string]string{
		"PROJECT_CHANGED":                "PROJECT",
		"AGENT_CHANGED":                  "AGENT",
		"ARTIFACT_CHANGED":               "ARTIFACT",
		"INSTRUCTIONS_PUBLISHED":         "INSTRUCTIONS",
		"WORKFLOW_CHANGED":               "WORKFLOW",
		"SCHEDULE_CHANGED":               "SCHEDULE",
		"INTEGRATION_CONNECTION_CHANGED": "INTEGRATION_CONNECTION",
		"INTEGRATION_GRANT_CHANGED":      "INTEGRATION_GRANT",
		"MEMBERSHIP_CHANGED":             "MEMBERSHIP",
		"SYSTEM_ASSISTANT_CHANGED":       "SYSTEM_ASSISTANT",
		"ROLE_IMAGE_RECIPE_CHANGED":      "ROLE_IMAGE_RECIPE",
		"ROLE_IMAGE_PROMOTION_REQUESTED": "ROLE_IMAGE_RECIPE",
		"ROLE_IMAGE_PROMOTED":            "ROLE_IMAGE_RECIPE",
		"RUNTIME_ENVIRONMENT_CHANGED":    "RUNTIME_ENVIRONMENT",
		"PROVIDER_ACCOUNT_CHANGED":       "PROVIDER_ACCOUNT",
		"PLATFORM_MEMBERSHIP_CHANGED":    "PLATFORM_MEMBERSHIP",
		"RUNTIME_SECRET_CHANGED":         "RUNTIME_SECRET",
		"MANAGED_CONFIGURATION_CHANGED":  "MANAGED_CONFIGURATION",
		"RUN_CHANGED":                    "RUN",
	}
	for eventName, kind := range want {
		if got := platformEventKind(eventName); got != kind {
			t.Errorf("event %s projected as %s, want %s", eventName, got, kind)
		}
	}
}

func TestPlatformEventKindRejectsUnknownEvent(t *testing.T) {
	if got := platformEventKind("UNKNOWN_CHANGED"); got != "" {
		t.Fatalf("unknown platform event projected as %s", got)
	}
}
