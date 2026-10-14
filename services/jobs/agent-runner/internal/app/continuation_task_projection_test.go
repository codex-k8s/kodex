package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Тот же synthetic immutable input сверяется CP с полным producer output.
// Изменение test-only; runner production и shared predicate остаются прежними.
func TestContinuationTaskProjectionProducerRunnerOracle(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "control-plane", "internal", "repository", "postgres", "platform", "testdata", "continuation_task_projection_v1.json"))
	var fixture runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("shared synthetic producer fixture unavailable")
	}
	for _, resumed := range []bool{false, true} {
		input := fixture
		if resumed {
			input.CodexSessionID = "exact-previous-provider-thread"
		}
		prompt, err := buildPrompt(input)
		if err != nil {
			t.Fatal(err)
		}
		messages := promptSessionMessages(t, prompt)
		want := fixture.SessionContext
		if resumed {
			want = want[len(want)-1:]
		}
		if !reflect.DeepEqual(messages, want) || !strings.HasSuffix(string(prompt), fixture.Task) || strings.Count(string(prompt), fixture.Task) != 1 ||
			strings.Contains(string(prompt), "<runtime-revision-delta>") {
			t.Fatal("cold/resume dropped or duplicated full task/history/notice")
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
