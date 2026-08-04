package service

import (
	"context"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"google.golang.org/grpc"
)

type runtimeBindingTestRepository struct {
	delivery                  adminrepo.RuntimeAgentBindingDelivery
	discovery                 adminrepo.RuntimeAgentBindingDiscovery
	intent                    adminrepo.RuntimeAgentBindingIntent
	complete                  bool
	discoveryCompleteFailures int
	enqueueCount              int
}

func (repository *runtimeBindingTestRepository) EnqueueRuntimeAgentBinding(_ context.Context, intent adminrepo.RuntimeAgentBindingIntent) (adminrepo.RuntimeAgentBindingDelivery, error) {
	repository.intent = intent
	repository.enqueueCount++
	return repository.delivery, nil
}

func (repository *runtimeBindingTestRepository) ClaimRuntimeAgentBindingDiscovery(context.Context, string, time.Time) (adminrepo.RuntimeAgentBindingDiscovery, error) {
	if repository.discovery.ID == 0 {
		return adminrepo.RuntimeAgentBindingDiscovery{}, adminrepo.ErrNotFound
	}
	return repository.discovery, nil
}

func (repository *runtimeBindingTestRepository) CompleteRuntimeAgentBindingDiscovery(context.Context, int64, string) error {
	if repository.discoveryCompleteFailures > 0 {
		repository.discoveryCompleteFailures--
		return adminrepo.ErrRuntimeAgentBindingConflict
	}
	return nil
}

func (*runtimeBindingTestRepository) RetryRuntimeAgentBindingDiscovery(context.Context, int64, string, time.Time, string) error {
	return nil
}

func (repository *runtimeBindingTestRepository) ClaimRuntimeAgentBinding(context.Context, string, time.Time) (adminrepo.RuntimeAgentBindingDelivery, error) {
	return repository.delivery, nil
}

func (repository *runtimeBindingTestRepository) CompleteRuntimeAgentBinding(_ context.Context, id int64, lease, sessionDigest, turnDigest string) error {
	repository.complete = id == repository.delivery.ID && lease == repository.delivery.LeaseToken &&
		len(sessionDigest) == 64 && len(turnDigest) == 64
	return nil
}

func (*runtimeBindingTestRepository) RetryRuntimeAgentBinding(context.Context, int64, string, time.Time, string) error {
	return nil
}

type runtimeBindingTestClient struct {
	request            *controlplanev1.BindRuntimeAgentSessionRequest
	materializeRequest *controlplanev1.MaterializeRuntimeAgentTurnRequest
	materialize        *controlplanev1.MaterializeRuntimeAgentTurnResponse
	materializeCount   int
	intent             *controlplanev1.ResolveRuntimeAgentBindingIntentResponse
}

func (client *runtimeBindingTestClient) MaterializeRuntimeAgentTurn(_ context.Context, request *controlplanev1.MaterializeRuntimeAgentTurnRequest, _ ...grpc.CallOption) (*controlplanev1.MaterializeRuntimeAgentTurnResponse, error) {
	client.materializeRequest = request
	client.materializeCount++
	return client.materialize, nil
}

func (client *runtimeBindingTestClient) ResolveRuntimeAgentBindingIntent(_ context.Context, _ *controlplanev1.ResolveRuntimeAgentBindingIntentRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveRuntimeAgentBindingIntentResponse, error) {
	return client.intent, nil
}

func (client *runtimeBindingTestClient) BindRuntimeAgentSession(_ context.Context, request *controlplanev1.BindRuntimeAgentSessionRequest, _ ...grpc.CallOption) (*controlplanev1.BindRuntimeAgentSessionResponse, error) {
	client.request = request
	return &controlplanev1.BindRuntimeAgentSessionResponse{
		SessionId: request.GetSessionId(), SessionVersion: request.GetExpectedSessionVersion(),
		TurnId: request.GetTurnId(), TurnVersion: request.GetExpectedTurnVersion(),
		AgentSessionBindingSha256: strings.Repeat("a", 64), AgentTurnBindingSha256: strings.Repeat("b", 64),
	}, nil
}

