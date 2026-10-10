package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResultPublicationGuidanceKeepsArchivesOutsideOutbox(t *testing.T) {
	for _, variant := range []struct{ locale, required string }{
		{"ru", "GZIP, TAR и ZIP"},
		{"en", "GZIP, TAR and ZIP"},
	} {
		t.Run(variant.locale, func(t *testing.T) {
			snapshot := semanticFixture()
			snapshot.Locale = variant.locale
			result, err := Materialize(`Agent {{slot "FILES"}}`, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var envelope semanticEnvelope
			if json.Unmarshal([]byte(result.Prompt), &envelope) != nil {
				t.Fatal("invalid semantic envelope")
			}
			for _, section := range envelope.Sections {
				if section.Slot != SlotFiles {
					continue
				}
				var files struct {
					Result struct {
						Directory, Instruction string
						InstructionRevision    string `json:"instruction_revision"`
					}
				}
				if json.Unmarshal([]byte(section.Content), &files) != nil || files.Result.Directory != "/workspace/.kodex/outbox" ||
					files.Result.InstructionRevision != "runtime-result-guidance-v1" || !strings.Contains(files.Result.Instruction, variant.required) ||
					!strings.Contains(files.Result.Instruction, "UTF-8") {
					t.Fatal("result publication format guidance is missing")
				}
				return
			}
			t.Fatal("FILES slot is missing")
		})
	}
}

func TestResultPublicationGuidancePreservesPinnedFilesSlot(t *testing.T) {
	const historical = `{"result":{"directory":"/workspace/.kodex/outbox","instruction":"synthetic historical instruction"}}`
	snapshot := semanticFixture()
	snapshot.SemanticValues = map[SemanticSlot]string{SlotFiles: historical}
	result, err := Materialize(`Agent {{slot "FILES"}}`, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var envelope semanticEnvelope
	if json.Unmarshal([]byte(result.Prompt), &envelope) != nil {
		t.Fatal("invalid semantic envelope")
	}
	for _, section := range envelope.Sections {
		if section.Slot == SlotFiles {
			if section.Content != historical {
				t.Fatal("pinned historical guidance was replaced")
			}
			return
		}
	}
	t.Fatal("FILES slot is missing")
}
