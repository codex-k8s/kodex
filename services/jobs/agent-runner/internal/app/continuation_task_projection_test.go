package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestContinuationTaskProjectionProducerRunnerOracle reads the shared synthetic producer fixture and checks cold/resumed prompt history, exact task cardinality and platform notice projection without invoking a provider or granting authority.
// TestContinuationTaskProjectionProducerRunnerOracle читает общую синтетическую producer fixture и проверяет cold/resumed prompt history, точное число task-вхождений и platform notice projection без провайдера и выдачи полномочий.
func TestContinuationTaskProjectionProducerRunnerOracle(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "control-plane", "internal", "repository", "postgres", "platform", "testdata", "continuation_task_projection_v1.json"))
	var fixture runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("shared synthetic producer fixture unavailable")
	}
	for _, resumed := range []bool{false, true} {
		input := fixture
		if resumed {
			input.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_previous", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_previous.json", SHA256: strings.Repeat("a", 64), SizeBytes: 800}
		}
		prompt, err := buildPrompt(input)
		if err != nil {
			t.Fatal(err)
		}
		messages := promptSessionMessages(t, prompt)
		want := fixture.SessionContext[len(fixture.SessionContext)-1].Content
		if len(messages) != 1 || messages[0].Content != want || string(prompt) != want+"\n"+fixture.Task || strings.Count(string(prompt), fixture.Task) != 1 {
			t.Fatal("native input dropped current task/notice or replayed prior history")
		}
		var envelope runtimecontract.PromptServiceEnvelope
		if json.Unmarshal([]byte(messages[len(messages)-1].Content), &envelope) != nil {
			t.Fatal("notice lost envelope")
		}
		found := false
		for _, section := range envelope.Sections {
			if section.Slot == "PURPOSE" {
				found = strings.Contains(section.Content, "continuation-task-projection-v1") && !strings.Contains(section.Content, fixture.Task)
			}
		}
		if !found {
			t.Fatal("task projection missing from actual delivered notice")
		}
	}
}
