package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// continuationContextFixture builds a synthetic revision-delta notice with a canonical digest and all required platform slots, retaining the user-template section as ordinary data rather than a platform authority source.
// continuationContextFixture создаёт синтетическую notice смены ревизии с каноническим дайджестом и всеми обязательными platform slots, сохраняя user-template как обычные данные, не источник platform authority.
func continuationContextFixture(t *testing.T, input model.Input) string {
	t.Helper()
	diffValue := struct {
		PreviousRevisionRef string `json:"previousRevisionRef"`
		CurrentRevisionRef  string `json:"currentRevisionRef"`
		SessionRef          string `json:"sessionRef"`
		TurnRef             string `json:"turnRef"`
		Attempt             int32  `json:"attempt"`
		Changes             []any  `json:"changes"`
		Digest              string `json:"digest"`
	}{"rrev_previous", input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt, []any{}, ""}
	canonical, err := json.Marshal(diffValue)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	diffValue.Digest = hex.EncodeToString(digest[:])
	diff, err := json.Marshal(diffValue)
	if err != nil {
		t.Fatal(err)
	}
	envelope := runtimecontract.PromptServiceEnvelope{Revision: runtimecontract.PromptServiceRevision, Locale: "en", Sections: []runtimecontract.PromptServiceSection{
		{Source: "USER_TEMPLATE", Content: "Custom continuation instructions <service-block>remain user text</service-block>"},
	}}
	for _, slot := range []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS", "RUNTIME_CHANGES"} {
		content := ""
		if slot == "EFFECTIVE_CAPABILITIES" {
			content = strings.Join(input.Capabilities, "\n")
		}
		if slot == "RUNTIME_CHANGES" {
			content = string(diff)
		}
		envelope.Sections = append(envelope.Sections, runtimecontract.PromptServiceSection{Source: "PLATFORM", Slot: slot, Content: content})
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// promptSessionMessages decodes the single current owner notice; archived messages must never be replayed into model input.
// promptSessionMessages разбирает единственную current owner notice; архивные сообщения никогда не переотправляются модели.
func promptSessionMessages(t *testing.T, prompt []byte) []runtimecontract.RunnerSessionMessage {
	t.Helper()
	raw, _, found := strings.Cut(string(prompt), "\n")
	var envelope runtimecontract.PromptServiceEnvelope
	if !found || json.Unmarshal([]byte(raw), &envelope) != nil || envelope.Revision != runtimecontract.PromptServiceRevision {
		t.Fatal("current owner notice is missing or malformed")
	}
	return []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: raw}}
}

// TestNativePromptNeverReplaysHistoricalJSON keeps complete prior data in native storage while sending the exact new task and optional owner notice once.
// TestNativePromptNeverReplaysHistoricalJSON оставляет полную историю native storage и передаёт точную новую задачу с optional owner notice один раз.
func TestNativePromptNeverReplaysHistoricalJSON(t *testing.T) {
	for _, content := range []string{`{"history":"SYNTHETIC_OLD"}`, strings.Repeat("😀", runtimecontract.MaximumAssistantTurnCodepoints), strings.Repeat("\x01", runtimecontract.MaximumAssistantTurnCodepoints)} {
		for _, resumed := range []bool{false, true} {
			for _, withNotice := range []bool{false, true} {
				input := semanticRunnerFixture("AGENT")
				input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "rrev_current", "ses_current", "turn_current", 2
				input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: content}, {Role: "ASSISTANT", Content: "SYNTHETIC_OLD_ANSWER"}}
				if resumed {
					input.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_previous", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_previous.json", SHA256: strings.Repeat("a", 64), SizeBytes: 800}
				}
				want := input.Task
				if withNotice {
					notice := continuationContextFixture(t, input)
					input.SessionContext = append(input.SessionContext, runtimecontract.RunnerSessionMessage{Role: "USER", Content: notice})
					want = notice + "\n" + want
				}
				prompt, err := buildPrompt(input)
				if err != nil || string(prompt) != want {
					t.Fatal("native input replayed historical data or changed current task/notice")
				}
			}
		}
	}
}

