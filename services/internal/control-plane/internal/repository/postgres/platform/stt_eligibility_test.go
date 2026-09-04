package platform

import "testing"

func TestSystemSTTModelMatchesExecutableProfile(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-5.5", "unknown", "", "whisper-1"} {
		if systemSTTModelSupported(model, "ru") {
			t.Fatalf("unsupported transcription model accepted: %q", model)
		}
	}
	if !systemSTTModelSupported("gpt-transcribe", "ru") || systemSTTModelSupported("gpt-transcribe", "unknown") {
		t.Fatal("transcription profile mismatch")
	}
}
