package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

var runtimeBindingSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type RegisterRuntimeAgentBindingCommand struct {
	IdempotencyKey         string `json:"idempotency_key"`
	ControlSessionID       string `json:"control_session_id"`
	ControlSessionVersion  uint64 `json:"control_session_version"`
	ControlTurnID          string `json:"control_turn_id"`
	ControlTurnVersion     uint64 `json:"control_turn_version"`
	Attempt                uint32 `json:"attempt"`
	InputSHA256            string `json:"input_sha256"`
	RuntimeRevisionID      string `json:"runtime_revision_id"`
	RuntimeRevisionVersion uint64 `json:"runtime_revision_version"`
	RuntimeRevisionSHA256  string `json:"runtime_revision_sha256"`
	AgentRunID             string `json:"agent_run_id"`
}

type RuntimeAgentBindingRegistration struct {
	DeliveryID         int64  `json:"delivery_id"`
	AgentSessionID     int64  `json:"agent_session_id"`
	AgentSessionTurnID int64  `json:"agent_session_turn_id"`
	State              string `json:"state"`
}

type runtimeAgentBindingClient interface {
	ResolveRuntimeAgentBindingIntent(context.Context, *controlplanev1.ResolveRuntimeAgentBindingIntentRequest, ...grpc.CallOption) (*controlplanev1.ResolveRuntimeAgentBindingIntentResponse, error)
	BindRuntimeAgentSession(context.Context, *controlplanev1.BindRuntimeAgentSessionRequest, ...grpc.CallOption) (*controlplanev1.BindRuntimeAgentSessionResponse, error)
}

type RuntimeAgentBindingService struct {
	repository adminrepo.RuntimeAgentBindingOutboxRepository
	client     runtimeAgentBindingClient
}

func NewRuntimeAgentBindingService(
	repository adminrepo.RuntimeAgentBindingOutboxRepository,
	client runtimeAgentBindingClient,
) *RuntimeAgentBindingService {
	return &RuntimeAgentBindingService{repository: repository, client: client}
}

// Register разрешает bot-owned ID и версии только из локального RunID. Поля
// control-plane служат precondition для owner RPC и не назначают bot authority.
func (service *RuntimeAgentBindingService) Register(
	ctx context.Context,
	command RegisterRuntimeAgentBindingCommand,
) (RuntimeAgentBindingRegistration, error) {
	if service == nil || service.repository == nil || service.client == nil ||
		!validRuntimeBindingIdentifier(command.IdempotencyKey, 16, 256) ||
		!validRuntimeBindingIdentifier(command.ControlSessionID, 1, 256) ||
		!validRuntimeBindingIdentifier(command.ControlTurnID, 1, 256) ||
		!validRuntimeBindingIdentifier(command.RuntimeRevisionID, 1, 256) ||
		!validRuntimeBindingIdentifier(command.AgentRunID, 1, 256) ||
		command.ControlSessionVersion == 0 || command.ControlTurnVersion == 0 ||
		command.RuntimeRevisionVersion == 0 || command.Attempt == 0 || command.Attempt > 100 ||
		!runtimeBindingSHA256Pattern.MatchString(command.InputSHA256) ||
		!runtimeBindingSHA256Pattern.MatchString(command.RuntimeRevisionSHA256) {
		return RuntimeAgentBindingRegistration{}, errors.New("runtime agent binding command is invalid")
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return RuntimeAgentBindingRegistration{}, errors.New("encode runtime agent binding command")
	}
	digest := sha256.Sum256(encoded)
	delivery, err := service.repository.EnqueueRuntimeAgentBinding(ctx, adminrepo.RuntimeAgentBindingIntent{
		IdempotencyKey: command.IdempotencyKey, RequestSHA256: hex.EncodeToString(digest[:]),
		ControlSessionID: command.ControlSessionID, ControlSessionVersion: command.ControlSessionVersion,
		ControlTurnID: command.ControlTurnID, ControlTurnVersion: command.ControlTurnVersion,
		Attempt: command.Attempt, InputSHA256: command.InputSHA256,
		RuntimeRevisionID:      command.RuntimeRevisionID,
		RuntimeRevisionVersion: command.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  command.RuntimeRevisionSHA256, AgentRunID: command.AgentRunID,
	})
	if err != nil {
		return RuntimeAgentBindingRegistration{}, err
	}
	return RuntimeAgentBindingRegistration{
		DeliveryID: delivery.ID, AgentSessionID: delivery.AgentSessionID,
		AgentSessionTurnID: delivery.AgentSessionTurnID, State: "pending",
	}, nil
}

