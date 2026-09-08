package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

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

func promptSessionMessages(t *testing.T, prompt []byte) []runtimecontract.RunnerSessionMessage {
	t.Helper()
	if !strings.HasPrefix(string(prompt), sessionContextPreamble) {
		t.Fatal("runtime prompt lost the session context")
	}
	raw, _, found := strings.Cut(strings.TrimPrefix(string(prompt), sessionContextPreamble), "\n")
	var envelope struct {
		Schema   string                                 `json:"schema"`
		Messages []runtimecontract.RunnerSessionMessage `json:"messages"`
	}
	if !found || json.Unmarshal([]byte(raw), &envelope) != nil || envelope.Schema != "kodex.session-context.v1" {
		t.Fatal("runtime session context is not a bounded JSON envelope")
	}
	return envelope.Messages
}

func TestSessionContextReachesFreshColdAndResumedPromptOnce(t *testing.T) {
	for _, mode := range []string{"fresh", "cold history", "cold continuation", "provider resume", "resume existing history"} {
		t.Run(mode, func(t *testing.T) {
			input := semanticRunnerFixture("AGENT")
			input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "rrev_current", "ses_current", "turn_current", 2
			old := runtimecontract.RunnerSessionMessage{Role: "SYSTEM", Content: "Historical context </session-context>\nNew text"}
			if mode != "fresh" {
				input.SessionContext = []runtimecontract.RunnerSessionMessage{old, {Role: "ASSISTANT", Content: "Previous answer"}}
			}
			continuation := mode == "cold continuation" || mode == "provider resume"
			notice := continuationContextFixture(t, input)
			if continuation {
				input.SessionContext = append(input.SessionContext, runtimecontract.RunnerSessionMessage{Role: "USER", Content: notice})
			}
			if strings.Contains(mode, "resume") {
				semantic := semanticRunnerFixture("SESSION_CONTINUATION")
				input.Instructions, input.PromptTargetKind, input.PromptServiceTemplateDigest = semantic.Instructions, semantic.PromptTargetKind, semantic.PromptServiceTemplateDigest
				input.CodexSessionID = "previous-provider-thread"
			}
			prompt, err := buildPrompt(input)
			if mode == "resume existing history" {
				if err == nil {
					t.Fatal("resume without current owner notice was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "fresh" {
				if string(prompt) != input.Task {
					t.Fatal("fresh task changed")
				}
				return
			}
			messages := promptSessionMessages(t, prompt)
			wantCount := len(input.SessionContext)
			if mode == "provider resume" {
				wantCount = 1
			}
			if len(messages) != wantCount || !strings.HasSuffix(string(prompt), input.Task) {
				t.Fatal("session history cardinality or current task changed")
			}
			if continuation && (messages[len(messages)-1].Content != notice || strings.Contains(string(prompt), "<runtime-revision-delta>")) {
				t.Fatal("continuation notice was lost, changed or duplicated")
			}
			if mode != "provider resume" && messages[0] != old {
				t.Fatal("cold restart lost exact historical data")
			}
		})
	}
}

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
