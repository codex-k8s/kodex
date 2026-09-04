package platform

import "testing"

func TestSystemSTTModelMatchesExecutableProfile(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-5.5", "unknown", ""} {
		if systemSTTModelSupported(model, "ru") {
			t.Fatalf("unsupported transcription model accepted: %q", model)
		}
	}
	for _, model := range []string{"gpt-transcribe", "gpt-4o-transcribe", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-12-15", "whisper-1"} {
		if !systemSTTModelSupported(model, "") || !systemSTTModelSupported(model, "en") {
			t.Fatalf("registered model rejected: %s", model)
		}
	}
	if !systemSTTModelSupported("gpt-transcribe", "ru") || systemSTTModelSupported("gpt-transcribe", "unknown") {
		t.Fatal("transcription profile mismatch")
	}
}