func (service *RuntimeAgentBindingService) DeliverOne(ctx context.Context) (bool, error) {
	if service == nil || service.repository == nil || service.client == nil {
		return false, errors.New("runtime agent binding service is not configured")
	}
	leaseToken := uuid.NewString()
	delivery, err := service.repository.ClaimRuntimeAgentBinding(ctx, leaseToken, time.Now().Add(30*time.Second))
	if errors.Is(err, adminrepo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	response, callErr := service.client.BindRuntimeAgentSession(ctx, &controlplanev1.BindRuntimeAgentSessionRequest{
		IdempotencyKey: delivery.IdempotencyKey,
		SessionId:      delivery.ControlSessionID, ExpectedSessionVersion: delivery.ControlSessionVersion,
		TurnId: delivery.ControlTurnID, ExpectedTurnVersion: delivery.ControlTurnVersion,
		ExpectedAttempt: delivery.Attempt, ExpectedInputSha256: delivery.InputSHA256,
		RuntimeRevisionId:      delivery.RuntimeRevisionID,
		RuntimeRevisionVersion: delivery.RuntimeRevisionVersion,
		RuntimeRevisionSha256:  delivery.RuntimeRevisionSHA256,
		AgentSessionKey:        delivery.AgentSessionKey, AgentSessionId: delivery.AgentSessionID,
		AgentSessionVersion: delivery.AgentSessionVersion,
		AgentSessionTurnId:  delivery.AgentSessionTurnID, AgentRunId: delivery.AgentRunID,
		AgentSessionTurnVersion: delivery.AgentSessionTurnVersion,
	})
	if callErr != nil {
		return true, service.retry(ctx, delivery, "rpc_unavailable")
	}
	if response.GetSessionId() != delivery.ControlSessionID ||
		response.GetTurnId() != delivery.ControlTurnID ||
		response.GetSessionVersion() != delivery.ControlSessionVersion ||
		response.GetTurnVersion() != delivery.ControlTurnVersion ||
		!runtimeBindingSHA256Pattern.MatchString(response.GetAgentSessionBindingSha256()) ||
		!runtimeBindingSHA256Pattern.MatchString(response.GetAgentTurnBindingSha256()) {
		return true, service.retry(ctx, delivery, "response_binding_mismatch")
	}
	if err := service.repository.CompleteRuntimeAgentBinding(ctx, delivery.ID, delivery.LeaseToken,
		response.GetAgentSessionBindingSha256(), response.GetAgentTurnBindingSha256()); err != nil {
		return true, err
	}
	return true, nil
}

// DiscoverOne связывает фактически созданный bot turn с exact control-plane
// owner tuple до claim. Потерянный ответ безопасно повторяет тот же intent.
func (service *RuntimeAgentBindingService) DiscoverOne(ctx context.Context) (bool, error) {
	if service == nil || service.repository == nil || service.client == nil {
		return false, errors.New("runtime agent binding service is not configured")
	}
	leaseToken := uuid.NewString()
	discovery, err := service.repository.ClaimRuntimeAgentBindingDiscovery(
		ctx, leaseToken, time.Now().Add(30*time.Second),
	)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	intent, callErr := service.client.ResolveRuntimeAgentBindingIntent(ctx,
		&controlplanev1.ResolveRuntimeAgentBindingIntentRequest{SourceRef: discovery.SourceRef})
	if callErr != nil {
		return true, service.repository.RetryRuntimeAgentBindingDiscovery(
			ctx, discovery.ID, discovery.LeaseToken, time.Now().Add(5*time.Second),
			"owner_intent_unavailable",
		)
	}
	idempotencyKey := "runtime-binding:" + intent.GetTurnId() + ":" +
		strconv.FormatUint(uint64(intent.GetAttempt()), 10)
	_, err = service.Register(ctx, RegisterRuntimeAgentBindingCommand{
		IdempotencyKey:   idempotencyKey,
		ControlSessionID: intent.GetSessionId(), ControlSessionVersion: intent.GetSessionVersion(),
		ControlTurnID: intent.GetTurnId(), ControlTurnVersion: intent.GetTurnVersion(),
		Attempt: intent.GetAttempt(), InputSHA256: intent.GetInputSha256(),
		RuntimeRevisionID:      intent.GetRuntimeRevisionId(),
		RuntimeRevisionVersion: intent.GetRuntimeRevisionVersion(),
		RuntimeRevisionSHA256:  intent.GetRuntimeRevisionSha256(),
		AgentRunID:             discovery.AgentRunID,
	})
	if err != nil {
		return true, service.repository.RetryRuntimeAgentBindingDiscovery(
			ctx, discovery.ID, discovery.LeaseToken, time.Now().Add(5*time.Second),
			"binding_intent_conflict",
		)
	}
	if err := service.repository.CompleteRuntimeAgentBindingDiscovery(
		ctx, discovery.ID, discovery.LeaseToken,
	); err != nil {
		return true, err
	}
	return true, nil
}

func (service *RuntimeAgentBindingService) retry(
	ctx context.Context,
	delivery adminrepo.RuntimeAgentBindingDelivery,
	errorCode string,
) error {
	return service.repository.RetryRuntimeAgentBinding(
		ctx, delivery.ID, delivery.LeaseToken, time.Now().Add(5*time.Second), errorCode,
	)
}

func (service *RuntimeAgentBindingService) Run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for index := 0; index < 32; index++ {
				worked, err := service.DiscoverOne(ctx)
				if err != nil {
					if logger != nil {
						logger.Error("runtime agent binding discovery failed", "error", err)
					}
					break
				}
				if !worked {
					break
				}
			}
			for index := 0; index < 32; index++ {
				worked, err := service.DeliverOne(ctx)
				if err != nil {
					if logger != nil {
						logger.Error("runtime agent binding delivery failed", "error", err)
					}
					break
				}
				if !worked {
					break
				}
			}
		}
	}
}

func validRuntimeBindingIdentifier(value string, minimum, maximum int) bool {
	return len(value) >= minimum && len(value) <= maximum && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}