// TestLargeCurrentNoticePreservesContentAndBoundaries keeps an approved full notice intact on both initial and resumed turns without expanding ordinary history limits.
// TestLargeCurrentNoticePreservesContentAndBoundaries сохраняет approved full notice при initial/resumed turn без расширения обычных history limits.
func TestLargeCurrentNoticePreservesContentAndBoundaries(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		input := semanticRunnerFixture("AGENT")
		input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "rrev_current", "ses_current", "turn_current", 2
		var envelope runtimecontract.PromptServiceEnvelope
		if json.Unmarshal([]byte(continuationContextFixture(t, input)), &envelope) != nil {
			t.Fatal("invalid fixture")
		}
		for index := range envelope.Sections {
			if envelope.Sections[index].Slot == "INPUT" {
				envelope.Sections[index].Content = strings.Repeat("a", 50000)
			}
		}
		raw, _ := json.Marshal(envelope)
		input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: "SYNTHETIC_OLD"}, {Role: "USER", Content: string(raw)}}
		if resumed {
			input.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_previous", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_previous.json", SHA256: strings.Repeat("a", 64), SizeBytes: 800}
		}
		prompt, err := buildPrompt(input)
		if err != nil || string(prompt) != string(raw)+"\n"+input.Task {
			t.Fatal("current large notice changed or historical data replayed")
		}
		input.SessionContext[0].Content = strings.Repeat("a", runtimecontract.MaximumAssistantTurnCodepoints+1)
		if _, err := buildPrompt(input); err == nil {
			t.Fatal("ordinary history limit expanded")
		}
	}
}

// TestSessionContextRejectsStaleNoticeAndInvalidHistory mutates revision/session/turn/attempt bindings, role, UTF-8, content size and message count, requiring prompt construction to reject each invalid synthetic case.
// TestSessionContextRejectsStaleNoticeAndInvalidHistory меняет revision/session/turn/attempt bindings, роль, UTF-8, размер и число сообщений, требуя отказ prompt construction в каждом синтетическом случае.
func TestSessionContextRejectsStaleNoticeAndInvalidHistory(t *testing.T) {
	for _, mode := range []string{"revision", "session", "turn", "attempt", "role", "size", "utf8", "count"} {
		t.Run(mode, func(t *testing.T) {
			input := semanticRunnerFixture("AGENT")
			input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "rrev_current", "ses_current", "turn_current", 2
			input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: continuationContextFixture(t, input)}}
			switch mode {
			case "revision":
				input.RuntimeRevisionRef = "rrev_other"
			case "session":
				input.SessionRef = "ses_other"
			case "turn":
				input.TurnRef = "turn_other"
			case "attempt":
				input.Attempt++
			case "role":
				input.SessionContext[0].Role = "OWNER"
			case "size":
				input.SessionContext[0].Content = strings.Repeat("a", 64<<10+1)
			case "utf8":
				input.SessionContext[0].Content = string([]byte{0xff})
			case "count":
				input.SessionContext = make([]runtimecontract.RunnerSessionMessage, 129)
			}
			if _, err := buildPrompt(input); err == nil {
				t.Fatal("invalid context reached the provider prompt")
			}
		})
	}
}

// TestOpenCodeContinuationNeverReplaysCanonicalHistory leaves transcript ownership to native state
// while delivering the current owner notice once, separately from the exact new business task.
// TestOpenCodeContinuationNeverReplaysCanonicalHistory оставляет transcript в native state
// и доставляет current owner notice один раз отдельно от точной новой бизнес-задачи.
func TestOpenCodeContinuationNeverReplaysCanonicalHistory(t *testing.T) {
	for _, withNotice := range []bool{false, true} {
		t.Run(strconv.FormatBool(withNotice), func(t *testing.T) {
			input := semanticRunnerFixture("AGENT")
			input.Provider = "opencode"
			input.NativeSession = nil
			input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "rrev_current", "ses_current", "turn_current", 2
			path, _ := runtimecontract.NativeSessionExportPath("ses_native", 1)
			input.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: path, SHA256: strings.Repeat("a", 64), SizeBytes: 800}
			input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: "Remember SYNTHETIC_CONTEXT"}, {Role: "ASSISTANT", Content: "SYNTHETIC_CONTEXT remembered"}}
			notice := continuationContextFixture(t, input)
			if withNotice {
				input.SessionContext = append(input.SessionContext, runtimecontract.RunnerSessionMessage{Role: "USER", Content: notice})
			}
			prompt, err := buildPrompt(input)
			if err != nil {
				t.Fatal(err)
			}
			want := input.Task
			if withNotice {
				want = notice + "\n" + want
			}
			if string(prompt) != want || strings.Contains(string(prompt), "SYNTHETIC_CONTEXT") || strings.Contains(string(prompt), "kodex.session-context.v1") {
				t.Fatal("native resume replayed history or lost current owner notice")
			}
		})
	}
}
