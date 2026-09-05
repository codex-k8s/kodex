package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type listenFunc func(context.Context, *controlplanev1.InteractionSource, mattermost.MessageHandler) error

func (fn listenFunc) Listen(ctx context.Context, source *controlplanev1.InteractionSource, handler mattermost.MessageHandler) error {
	return fn(ctx, source, handler)
}

func testSource(revision string) *controlplanev1.InteractionSource {
	return &controlplanev1.InteractionSource{ConnectionRef: "connection", CredentialMaterializationRef: revision, EnabledCapabilities: []string{"mattermost.inbound"}}
}

type discoveryControl struct {
	controlplanev1.InteractionWorkServiceClient
	started <-chan struct{}
}

func (control discoveryControl) ListInteractionSources(ctx context.Context, _ *controlplanev1.ListInteractionSourcesRequest, _ ...grpc.CallOption) (*controlplanev1.ListInteractionSourcesResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-control.started:
		return nil, status.Error(codes.Unavailable, "synthetic owner outage")
	}
}

func TestSourceDiscoveryFailureCancelsPreviousSubscription(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	listener := listenFunc(func(ctx context.Context, _ *controlplanev1.InteractionSource, _ mattermost.MessageHandler) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	config := Config{RequestTimeout: time.Second, SourceRefreshInterval: time.Hour}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	control := discoveryControl{started: started}
	manager := newSourceManager(control, listener, logger, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	manager.Reconcile(ctx, []*controlplanev1.InteractionSource{testSource("same")})
	finished := make(chan error, 1)
	go func() {
		finished <- runSourceRefresh(manager, &controlplaneclient.Client{Interaction: control}, logger, config)(ctx)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("unconfirmed source kept external subscription")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("source discovery did not stop")
	}
	shutdown, stopShutdown := context.WithTimeout(t.Context(), time.Second)
	defer stopShutdown()
	if err := manager.Close(shutdown); err != nil {
		t.Fatal(err)
	}
}

func TestSourceFingerprintIncludesImmutableConnectionAndCredential(t *testing.T) {
	source := testSource("same")
	source.ConnectionVersion = 1
	source.CredentialRevisionRef = "credential"
	source.CredentialRevision = 1
	source.CredentialDescriptor = &controlplanev1.IntegrationCredentialRevision{Ref: "credential", Revision: 1, SecretRef: "secret#key", SecretUid: "uid", SecretResourceVersion: "1", ContentSha256: "digest"}
	fingerprint := sourceFingerprint(source)
	for _, mutate := range []func(*controlplanev1.InteractionSource){
		func(value *controlplanev1.InteractionSource) { value.ConnectionVersion++ },
		func(value *controlplanev1.InteractionSource) { value.CredentialRevision++ },
		func(value *controlplanev1.InteractionSource) { value.CredentialRevisionRef = "other" },
		func(value *controlplanev1.InteractionSource) { value.CredentialDescriptor.SecretResourceVersion = "2" },
		func(value *controlplanev1.InteractionSource) { value.CredentialDescriptor.ContentSha256 = "other" },
	} {
		changed := proto.Clone(source).(*controlplanev1.InteractionSource)
		mutate(changed)
		if sourceFingerprint(changed) == fingerprint {
			t.Fatal("changed connection or credential reused old listener")
		}
	}
	source.EnabledCapabilities = []string{"mattermost.inbound", "mattermost.gate_decisions"}
	fingerprint = sourceFingerprint(source)
	source.EnabledCapabilities[0], source.EnabledCapabilities[1] = source.EnabledCapabilities[1], source.EnabledCapabilities[0]
	if sourceFingerprint(source) != fingerprint {
		t.Fatal("capability order restarted unchanged listener")
	}
}

func TestRejectedSenderDoesNotDisconnectChannel(t *testing.T) {
	for _, code := range []codes.Code{codes.InvalidArgument, codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition, codes.Unauthenticated, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			calls := 0
			control := messageControl{accept: func(context.Context, *controlplanev1.AcceptInteractionMessageRequest) (*controlplanev1.AcceptInteractionMessageResponse, error) {
				calls++
				if calls == 1 {
					return nil, status.Error(code, "synthetic rejection")
				}
				return &controlplanev1.AcceptInteractionMessageResponse{Outcome: controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_RUN_STARTED}, nil
			}}
			finished := make(chan error, 1)
			listener := listenFunc(func(ctx context.Context, _ *controlplanev1.InteractionSource, handler mattermost.MessageHandler) error {
				err := handler(ctx, mattermost.Message{EventRef: "rejected"})
				if err == nil {
					err = handler(ctx, mattermost.Message{EventRef: "accepted"})
				}
				finished <- err
				<-ctx.Done()
				return ctx.Err()
			})
			manager := newSourceManager(control, listener, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{RequestTimeout: time.Second, PollInterval: time.Millisecond})
			manager.Reconcile(t.Context(), []*controlplanev1.InteractionSource{testSource("same")})
			select {
			case err := <-finished:
				unavailable := code == codes.Unauthenticated || code == codes.Unavailable
				if (err != nil) != unavailable || !unavailable && calls != 2 {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			case <-time.After(time.Second):
				t.Fatal("channel handler did not finish")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := manager.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
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
		err := handler(ctx, mattermost.Message{EventRef: "event", PostRef: "post", RootPostRef: "root", TeamRef: "team", ChannelRef: "channel", UserDigest: "verified-digest", GateRef: "gate", GateVersion: 7, RunRef: "run", Text: "approve", Decision: controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE})
		if err != nil {
			t.Errorf("acceptance = %v", err)
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
