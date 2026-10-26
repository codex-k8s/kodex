package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCaptureProgressPromptReachability ties the actual prompt example to the strict decoder and
// verifies that an external receipt key cannot become a prepared-file selector or acceptance proof.
// TestCaptureProgressPromptReachability связывает настоящий пример подсказки со строгим decoder и
// проверяет, что ключ внешней receipt не становится файловым селектором либо доказательством приёмки.
func TestCaptureProgressPromptReachability(t *testing.T) {
	requirements := captureProgressFixture()
	prompt, err := CaptureProgressPrompt(ServerDAGV1, requirements)
	if err != nil {
		t.Fatal("progress prompt unavailable")
	}
	lines := strings.Split(prompt, "\n")
	if len(lines) != 5 {
		t.Fatal("progress prompt data boundaries changed")
	}
	var selectors []map[string]string
	if json.Unmarshal([]byte(lines[1]), &selectors) != nil || len(selectors) != 3 {
		t.Fatal("progress prompt selectors invalid")
	}
	for index, selector := range selectors {
		if selector["key"] != requirements[index].Key || selector["fileName"] != requirements[index].FileName || selector["mediaType"] != requirements[index].MediaType || len(selector) != 3 {
			t.Fatal("progress prompt changed published selectors")
		}
	}
	declaration, err := DecodeCaptureProgress(lines[3], ServerDAGV1, requirements)
	if err != nil || strings.Join(declaration.ArtifactKeys, ",") != "bundle,manifest,handoff" {
		t.Fatal("progress prompt example cannot reach strict decoder")
	}
	for _, required := range []string{"не финальный бизнес-результат", "бюджет", "тем же business attempt", "прежний receipt", "Human Gate", "не запускает зависимые шаги"} {
		if !strings.Contains(prompt, required) {
			t.Fatal("progress prompt lost lifecycle boundary")
		}
	}
	if text, err := CaptureProgressPrompt("MANAGER_DRIVEN", requirements); err == nil || text != "" {
		t.Fatal("legacy mode received capture-progress instruction")
	}
	if text, err := CaptureProgressPrompt(ServerDAGV1, requirements[3:]); err == nil || text != "" {
		t.Fatal("non-artifact step received capture selectors")
	}
}
