package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestExecutedUsageCompletenessCallback crosses real completion validation and mutual-TLS delivery
// for UNKNOWN zero, COMPLETE zero and PARTIAL subtotal on both successful and failed turns.
// TestExecutedUsageCompletenessCallback проходит настоящую completion validation и mutual-TLS
// доставку UNKNOWN0, COMPLETE0 и PARTIAL-подытога при успешных и неуспешных ходах.
func TestExecutedUsageCompletenessCallback(t *testing.T) {
	for _, quality := range []codex.UsageCompleteness{codex.UsageUnknown, codex.UsageComplete, codex.UsagePartial} {
		for _, success := range []bool{true, false} {
			name, _ := json.Marshal(quality)
			t.Run(string(name)+map[bool]string{true: "-success", false: "-failure"}[success], func(t *testing.T) {
				input := model.Input{RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 1, LeaseRef: "lease_fixture"}
				usage := runtimecontract.TokenUsage{ModelContextWindow: 475000}
				if quality == codex.UsagePartial {
					usage.TotalTokens, usage.InputTokens = 100, 100
				}
				result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "Synthetic result", Usage: usage, UsageCompleteness: quality}
				if !success {
					result.Outcome, result.FailureCode = "FAILED", "usage_limit_exceeded"
				}
				calls := 0
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					calls++
					var payload runtimecontract.RunnerCompletionRequest
					if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Validate() != nil ||
						payload.Usage.Completeness != quality || payload.Usage.TotalTokens != usage.TotalTokens ||
						payload.Usage.ModelContextWindow != 475000 || payload.Success != success {
						t.Error("terminal callback collapsed measured zero or lost usage quality")
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
				server.StartTLS()
				defer server.Close()
				client := completionTestClient(t, &input, server)
				if err := completeExecutedTurn(t.Context(), input, client, result, func(context.Context) error { return nil }, nil); err != nil || calls != 1 {
					t.Fatal("native terminal callback was not delivered exactly once")
				}
			})
		}
	}
}
