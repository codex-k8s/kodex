package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// stoppedExecutorProxy exposes one real orchestration boundary, with an executor already
// stopped by its independent container lifecycle. It cannot manufacture a provider join proof.
// stoppedExecutorProxy предоставляет границу настоящей orchestration при уже остановленном
// lifecycle executor; provider join proof эта оснастка не создаёт.
type stoppedExecutorProxy struct {
	turnProxyFixture
	quiesced bool
	joinErr  error
}

// QuiesceWorkspace verifies the role supplies an independent cleanup context after its turn
// was cancelled, then reports the late-UDS failure reproduced separately with the real PID1.
// QuiesceWorkspace проверяет независимый cleanup context после отмены turn и сообщает поздний
// UDS failure, отдельно воспроизведённый настоящим PID1 executor.
func (proxy *stoppedExecutorProxy) QuiesceWorkspace(ctx context.Context) error {
	if ctx.Err() != nil {
		proxy.t.Fatal("role cleanup inherited cancelled execution context")
	}
	proxy.quiesced = true
	return proxy.joinErr
}

// TestCancelledTurnMissingExecutorJoinDoesNotPublishQuiescence reproduces the observable
// app outcome: acknowledged measured failure returns nil while quiescence publication remains
// closed. Native capture preservation is qualified independently at the real native boundary.
// TestCancelledTurnMissingExecutorJoinDoesNotPublishQuiescence воспроизводит исход app:
// подтверждённый measured failure возвращает nil без публикации quiescence. Сохранность native
// capture проверяется отдельно на настоящей native-границе, не подменяется полями Result.
func TestCancelledTurnMissingExecutorJoinDoesNotPublishQuiescence(t *testing.T) {
	input := acceptanceTurnFixture(t, "agent")
	input.Provider = "opencode"
	input.EnvironmentPolicy.ManagedWorkspace = true
	input.WorkspaceStorage = &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile}
	usage := runtimecontract.TokenUsage{InputTokens: 4, OutputTokens: 1, TotalTokens: 5}
	progress, completed := 0, 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/executions/lease_fixture/progress":
			progress++
		case "/v1/executions/lease_fixture/complete":
			var payload runtimecontract.RunnerCompletionRequest
			if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Validate() != nil || payload.Success || payload.SafeErrorCode != "RUNTIME_UNAVAILABLE" || payload.Usage != usage || len(payload.Artifacts) != 0 {
				t.Error("cancelled executor failure changed measured completion or published an artifact")
			}
			completed++
		default:
			t.Error("unexpected callback during executor cancellation")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := completionTestClient(t, &input, server)
	proxy := &stoppedExecutorProxy{turnProxyFixture: turnProxyFixture{t: t}, joinErr: errors.New("synthetic stopped executor")}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	runtime := turnRuntime{
		prepare: func(ctx context.Context, _ model.Input, _ *callback.Client) (preparedTurn, string, error) {
			return preparedTurn{ctx: ctx, proxy: proxy, cancel: func() {}}, "", nil
		},
		execute: func(context.Context, model.Input, []byte, string, string, func(runtimecontract.RuntimeActivity) error) (codex.Result, error) {
			cancel()
			return codex.Result{Outcome: "FAILED", Usage: usage}, context.Canceled
		},
	}
	if err := runTurn(ctx, input, client, nil, runtime); err != nil {
		t.Fatal("acknowledged failure was not delivered after cancellation", err)
	}
	if !proxy.quiesced || !proxy.closed || progress != 1 || completed != 1 || !strings.Contains(logs.String(), `"quiescence_stage":"EXECUTOR_JOIN"`) || strings.Contains(logs.String(), `"quiescence_stage":"WRITER_ACK"`) {
		t.Fatal("executor denial did not stop before positive-record publication or lost its closed stage")
	}
}

// TestWorkspacePublicationRequiresAcknowledgedExecutor checks the actual app sequencing
// primitive without manufacturing a broker witness or an operational quiescence receipt.
// TestWorkspacePublicationRequiresAcknowledgedExecutor проверяет порядок настоящего app
// primitive без создания broker witness либо operational quiescence receipt.
func TestWorkspacePublicationRequiresAcknowledgedExecutor(t *testing.T) {
	for _, mode := range []string{"acknowledged", "unknown", "publisher-denied"} {
		t.Run(mode, func(t *testing.T) {
			input := acceptanceTurnFixture(t, "agent")
			input.EnvironmentPolicy.ManagedWorkspace = true
			proxy := &stoppedExecutorProxy{turnProxyFixture: turnProxyFixture{t: t}}
			if mode == "unknown" {
				proxy.joinErr = errors.New("synthetic unknown executor acknowledgement")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			published := 0
			err := quiesceAndPublishWorkspace(ctx, input, codex.Result{}, proxy, func(actual model.Input, _ codex.Result) error {
				if !proxy.quiesced || proxy.joinErr != nil || actual.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
					t.Fatal("publisher ran before acknowledged same-execution executor join")
				}
				published++
				if mode == "publisher-denied" {
					return errors.New("synthetic publisher still requires sealed provider proof")
				}
				return nil
			})
			wantCalls := 1
			if mode == "unknown" {
				wantCalls = 0
			}
			if published != wantCalls || (err == nil) != (mode == "acknowledged") {
				t.Fatal("app sequence lost acknowledged/unknown/publisher-denied distinction")
			}
		})
	}
}
