package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestPromptServiceExplicitImmutableRevisions(t *testing.T) {
	for _, revision := range []string{"prompt-service-v2", "prompt-service-v3"} {
		t.Run(revision, func(t *testing.T) {
			input := revisionPromptFixture(t, revision)
			if _, err := DecodePromptService(input); err != nil {
				t.Fatalf("explicit pinned revision rejected: %v", err)
			}
			for _, invalid := range []string{"", "prompt-service-v1", "prompt-service-v4", "prompt-service-v3 ", "PROMPT-SERVICE-V3"} {
				candidate := input
				candidate.PromptServiceTemplateRevision = invalid
				if _, err := DecodePromptService(candidate); err == nil {
					t.Fatalf("unknown revision accepted: %q", invalid)
				}
			}
			candidate := input
			candidate.PromptServiceTemplateRevision = "prompt-service-v2"
			if revision == "prompt-service-v2" {
				candidate.PromptServiceTemplateRevision = "prompt-service-v3"
			}
			if _, err := DecodePromptService(candidate); err == nil {
				t.Fatal("revision relabel accepted old material and digest")
			}
			candidate = input
			candidate.Capabilities = []string{"admin"}
			if _, err := DecodePromptService(candidate); err == nil {
				t.Fatal("version support expanded effective authority")
			}
		})
	}
}

func revisionPromptFixture(t *testing.T, revision string) RunnerInput {
	t.Helper()
	slots := []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	envelope := PromptServiceEnvelope{Revision: revision, Locale: "en", Sections: []PromptServiceSection{}}
	for _, slot := range slots {
		content := ""
		if slot == "EFFECTIVE_CAPABILITIES" {
			content = "read"
		}
		envelope.Sections = append(envelope.Sections, PromptServiceSection{Source: "PLATFORM", Slot: slot, Content: content})
	}
	material, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{revision, "en", "AGENT", slots})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return RunnerInput{Instructions: string(material), Capabilities: []string{"read"}, PromptTargetKind: "AGENT", PromptServiceTemplateRevision: revision, PromptServiceTemplateDigest: hex.EncodeToString(digest[:])}
}
