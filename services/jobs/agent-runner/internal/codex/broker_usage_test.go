package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestProviderRefreshFailurePreservesMeasurementsAcrossBrokerTransport(t *testing.T) {
	for _, mode := range []string{"missing auth", "invalid auth", "changed API key", "commit", "cancelled commit", "execution", "before effect"} {
		t.Run(mode, func(t *testing.T) {
			input, authPath := providerTurnFixture(t, []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"synthetic-old"}}`))
			usage := runtimecontract.TokenUsage{TotalTokens: 30, InputTokens: 20, OutputTokens: 10, ReasoningOutputTokens: 3}
			want := Result{Usage: usage, FinalMessage: "unconfirmed final", Outcome: "SUCCEEDED", SessionID: "unconfirmed-session",
				ArchivePath: "unconfirmed-archive", ToolCalls: []runtimecontract.NativeToolCall{{CallID: "call-one", Kind: runtimecontract.NativeToolKindSleep,
					State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
					SafeParameters: map[string]any{"requested_duration_ms": int64(25)}, DurationMS: 25}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executions, commits := 0, 0
			result, executionErr := executeProviderTurn(ctx, input, []byte("synthetic task"), strings.Repeat("a", 64),
				func(context.Context, model.Input, []byte, string) (Result, error) {
					executions++
					switch mode {
					case "missing auth":
						if err := os.Remove(authPath); err != nil {
							t.Fatal(err)
						}
					case "invalid auth":
						if err := os.WriteFile(authPath, []byte("invalid"), 0o600); err != nil {
							t.Fatal(err)
						}
					case "changed API key":
						if err := os.WriteFile(authPath, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-changed"}`), 0o600); err != nil {
							t.Fatal(err)
						}
					case "commit", "cancelled commit":
						if err := os.WriteFile(authPath, []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"synthetic-new"}}`), 0o600); err != nil {
							t.Fatal(err)
						}
						if mode == "cancelled commit" {
							cancel()
						}
					case "execution":
						return want, errors.New("synthetic execution failure")
					case "before effect":
						return Result{}, errors.New("synthetic pre-effect failure")
					}
					return want, nil
				}, func(ctx context.Context, got model.Input, payload runtimecontract.RunnerProviderCredentialRefreshRequest) error {
					commits++
					deadline, bounded := ctx.Deadline()
					if ctx.Err() != nil || !bounded || time.Until(deadline) > providerRefreshCommitTimeout ||
						got.RuntimeRevisionDigest != input.RuntimeRevisionDigest || payload.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
						payload.PreviousContentSHA256 != input.ProviderCredentialSHA256 || payload.Validate() != nil {
						t.Error("credential commit lost independent budget or exact binding")
					}
					return errors.New("synthetic commit denial")
				})
			if executionErr == nil {
				t.Fatal("provider failure became success")
			}
			reader, writer := io.Pipe()
			written := make(chan error, 1)
			go func() {
				err := writeProviderBrokerResultFailure(writer, result, executionErr)
				_ = writer.CloseWithError(err)
				written <- err
			}()
			got, err := readProviderBrokerResponse(reader)
			_ = reader.Close()
			if writeErr := <-written; writeErr != nil || err == nil {
				t.Fatal("broker failed to carry a typed failure")
			}
			if mode == "before effect" {
				want = Result{}
			}
			// JSON меняет числовые значения map, поэтому сверяем канонические bytes.
			gotCalls, _ := json.Marshal(got.ToolCalls)
			wantCalls, _ := json.Marshal(want.ToolCalls)
			if got.Usage != want.Usage || !bytes.Equal(gotCalls, wantCalls) || got.Outcome != "" || got.FinalMessage != "" ||
				got.SessionID != "" || got.ArchivePath != "" || got.ArchiveRelativePath != "" || got.ArchiveSHA256 != "" || got.ArchiveSizeBytes != 0 {
				t.Fatal("broker lost measurements or published an unconfirmed result")
			}
			wantCommits := 0
			if mode == "commit" || mode == "cancelled commit" {
				wantCommits = 1
			}
			if executions != 1 || commits != wantCommits {
				t.Fatal("provider effect was retried or credential authority was bypassed")
			}
			assertRemoved(t, authPath)
		})
	}
}

func TestBrokerRejectsMalformedPartialMeasurements(t *testing.T) {
	for _, mode := range []string{"unknown failure", "invalid usage", "invalid call", "too many calls", "trailing data"} {
		t.Run(mode, func(t *testing.T) {
			response := brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: runtimecontract.TokenUsage{TotalTokens: 2, InputTokens: 1, OutputTokens: 1}}}
			switch mode {
			case "unknown failure":
				response.Failure = "FUTURE"
			case "invalid usage":
				response.Result.Usage.TotalTokens = -1
			case "invalid call":
				response.Result.ToolCalls = []runtimecontract.NativeToolCall{{CallID: "invalid"}}
			case "too many calls":
				response.Result.ToolCalls = make([]runtimecontract.NativeToolCall, runtimecontract.MaximumNativeToolCalls+1)
			}
			encoded, _ := json.Marshal(response)
			if mode == "trailing data" {
				encoded = append(encoded, []byte(" {}")...)
			}
			result, err := readProviderBrokerResponse(bytes.NewReader(encoded))
			if err == nil || !reflect.DeepEqual(result, Result{}) {
				t.Fatal("invalid broker envelope produced partial measurements")
			}
		})
	}
}

func TestMeasuredResultUsesOnlyPreviouslyValidatedTurnObservations(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	if err := state.notification("thread/tokenUsage/updated", tokenUsageNotification("01980000-0000-7000-8000-000000000099", 100, 80, 20, 10, 20, 5)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.measuredResult(), Result{}) {
		t.Fatal("resumed thread baseline was charged to a new turn")
	}
	if err := state.captureUsageBaseline(); err != nil {
		t.Fatal(err)
	}
	state.turnID = testTurnID
	if err := state.notification("thread/tokenUsage/updated", tokenUsageNotification(testTurnID, 170, 140, 60, 20, 30, 8)); err != nil {
		t.Fatal(err)
	}
	before := state.measuredResult()
	if before.Usage.TotalTokens != 70 || before.Usage.InputTokens != 60 || before.Usage.OutputTokens != 10 || before.Outcome != "" || before.SessionID != "" {
		t.Fatal("partial turn lost measured delta or invented a terminal")
	}
	if err := state.notification("thread/tokenUsage/updated", tokenUsageNotification(testTurnID, 1, 140, 60, 20, 30, 8)); err == nil {
		t.Fatal("malformed later observation was accepted")
	}
	if !reflect.DeepEqual(state.measuredResult(), before) {
		t.Fatal("failed parsing changed the last validated measurements")
	}
}

type deadlineRecordingConnection struct {
	net.Conn
	mu       sync.Mutex
	deadline time.Time
}

func (connection *deadlineRecordingConnection) SetReadDeadline(deadline time.Time) error {
	connection.mu.Lock()
	connection.deadline = deadline
	connection.mu.Unlock()
	return connection.Conn.SetReadDeadline(deadline)
}

func TestCancelledBrokerConnectionReceivesBoundedFinalMeasurements(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	connection := &deadlineRecordingConnection{Conn: client}
	ctx, cancel := context.WithCancel(t.Context())
	stop := bindBrokerConnectionContext(ctx, connection)
	cancel()
	// Ждём callback отмены через тот же обязательный join, без sleep.
	stop()
	connection.mu.Lock()
	remaining := time.Until(connection.deadline)
	connection.mu.Unlock()
	if remaining <= 0 || remaining > providerResultDeliveryGrace {
		t.Fatal("cancelled broker has no bounded result delivery budget")
	}
	done := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		err := writeProviderBrokerResultFailure(server, Result{Usage: usage}, errors.New("synthetic cancellation"))
		_ = server.Close()
		done <- err
	}()
	result, err := readProviderBrokerResponse(connection)
	if writeErr := <-done; writeErr != nil || err == nil || result.Usage != usage {
		t.Fatal("cancellation discarded already measured usage")
	}
}
