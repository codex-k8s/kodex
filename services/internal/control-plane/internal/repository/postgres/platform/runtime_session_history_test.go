package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestRuntimeSessionHistoryPreservesWholeMessages(t *testing.T) {
	original := `{"operations":[{"instructions":"` + strings.Repeat("Полный JSON 😀 <>& ", 600) + `"}],"complete":true}`
	for _, content := range []string{original, strings.Repeat("😀", 32768), strings.Repeat("\x01", 32768)} {
		messages := []map[string]string{{"role": "USER", "content": content}, {"role": "ASSISTANT", "content": "Complete result"}}
		actual := boundedRuntimeSessionHistory(messages)
		if len(actual) != 2 || actual[0]["content"] != content || actual[1]["content"] != "Complete result" {
			t.Fatal("whole bounded message changed")
		}
		actual[0]["content"] = "changed"
		if messages[0]["content"] != content {
			t.Fatal("history aliases the source snapshot")
		}
	}
}

func TestRuntimeSessionHistoryBudgetDropsWholeOldestPrefix(t *testing.T) {
	// HTML escaping делает каждое такое сообщение около 192KiB в JSON.
	content := strings.Repeat("&", runtimecontract.MaximumAssistantTurnCodepoints)
	messages := []map[string]string{{"role": "ASSISTANT", "content": "oldest"}}
	for range 4 {
		messages = append(messages, map[string]string{"role": "USER", "content": content})
	}
	actual := boundedRuntimeSessionHistory(messages)
	raw, err := json.Marshal(actual)
	if err != nil || len(raw) > maximumRuntimeSessionHistoryBytes || len(actual) != 2 {
		t.Fatal("history did not account for encoded byte budget")
	}
	for _, message := range actual {
		if message["content"] != content {
			t.Fatal("budget retained a partial message")
		}
	}
	candidate := append([]map[string]string{messages[2]}, actual...)
	raw, _ = json.Marshal(candidate)
	if len(raw) <= maximumRuntimeSessionHistoryBytes {
		t.Fatal("fixture does not cross exact history budget")
	}
	for _, invalid := range []map[string]string{
		{"role": "USER", "content": strings.Repeat("я", 32769)},
		{"role": "USER", "content": "bad\x00"},
		{"role": "USER", "content": "bad\xff"},
		{"role": "OWNER", "content": "authority"},
		{"role": "ASSISTANT", "content": strings.Repeat("a", 64<<10+1)},
	} {
		input := []map[string]string{{"role": "USER", "content": "oldest"}, invalid, {"role": "ASSISTANT", "content": "newest"}}
		actual := boundedRuntimeSessionHistory(input)
		if len(actual) != 1 || actual[0]["content"] != "newest" {
			t.Fatal("history skipped a rejected message and rejoined older context")
		}
	}
	if len(boundedRuntimeSessionHistory(nil)) != 0 {
		t.Fatal("empty history synthesized a message")
	}
	long := make([]map[string]string, 21)
	for index := range long {
		long[index] = map[string]string{"role": "USER", "content": "recent"}
	}
	long[0]["content"] = "oldest"
	actual = boundedRuntimeSessionHistory(long)
	if len(actual) != 20 || actual[0]["content"] == "oldest" {
		t.Fatal("history lost newest twenty-message boundary")
	}
}
