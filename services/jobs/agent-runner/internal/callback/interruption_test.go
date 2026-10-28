package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// interruptionFixture binds synthetic provider cancellation, partial usage, an
// artifact and archive pins to the existing valid turn without external effects.
// interruptionFixture связывает синтетическую отмену, частичный расход, артефакт
// и архив с прежним корректным ходом без внешних эффектов.
func interruptionFixture() (runtimecontract.RunnerInput, runtimecontract.RunnerCompletionRequest) {
	input := validWarmTurnFixture()
	content := []byte("published result")
	digest := sha256.Sum256(content)
	return input, runtimecontract.RunnerCompletionRequest{
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt,
		SafeErrorCode: "PROVIDER_UNAVAILABLE", ResultSummary: "i18n:RUNTIME_PROVIDER_UNAVAILABLE",
		Usage:               runtimecontract.TokenUsage{Completeness: runtimecontract.UsagePartial, InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
		Artifacts:           []runtimecontract.RunnerArtifact{{FileName: "result.md", MediaType: "text/markdown", SHA256: hex.EncodeToString(digest[:]), Content: content}},
		CodexSessionID:      "00000000-0000-4000-8000-000000000001",
		ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000001.jsonl",
		ArchiveSHA256:       strings.Repeat("f", 64), ArchiveSizeBytes: 123,
		ProviderDiagnostic: &runtimecontract.ProviderFailureDiagnostic{Schema: runtimecontract.ProviderFailureDiagnosticSchema,
			RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
			SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
			Kind: "REQUEST_FAILURE", Stage: "TERMINAL_WAIT", Class: "PROVIDER", Detail: "CONTEXT_CANCELLED",
			Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE"},
	}
}

// TestInterruptedCompletionRequiresExactValidatedFailure proves the narrow mapping
// preserves unrelated failures and every non-presentation field, including usage
// quality and durable artifact/archive pins; it grants no new authority.
// TestInterruptedCompletionRequiresExactValidatedFailure доказывает узость mapping,
// сохранение прочих отказов и всех полей кроме сообщения/кода, включая качество
// расхода и привязки артефакта/архива; новых полномочий не появляется.
func TestInterruptedCompletionRequiresExactValidatedFailure(t *testing.T) {
	input, payload := interruptionFixture()
	if err := payload.Validate(); err != nil {
		t.Fatal("invalid interruption fixture")
	}
	want := payload
	want.SafeErrorCode, want.ResultSummary = "RUNTIME_UNAVAILABLE", runtimeInterruptedSummary
	if got := interruptedCompletion(input, payload); !reflect.DeepEqual(got, want) {
		t.Fatal("mapping changed execution evidence or did not remove the provider claim")
	}
	for _, scenario := range []struct {
		name   string
		change func(*runtimecontract.RunnerCompletionRequest)
	}{
		{"no diagnostic", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic = nil }},
		{"success", func(p *runtimecontract.RunnerCompletionRequest) { p.Success = true }},
		{"authentication", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "PROVIDER_AUTH_REJECTED" }},
		{"timeout", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "RUNTIME_LIMIT_EXCEEDED" }},
		{"cancel", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "RUNTIME_CANCELLED" }},
		{"workspace", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "RUNTIME_ARTIFACT_INVALID" }},
		{"already normalized", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "RUNTIME_UNAVAILABLE" }},
		{"prefixed code", func(p *runtimecontract.RunnerCompletionRequest) { p.SafeErrorCode = "RUNTIME_PROVIDER_UNAVAILABLE" }},
		{"attempt", func(p *runtimecontract.RunnerCompletionRequest) { p.Attempt++ }},
		{"revision", func(p *runtimecontract.RunnerCompletionRequest) { p.RuntimeRevisionDigest = strings.Repeat("e", 64) }},
		{"diagnostic input", func(p *runtimecontract.RunnerCompletionRequest) {
			p.ProviderDiagnostic.InputDigest = strings.Repeat("e", 64)
		}},
		{"diagnostic execution", func(p *runtimecontract.RunnerCompletionRequest) {
			p.ProviderDiagnostic.ExecutionBindingDigest = strings.Repeat("e", 64)
		}},
		{"diagnostic session", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.SessionRef = "session_foreign" }},
		{"diagnostic turn", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.TurnRef = "turn_foreign" }},
		{"stage", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.Stage = "TURN_START" }},
		{"class", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.Class = "AUTHENTICATION" }},
		{"detail", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.Detail = "STREAM_CLOSED" }},
		{"malformed", func(p *runtimecontract.RunnerCompletionRequest) {
			p.ProviderDiagnostic.Notification = "PRIVATE_SENTINEL"
		}},
		{"terminal failure", func(p *runtimecontract.RunnerCompletionRequest) { p.ProviderDiagnostic.Kind = "TERMINAL_FAILURE" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, other := interruptionFixture()
			scenario.change(&other)
			if got := interruptedCompletion(input, other); !reflect.DeepEqual(got, other) {
				t.Fatal("unrelated completion changed")
			}
		})
	}
}

// TestInterruptedCompletionLostACKKeepsFrozenHTTPIntent simulates a committed
// first request with a lost HTTP reply, then a retry after a mutable observation
// changes. The callback serializes once: two requests carry identical evidence
// and the simulated owner accepts one intent. No Kubernetes or PostgreSQL is used.
// TestInterruptedCompletionLostACKKeepsFrozenHTTPIntent имитирует commit первого
// запроса с потерей HTTP-ответа и повтор после изменения наблюдения. Callback
// сериализуется один раз: два запроса несут одинаковые данные, условный owner
// принимает один intent. Kubernetes и PostgreSQL здесь не используются.
func TestInterruptedCompletionLostACKKeepsFrozenHTTPIntent(t *testing.T) {
	input, payload := interruptionFixture()
	var mu sync.Mutex
	var bodies [][]byte
	observedPod := "RUNNING"
	ownerIntents := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error("read callback body")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			ownerIntents++
			observedPod = "Evicted"
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error("hijack callback response")
				return
			}
			_ = connection.Close()
			return
		}
		if !bytes.Equal(body, bodies[0]) {
			ownerIntents++
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond}}
	if err := client.Complete(context.Background(), input, payload); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || ownerIntents != 1 || observedPod != "Evicted" || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("lost ACK changed immutable callback intent")
	}
	var delivered runtimecontract.RunnerCompletionRequest
	if err := json.Unmarshal(bodies[0], &delivered); err != nil {
		t.Fatal(err)
	}
	want := interruptedCompletion(input, payload)
	if !reflect.DeepEqual(delivered, want) {
		t.Fatal("HTTP callback lost completion evidence")
	}
}
