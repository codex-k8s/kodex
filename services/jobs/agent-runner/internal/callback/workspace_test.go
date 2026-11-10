package callback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// workspaceObservationFixture supplies a synthetic BIND and its exact owner acknowledgement, without production authority.
// workspaceObservationFixture создаёт синтетические BIND и exact owner ACK без производственных полномочий.
func workspaceObservationFixture(t *testing.T) (model.Input, runtimecontract.RunnerWorkspaceObservation, []byte) {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	input := model.Input{RoleRuntimeContractRevision: runtimecontract.WorkspaceStorageRuntimeContractRevision, Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone,
		OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, AgentRef: scope.AgentRef,
		RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000", RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000", LeaseGeneration: 1, Attempt: 1,
		WorkspaceStorage: &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 1}}
	observation := runtimecontract.RunnerWorkspaceObservation{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Phase: runtimecontract.WorkspaceObserveBind, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, Generation: 1}
	ack := runtimecontract.WorkspaceWriterAcknowledgement{Schema: runtimecontract.WorkspaceWriterAcknowledgementSchema, Scope: scope, WorkspaceRef: observation.WorkspaceRef, Generation: 1,
		Writer: runtimecontract.WorkspaceWriterCommitment{RunRef: input.RunRef, NodeRef: input.NodeRef, TurnRef: input.TurnRef, RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: 1, RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: 1, Attempt: 1, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}}
	raw, err := runtimecontract.EncodeWorkspaceWriterAcknowledgement(ack)
	if err != nil || !ack.Matches(input) || observation.ValidateForInput(input) != nil {
		t.Fatal("invalid synthetic workspace fixture")
	}
	return input, observation, raw
}

// TestObserveWorkspaceRetriesOnlyBeforeTCPConnection models a cold allow-rule with one refused dial, then verifies one exact authenticated POST and owner ACK.
// TestObserveWorkspaceRetriesOnlyBeforeTCPConnection моделирует один refused dial до применения allow-rule и проверяет единственный exact POST с owner ACK.
func TestObserveWorkspaceRetriesOnlyBeforeTCPConnection(t *testing.T) {
	input, observation, ack := workspaceObservationFixture(t)
	var posts, dials atomic.Int32
	client := callbackFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer ticket" || r.URL.Path != "/v1/executions/lease_fixture000/workspace" {
			t.Error("workspace callback route or authentication changed")
		}
		var got runtimecontract.RunnerWorkspaceObservation
		if json.NewDecoder(r.Body).Decode(&got) != nil || got != observation {
			t.Error("workspace callback observation changed")
		}
		_, _ = w.Write(ack)
	}))
	transport := client.http.Transport.(*http.Transport).Clone()
	original := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, &net.OpError{Op: "dial", Net: network, Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
		}
		return original(ctx, network, address)
	}
	client.http.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	got, err := client.ObserveWorkspace(t.Context(), input, observation)
	if err != nil || !got.Matches(input) || dials.Load() != 2 || posts.Load() != 1 {
		t.Fatalf("BIND: error=%v, dials=%d, posts=%d", err, dials.Load(), posts.Load())
	}
}

// TestObserveWorkspaceNeverReplaysAnUncertainEffect rejects HTTP errors, lost ACKs and redirects after one owner request, even when another endpoint could accept a replay.
// TestObserveWorkspaceNeverReplaysAnUncertainEffect отклоняет HTTP errors, lost ACK и redirects после одного запроса без повтора эффекта.
func TestObserveWorkspaceNeverReplaysAnUncertainEffect(t *testing.T) {
	for _, kind := range []string{"status", "lost acknowledgement", "redirect", "invalid acknowledgement"} {
		t.Run(kind, func(t *testing.T) {
			input, observation, ack := workspaceObservationFixture(t)
			var posts atomic.Int32
			client := callbackFixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				switch kind {
				case "status":
					http.Error(w, "private diagnostic", http.StatusServiceUnavailable)
				case "lost acknowledgement":
					c, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = c.Close()
				case "redirect":
					if r.URL.Path == "/replay" {
						_, _ = w.Write(ack)
						return
					}
					http.Redirect(w, r, "/replay", http.StatusTemporaryRedirect)
				default:
					_, _ = w.Write([]byte("private invalid acknowledgement"))
				}
			}))
			got, err := client.ObserveWorkspace(t.Context(), input, observation)
			if err == nil || got.Matches(input) || posts.Load() != 1 || strings.Contains(err.Error(), "private") {
				t.Fatalf("uncertain BIND: error=%v, posts=%d", err, posts.Load())
			}
		})
	}
}

// TestObserveWorkspaceTransportFailureIsNotAConnectionRetry refuses untyped errors and post-connect read failures without another network attempt.
// TestObserveWorkspaceTransportFailureIsNotAConnectionRetry не повторяет untyped errors и post-connect read failures.
func TestObserveWorkspaceTransportFailureIsNotAConnectionRetry(t *testing.T) {
	for _, cause := range []error{syscall.ECONNREFUSED, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNREFUSED}, errors.New("private transport failure")} {
		t.Run(cause.Error(), func(t *testing.T) {
			input, observation, _ := workspaceObservationFixture(t)
			var dials atomic.Int32
			client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected owner request") }))
			transport := client.http.Transport.(*http.Transport).Clone()
			transport.DialContext = func(context.Context, string, string) (net.Conn, error) { dials.Add(1); return nil, cause }
			client.http.Transport = transport
			t.Cleanup(transport.CloseIdleConnections)
			_, err := client.ObserveWorkspace(t.Context(), input, observation)
			if err == nil || dials.Load() != 1 || strings.Contains(err.Error(), "private") {
				t.Fatalf("transport BIND: error=%v, dials=%d", err, dials.Load())
			}
		})
	}
}

// TestObserveWorkspaceRefusalsHaveAFixedBudget exhausts only the closed dial schedule without delivering an owner request.
// TestObserveWorkspaceRefusalsHaveAFixedBudget исчерпывает фиксированное расписание dial без отправки owner request.
func TestObserveWorkspaceRefusalsHaveAFixedBudget(t *testing.T) {
	input, observation, _ := workspaceObservationFixture(t)
	var dials atomic.Int32
	client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected owner request") }))
	transport := client.http.Transport.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}
	client.http.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	start := time.Now()
	_, err := client.ObserveWorkspace(t.Context(), input, observation)
	if err == nil || dials.Load() != 5 || time.Since(start) > 6*time.Second {
		t.Fatalf("unbounded BIND: error=%v, dials=%d", err, dials.Load())
	}
	var waits time.Duration
	for _, delay := range workspaceConnectDelays {
		if delay <= 0 {
			t.Fatal("invalid connection wait")
		}
		waits += delay
	}
	if waits != 3750*time.Millisecond || workspaceExchangeTimeout != 35*time.Second {
		t.Fatal("workspace connection budget changed")
	}
}

// TestObserveWorkspaceCancellationStopsConnectionWait cancels after the first refused dial and verifies no detached retry.
// TestObserveWorkspaceCancellationStopsConnectionWait отменяет вызов после первого refused dial и проверяет отсутствие detached retry.
func TestObserveWorkspaceCancellationStopsConnectionWait(t *testing.T) {
	input, observation, _ := workspaceObservationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var dials atomic.Int32
	client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected owner request") }))
	transport := client.http.Transport.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}
	client.http.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	start := time.Now()
	_, err := client.ObserveWorkspace(ctx, input, observation)
	if err == nil || dials.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("cancelled BIND: error=%v, dials=%d", err, dials.Load())
	}
}