func TestRuntimeAgentBindingDiscoveryRejoinsAfterLostCompletionResponse(t *testing.T) {
	intent := &controlplanev1.MaterializeRuntimeAgentTurnResponse{
		SessionId: "control-session", SessionVersion: 7,
		TurnId: "control-turn", TurnVersion: 8, Attempt: 1,
		InputSha256: strings.Repeat("1", 64), RuntimeRevisionId: "revision",
		RuntimeRevisionVersion: 3, RuntimeRevisionSha256: strings.Repeat("2", 64),
		AgentSessionBindingSha256: strings.Repeat("a", 64),
		AgentTurnBindingSha256:    strings.Repeat("b", 64),
	}
	repository := &runtimeBindingTestRepository{
		discovery: adminrepo.RuntimeAgentBindingDiscovery{
			ID: 71, AgentSessionTurnID: 61, AgentRunID: "bot-run", SourceRef: "mattermost-post",
			LeaseToken: "discovery-lease", AgentSessionID: 51, AgentSessionVersion: 9,
			AgentSessionTurnVersion: 10, AgentSessionKey: "bot-session",
			RoleStableKey: "developer", ExternalChannelRef: "mattermost-channel", PromptText: "Fix issue",
		},
		delivery: adminrepo.RuntimeAgentBindingDelivery{
			ID: 41, AgentSessionID: 51, AgentSessionTurnID: 61,
		},
		discoveryCompleteFailures: 1,
	}
	client := &runtimeBindingTestClient{materialize: intent}
	service := NewRuntimeAgentBindingService(repository, client)
	if worked, err := service.DiscoverOne(t.Context()); !worked || err == nil {
		t.Fatalf("lost completion response was not surfaced: worked=%v err=%v", worked, err)
	}
	if worked, err := service.DiscoverOne(t.Context()); !worked || err != nil {
		t.Fatalf("discovery retry did not rejoin: worked=%v err=%v", worked, err)
	}
	if client.materializeCount != 2 || repository.enqueueCount != 2 ||
		client.materializeRequest.GetAgentRunId() != "bot-run" ||
		client.materializeRequest.GetAgentSessionId() != 51 ||
		client.materializeRequest.GetRoleStableKey() != "developer" {
		t.Fatalf("retry changed exact bot/control tuple: count=%d request=%+v", client.materializeCount, client.materializeRequest)
	}
	if repository.intent.ControlTurnID != intent.GetTurnId() || repository.intent.AgentRunID != "bot-run" ||
		repository.intent.RuntimeRevisionSHA256 != intent.GetRuntimeRevisionSha256() {
		t.Fatalf("materialized owner tuple was not durably enqueued: %+v", repository.intent)
	}
}

func TestRuntimeAgentBindingUsesRepositoryOwnedBotTupleAndExactReplayIntent(t *testing.T) {
	delivery := adminrepo.RuntimeAgentBindingDelivery{
		ID: 41, AgentSessionID: 51, AgentSessionTurnID: 61, LeaseToken: "lease",
		IdempotencyKey: "runtime-binding-idempotency", ControlSessionID: "control-session",
		ControlSessionVersion: 7, ControlTurnID: "control-turn", ControlTurnVersion: 8,
		Attempt: 2, InputSHA256: strings.Repeat("1", 64), RuntimeRevisionID: "revision",
		RuntimeRevisionVersion: 3, RuntimeRevisionSHA256: strings.Repeat("2", 64),
		AgentSessionKey: "bot-session", AgentSessionVersion: 9,
		AgentRunID: "bot-run", AgentSessionTurnVersion: 10,
	}
	repository := &runtimeBindingTestRepository{delivery: delivery}
	client := &runtimeBindingTestClient{}
	service := NewRuntimeAgentBindingService(repository, client)
	registration, err := service.Register(t.Context(), RegisterRuntimeAgentBindingCommand{
		IdempotencyKey: delivery.IdempotencyKey, ControlSessionID: delivery.ControlSessionID,
		ControlSessionVersion: delivery.ControlSessionVersion, ControlTurnID: delivery.ControlTurnID,
		ControlTurnVersion: delivery.ControlTurnVersion, Attempt: delivery.Attempt,
		InputSHA256: delivery.InputSHA256, RuntimeRevisionID: delivery.RuntimeRevisionID,
		RuntimeRevisionVersion: delivery.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  delivery.RuntimeRevisionSHA256, AgentRunID: delivery.AgentRunID,
	})
	if err != nil || registration.AgentSessionID != delivery.AgentSessionID || repository.intent.AgentRunID != delivery.AgentRunID {
		t.Fatalf("register exact bot tuple: registration=%+v intent=%+v err=%v", registration, repository.intent, err)
	}
	worked, err := service.DeliverOne(t.Context())
	if err != nil || !worked || !repository.complete {
		t.Fatalf("deliver exact binding: worked=%v complete=%v err=%v", worked, repository.complete, err)
	}
	if client.request.GetAgentSessionKey() != delivery.AgentSessionKey ||
		client.request.GetAgentSessionId() != delivery.AgentSessionID ||
		client.request.GetAgentSessionTurnId() != delivery.AgentSessionTurnID ||
		client.request.GetAgentRunId() != delivery.AgentRunID ||
		client.request.GetAgentSessionVersion() != delivery.AgentSessionVersion ||
		client.request.GetAgentSessionTurnVersion() != delivery.AgentSessionTurnVersion {
		t.Fatalf("generated client request lost repository-owned tuple: %+v", client.request)
	}
}
