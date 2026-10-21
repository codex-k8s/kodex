package runtimecontract

import (
	"strings"
	"testing"
)

// TestAssistantTurnContentCodepointBounds accepts bounded multibyte content and rejects overflow, invalid UTF-8, NUL and blank input.
// TestAssistantTurnContentCodepointBounds принимает ограниченный многобайтный текст и отклоняет превышение лимита, некорректный UTF-8, NUL и пустой ввод.
func TestAssistantTurnContentCodepointBounds(t *testing.T) {
	for _, test := range []struct {
		name, content string
		valid         bool
	}{
		{"russian-live-size", strings.Repeat("я", 24995), true},
		{"russian-boundary", strings.Repeat("я", 32768), true},
		{"astral-boundary", strings.Repeat("😀", 32768), true},
		{"replacement-is-valid", "я\ufffd", true},
		{"russian-overflow", strings.Repeat("я", 32769), false},
		{"ascii-overflow", strings.Repeat("a", 32769), false},
		{"invalid-utf8", "bad\xff", false},
		{"nul", "bad\x00", false},
		{"empty", "", false},
		{"blank", " \n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if ValidAssistantTurnContent(test.content) != test.valid {
				t.Fatal("unexpected input validation result")
			}
		})
	}
}

// TestRunEventEnvelopeFitsStreamWithHeaderReserve checks that escaped maximum user text plus metadata fits the event ceiling while preserving broker header space.
// TestRunEventEnvelopeFitsStreamWithHeaderReserve проверяет, что максимальный экранированный текст и метаданные помещаются в событие с сохранением места для брокерных заголовков.
func TestRunEventEnvelopeFitsStreamWithHeaderReserve(t *testing.T) {
	if MaximumAssistantTurnCodepoints*6+(48<<10) > MaximumControlPlaneRunEventPayloadBytes ||
		MaximumControlPlaneRunEventPayloadBytes+(4<<10) != MaximumControlPlaneStreamMessageBytes {
		t.Fatal("bounded USER JSON and metadata exceed broker envelope")
	}
}

// TestSessionContextPreservesBoundedUnicodeUSER applies codepoint bounds to USER history and distinct byte bounds to known assistant and system roles.
// TestSessionContextPreservesBoundedUnicodeUSER применяет лимит кодовых точек к истории USER и отдельные байтовые лимиты к известным ролям помощника и системы.
func TestSessionContextPreservesBoundedUnicodeUSER(t *testing.T) {
	for _, test := range []struct {
		role, content string
		valid         bool
	}{
		{"USER", strings.Repeat("😀", 32768), true},
		{"USER", strings.Repeat("я", 32768), true},
		{"USER", strings.Repeat("я", 32769), false},
		{"USER", "bad\xff", false},
		{"USER", "bad\x00", false},
		{"ASSISTANT", strings.Repeat("a", 64<<10), true},
		{"ASSISTANT", strings.Repeat("a", (64<<10)+1), false},
		{"SYSTEM", strings.Repeat("a", (64<<10)+1), false},
		{"UNKNOWN", "text", false},
	} {
		if validSessionContext(RunnerInput{SessionContext: []RunnerSessionMessage{{Role: test.role, Content: test.content}}}) != test.valid {
			t.Fatal("unexpected session context boundary")
		}
	}
}

// TestRunnerRoundtripPreservesMaximumUnicodeUSERHistory round-trips maximum emoji history exactly without classifying raw user text as a platform continuation notice.
// TestRunnerRoundtripPreservesMaximumUnicodeUSERHistory точно сохраняет предельную историю с эмодзи при кодировании, не принимая пользовательский текст за системное уведомление продолжения.
func TestRunnerRoundtripPreservesMaximumUnicodeUSERHistory(t *testing.T) {
	input := validRunnerInputFixture()
	content := strings.Repeat("😀", MaximumAssistantTurnCodepoints)
	input.SessionContext = []RunnerSessionMessage{{Role: "USER", Content: content}}
	refreshRunnerInputBindings(&input)
	raw, err := EncodeRunnerInput(input)
	if err != nil {
		t.Fatal("maximum USER history rejected by runner encoder")
	}
	decoded, err := DecodeRunnerInput(raw)
	if err != nil || len(decoded.SessionContext) != 1 || decoded.SessionContext[0].Content != content {
		t.Fatal("runner lost exact USER history")
	}
	if notice, err := CurrentContinuationNotice(decoded); err != nil || notice {
		t.Fatal("raw USER task was treated as platform continuation notice")
	}
}
