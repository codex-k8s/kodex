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

// listenFunc adapts a fixture callback to a source subscription without opening a remote stream.
// RU: listenFunc превращает обработчик оснастки в подписку источника без открытия удалённого потока.
type listenFunc func(context.Context, *controlplanev1.InteractionSource, mattermost.MessageHandler) error

// Listen passes source identity, lifecycle context and the real acceptance handler to the test's subscription callback.
// RU: Listen передаёт идентичность источника, контекст жизни и настоящий обработчик приёма тестовой подписке.
func (fn listenFunc) Listen(ctx context.Context, source *controlplanev1.InteractionSource, handler mattermost.MessageHandler) error {
	return fn(ctx, source, handler)
}

// testSource creates a synthetic inbound-enabled connection snapshot with a caller-selected credential materialization revision.
// RU: testSource создаёт синтетический снимок подключения с входящей capability и выбранной тестом ревизией материализации реквизитов.
func testSource(revision string) *controlplanev1.InteractionSource {
	return &controlplanev1.InteractionSource{ConnectionRef: "connection", CredentialMaterializationRef: revision, EnabledCapabilities: []string{"mattermost.inbound"}}
}

// discoveryControl delays a synthetic discovery outage until an existing listener has started, making cancellation observable.
// RU: discoveryControl задерживает синтетический отказ обнаружения до запуска существующего listener, делая отмену наблюдаемой.
type discoveryControl struct {
	controlplanev1.InteractionWorkServiceClient
	started <-chan struct{}
}

// ListInteractionSources returns context cancellation or a controlled owner outage after the subscription-start barrier.
// RU: ListInteractionSources возвращает отмену контекста либо управляемый отказ владельца после барьера начала подписки.
func (control discoveryControl) ListInteractionSources(ctx context.Context, _ *controlplanev1.ListInteractionSourcesRequest, _ ...grpc.CallOption) (*controlplanev1.ListInteractionSourcesResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-control.started:
		return nil, status.Error(codes.Unavailable, "synthetic owner outage")
	}
}

// TestSourceDiscoveryFailureCancelsPreviousSubscription proves an unconfirmed owner snapshot revokes the old listener and that refresh and close join cleanly.
// RU: TestSourceDiscoveryFailureCancelsPreviousSubscription доказывает отзыв старого listener при неподтверждённом снимке владельца и завершение refresh и close.
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

// TestSourceFingerprintIncludesImmutableConnectionAndCredential distinguishes every tested connection/credential pin while ignoring capability order.
// RU: TestSourceFingerprintIncludesImmutableConnectionAndCredential различает все проверяемые координаты подключения и реквизитов, игнорируя порядок capabilities.
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

// TestRejectedSenderDoesNotDisconnectChannel keeps defined per-message rejections local but propagates authentication and availability failures.
// RU: TestRejectedSenderDoesNotDisconnectChannel оставляет заданные отказы отдельному сообщению локальными, но передаёт ошибки аутентификации и доступности.
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

// TestSourceReplacementJoinsAllPredecessors exercises rapid replacement and remove/readd, proving no overlapping listeners or starts after closure.
// RU: TestSourceReplacementJoinsAllPredecessors проверяет быструю замену и удаление с повторным добавлением, доказывая отсутствие перекрытия listener и запусков после закрытия.
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

// TestSourceUnchangedConfigurationDoesNotRestart preserves the existing listener completion signal when the exact source snapshot is unchanged.
// RU: TestSourceUnchangedConfigurationDoesNotRestart сохраняет сигнал завершения существующего listener при неизменном точном снимке источника.
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

// messageControl captures only source message acceptance so tests can inspect trusted identity and exact gate coordinates.
// RU: messageControl перехватывает только приём сообщения источника для проверки доверенной идентичности и точных координат gate.
type messageControl struct {
	controlplanev1.InteractionWorkServiceClient
	accept func(context.Context, *controlplanev1.AcceptInteractionMessageRequest) (*controlplanev1.AcceptInteractionMessageResponse, error)
}

// AcceptInteractionMessage forwards the unchanged acceptance request to the synthetic owner callback without resolving a real gate.
// RU: AcceptInteractionMessage передаёт неизменный запрос приёма синтетическому обработчику владельца без разрешения реального gate.
func (control messageControl) AcceptInteractionMessage(ctx context.Context, request *controlplanev1.AcceptInteractionMessageRequest, _ ...grpc.CallOption) (*controlplanev1.AcceptInteractionMessageResponse, error) {
	return control.accept(ctx, request)
}

// TestSourcePassesVerifiedIdentityAndExactGateTuple checks bounded forwarding of adapter identity, gate/version/run pins and a connection-scoped event key.
// RU: TestSourcePassesVerifiedIdentityAndExactGateTuple проверяет ограниченную передачу идентичности адаптера, координат gate/version/run и ключа события в области подключения.
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
