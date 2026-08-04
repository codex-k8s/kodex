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
	delivery adminrepo.RuntimeAgentBindingDelivery
	intent   adminrepo.RuntimeAgentBindingIntent
	complete bool
}

func (repository *runtimeBindingTestRepository) EnqueueRuntimeAgentBinding(_ context.Context, intent adminrepo.RuntimeAgentBindingIntent) (adminrepo.RuntimeAgentBindingDelivery, error) {
	repository.intent = intent
	return repository.delivery, nil
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
	request *controlplanev1.BindRuntimeAgentSessionRequest
}

func (client *runtimeBindingTestClient) BindRuntimeAgentSession(_ context.Context, request *controlplanev1.BindRuntimeAgentSessionRequest, _ ...grpc.CallOption) (*controlplanev1.BindRuntimeAgentSessionResponse, error) {
	client.request = request
	return &controlplanev1.BindRuntimeAgentSessionResponse{
		SessionId: request.GetSessionId(), SessionVersion: request.GetExpectedSessionVersion(),
		TurnId: request.GetTurnId(), TurnVersion: request.GetExpectedTurnVersion(),
		AgentSessionBindingSha256: strings.Repeat("a", 64), AgentTurnBindingSha256: strings.Repeat("b", 64),
	}, nil
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
		RuntimeRevisionSHA256: delivery.RuntimeRevisionSHA256, AgentRunID: delivery.AgentRunID,
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
