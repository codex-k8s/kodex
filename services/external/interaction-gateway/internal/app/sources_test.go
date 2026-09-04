package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"google.golang.org/grpc"
)

type listenFunc func(context.Context, *controlplanev1.InteractionSource, mattermost.MessageHandler) error

func (fn listenFunc) Listen(ctx context.Context, source *controlplanev1.InteractionSource, handler mattermost.MessageHandler) error {
	return fn(ctx, source, handler)
}

func testSource(revision string) *controlplanev1.InteractionSource {
	return &controlplanev1.InteractionSource{ConnectionRef: "connection", CredentialMaterializationRef: revision, EnabledCapabilities: []string{"mattermost.inbound"}}
}

func TestSourceReplacementJoinsAllPredecessors(t *testing.T) {
	for _, removeBetween := range []bool{false, true} {
		t.Run(map[bool]string{false: "rapid_revisions", true: "remove_and_readd"}[removeBetween], func(t *testing.T) {
			started := make(chan string, 4)
			releaseOld := make(chan struct{})
			var active, overlap atomic.Int32
			listener := listenFunc(func(ctx context.Context, source *controlplanev1.InteractionSource, _ mattermost.MessageHandler) error {
				if active.Add(1) != 1 {
					overlap.Add(1)
				}
				defer active.Add(-1)
				started <- source.GetCredentialMaterializationRef()
				<-ctx.Done()
				if source.GetCredentialMaterializationRef() == "one" {
					<-releaseOld
				}
				return ctx.Err()
			})
			manager := newSourceManager(nil, listener, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PollInterval: time.Millisecond})
			manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("one")})
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("initial listener not started")
			}
			if removeBetween {
				manager.Reconcile(t.Context(), nil)
			}
			manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("two")})
			manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("three")})
			close(releaseOld)
			select {
			case revision := <-started:
				if revision != "three" {
					t.Errorf("superseded listener started: %s", revision)
				}
			case <-time.After(time.Second):
				t.Error("replacement listener not started")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := manager.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if active.Load() != 0 || overlap.Load() != 0 {
				t.Fatalf("active=%d overlap=%d", active.Load(), overlap.Load())
			}
			manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("four")})
			if len(manager.sources) != 0 {
				t.Fatal("closed manager accepted a new source")
			}
		})
	}
}

func TestSourceUnchangedConfigurationDoesNotRestart(t *testing.T) {
	started := make(chan struct{}, 4)
	listener := listenFunc(func(ctx context.Context, _ *controlplanev1.InteractionSource, _ mattermost.MessageHandler) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	manager := newSourceManager(nil, listener, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PollInterval: time.Millisecond})
	manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("same")})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("listener not started")
	}
	first := manager.sources["connection"].done
	manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("same")})
	if manager.sources["connection"].done != first {
		t.Fatal("unchanged source restarted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type messageControl struct {
	controlplanev1.InteractionWorkServiceClient
	accept func(context.Context, *controlplanev1.AcceptInteractionMessageRequest) (*controlplanev1.AcceptInteractionMessageResponse, error)
}

func (control messageControl) AcceptInteractionMessage(ctx context.Context, request *controlplanev1.AcceptInteractionMessageRequest, _ ...grpc.CallOption) (*controlplanev1.AcceptInteractionMessageResponse, error) {
	return control.accept(ctx, request)
}

func TestSourcePassesVerifiedIdentityAndExactGateTuple(t *testing.T) {
	accepted := make(chan struct{})
	control := messageControl{accept: func(ctx context.Context, request *controlplanev1.AcceptInteractionMessageRequest) (*controlplanev1.AcceptInteractionMessageResponse, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("acceptance RPC has no deadline")
		}
		if request.GetConnectionRef() != "connection" || request.GetExternalTeamRef() != "team" || request.GetExternalChannelRef() != "channel" || request.GetExternalUserDigest() != "verified-digest" || request.GetGateRef() != "gate" || request.GetExpectedGateVersion() != 7 || request.GetRunRef() != "run" || request.GetDecision() != controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE {
			t.Errorf("identity or gate tuple was lost: %v", request)
		}
		if request.GetMutation().GetIdempotencyKey() != stableKey("connection", "event") {
			t.Error("message receipt identity changed")
		}
		return &controlplanev1.AcceptInteractionMessageResponse{Outcome: controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_GATE_RESOLVED, MessageKey: "ACK"}, nil
	}}
	listener := listenFunc(func(ctx context.Context, _ *controlplanev1.InteractionSource, handler mattermost.MessageHandler) error {
		key, err := handler(ctx, mattermost.Message{EventRef: "event", PostRef: "post", RootPostRef: "root", TeamRef: "team", ChannelRef: "channel", UserDigest: "verified-digest", GateRef: "gate", GateVersion: 7, RunRef: "run", Text: "approve", Decision: controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE})
		if err != nil || key != "ACK" {
			t.Errorf("acceptance = %q %v", key, err)
		}
		close(accepted)
		<-ctx.Done()
		return ctx.Err()
	})
	manager := newSourceManager(control, listener, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{RequestTimeout: time.Second, PollInterval: time.Millisecond})
	manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("same")})
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("message was not accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
