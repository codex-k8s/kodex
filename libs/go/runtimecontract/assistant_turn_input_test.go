package runtimecontract

import (
	"strings"
	"testing"
)

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

func TestRunEventEnvelopeFitsStreamWithHeaderReserve(t *testing.T) {
	if MaximumAssistantTurnCodepoints*6+(48<<10) > MaximumControlPlaneRunEventPayloadBytes ||
		MaximumControlPlaneRunEventPayloadBytes+(4<<10) != MaximumControlPlaneStreamMessageBytes {
		t.Fatal("bounded USER JSON and metadata exceed broker envelope")
	}
}

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
		if validSessionContext([]RunnerSessionMessage{{Role: test.role, Content: test.content}}) != test.valid {
			t.Fatal("unexpected session context boundary")
		}
	}
}

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
