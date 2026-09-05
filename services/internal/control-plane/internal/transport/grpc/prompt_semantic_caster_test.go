package grpc

import "testing"

func TestPromptSemanticCasterRejectsUnknownAndMissingPlatformIdentity(t *testing.T) {
	for _, value := range []string{"", "UNSPECIFIED", "future", "PURPOSE_SUFFIX"} {
		if _, ok := castPromptSlot(value, false); ok {
			t.Fatalf("invalid slot accepted: %q", value)
		}
	}
	if _, ok := castPromptSlot("", true); !ok {
		t.Fatal("user section requires no platform slot")
	}
	for _, value := range []string{"PURPOSE", "WORKFLOW", "RUNTIME_CHANGES"} {
		if _, ok := castPromptSlot(value, false); !ok {
			t.Fatalf("known slot lost: %q", value)
		}
	}
	for _, value := range []string{"", "UNSPECIFIED", "future"} {
		if _, ok := castPromptSource(value); ok {
			t.Fatalf("invalid source accepted: %q", value)
		}
	}
	for _, value := range []string{"PLATFORM", "USER_TEMPLATE"} {
		if _, ok := castPromptSource(value); !ok {
			t.Fatalf("known source lost: %q", value)
		}
	}
}
