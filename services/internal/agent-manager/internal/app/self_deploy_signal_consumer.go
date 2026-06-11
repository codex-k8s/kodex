package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	selfDeploySignalSourceService = "provider-hub"
	selfDeploySignalConsumerActor = "provider-hub"
)

type selfDeploySignalReader interface {
	GetSelfDeploySignal(context.Context, agentservice.SelfDeploySignalLookupInput) (agentservice.SelfDeploySignalReadResult, error)
}

type selfDeployPlanCreator interface {
	CreateSelfDeployPlanFromSignal(context.Context, agentservice.CreateSelfDeployPlanFromSignalInput) (entity.SelfDeployPlan, error)
}

func startSelfDeploySignalConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	reader selfDeploySignalReader,
	creator selfDeployPlanCreator,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	starter := selfDeploySignalConsumerStarter{
		cfg:          cfg,
		eventLogPool: eventLogPool,
		reader:       reader,
		creator:      creator,
		logger:       logger,
		errCh:        errCh,
	}
	return starter.start(ctx)
}

type selfDeploySignalConsumerStarter struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	reader       selfDeploySignalReader
	creator      selfDeployPlanCreator
	logger       *slog.Logger
	errCh        chan<- error
}

func (s selfDeploySignalConsumerStarter) start(ctx context.Context) error {
	return startManagedEventConsumerWithParts(ctx, s.cfg.SelfDeploySignalConsumerEnabled, s.logger, s.errCh, s.validate, s.runner, runSelfDeploySignalConsumer)
}

func (s selfDeploySignalConsumerStarter) validate() error {
	switch {
	case s.reader == nil:
		return fmt.Errorf("agent-manager self-deploy signal consumer requires project-catalog reader")
	case s.creator == nil:
		return fmt.Errorf("agent-manager self-deploy signal consumer requires self-deploy plan creator")
	case s.eventLogPool == nil:
		return fmt.Errorf("agent-manager self-deploy signal consumer requires platform event-log database")
	default:
		return nil
	}
}

func (s selfDeploySignalConsumerStarter) runner(logger *slog.Logger) (*eventconsumer.Runner, error) {
	return newEventConsumerRunner(s.eventLogPool, selfDeploySignalConsumerRegistration(s.cfg, s.reader, s.creator), s.cfg.SelfDeploySignalConsumerConfig(), logger)
}

func selfDeploySignalConsumerRegistration(cfg Config, reader selfDeploySignalReader, creator selfDeployPlanCreator) eventconsumer.Registration {
	return eventconsumer.Registration{
		EventType:     providerevents.EventRepositoryChanged,
		SchemaVersion: providerevents.SchemaVersion,
		Handler: selfDeploySignalEventHandler{
			cfg:     cfg,
			reader:  reader,
			creator: creator,
		},
	}
}

func runSelfDeploySignalConsumer(ctx context.Context, runner *eventconsumer.Runner, logger *slog.Logger, errCh chan<- error) {
	logger.Info("agent-manager self-deploy signal consumer starting")
	if err := runner.Run(ctx); err != nil {
		errCh <- err
	}
}

type selfDeploySignalEventHandler struct {
	cfg     Config
	reader  selfDeploySignalReader
	creator selfDeployPlanCreator
}

func (h selfDeploySignalEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.SourceService) != selfDeploySignalSourceService {
		return eventconsumer.Poison("invalid_source_service", "self-deploy signal event source service is not provider-hub")
	}
	if strings.TrimSpace(storedEvent.AggregateType) != providerevents.AggregateRepositoryChangeSignal {
		return eventconsumer.Poison("invalid_aggregate_type", "self-deploy signal aggregate type is not repository_change_signal")
	}
	var payload providerevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "self-deploy signal payload is not valid provider json")
	}
	if !selfDeploySignalEventRelevant(payload, h.cfg.SelfDeploySignalConsumerTargetBranch) {
		return eventconsumer.Ack()
	}
	lookup, result := h.lookupInput(payload)
	if result.Status != "" {
		return result
	}
	enriched, err := h.reader.GetSelfDeploySignal(ctx, lookup)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return selfDeploySignalStaleProviderResult()
		}
		return selfDeploySignalConsumerError(err)
	}
	if enriched.Status == agentservice.SelfDeploySignalStatusNotDeployRelevant {
		return eventconsumer.Ack()
	}
	if enriched.Status == agentservice.SelfDeploySignalStatusProviderSignalNotFound {
		return selfDeploySignalStaleProviderResult()
	}
	if enriched.Status != agentservice.SelfDeploySignalStatusReady {
		return selfDeploySignalNotReadyResult(enriched)
	}
	input, err := selfDeployPlanInputFromSignal(enriched.Signal)
	if err != nil {
		return eventconsumer.Poison("invalid_self_deploy_signal", "project self-deploy signal cannot be converted to plan input")
	}
	if _, err := h.creator.CreateSelfDeployPlanFromSignal(ctx, input); err != nil {
		return selfDeploySignalConsumerError(err)
	}
	return eventconsumer.Ack()
}

func (h selfDeploySignalEventHandler) lookupInput(payload providerevents.Payload) (agentservice.SelfDeploySignalLookupInput, eventconsumer.Result) {
	projectID, err := selfDeploySignalProjectID(payload.ProjectID, h.cfg.SelfDeploySignalConsumerProjectID)
	if err != nil {
		return agentservice.SelfDeploySignalLookupInput{}, eventconsumer.Poison("missing_self_deploy_project_ref", "self-deploy signal has no project ref for project-catalog enrichment")
	}
	repositoryID, err := optionalSelfDeploySignalUUID(payload.RepositoryID, h.cfg.SelfDeploySignalConsumerRepositoryID)
	if err != nil {
		return agentservice.SelfDeploySignalLookupInput{}, eventconsumer.Poison("invalid_self_deploy_repository_ref", "self-deploy signal repository ref is invalid")
	}
	signalID := strings.TrimSpace(payload.RepositoryChangeSignalID)
	signalKey := strings.TrimSpace(payload.SignalKey)
	if signalID == "" && signalKey == "" {
		return agentservice.SelfDeploySignalLookupInput{}, eventconsumer.Poison("missing_provider_signal_ref", "self-deploy signal has no provider signal ref")
	}
	return agentservice.SelfDeploySignalLookupInput{
		Meta:              selfDeploySignalCommandMeta(),
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSignalID:  signalID,
		ProviderSignalKey: signalKey,
	}, eventconsumer.Result{}
}

func selfDeploySignalEventRelevant(payload providerevents.Payload, targetBranch string) bool {
	branch := strings.TrimSpace(payload.BaseBranch)
	if branch == "" {
		branch = branchFromRef(payload.SourceRef)
	}
	target := strings.TrimSpace(targetBranch)
	if target != "" && branch != "" && branch != target {
		return false
	}
	return payload.ServicesPolicyChanged || payload.DeployRelevantChanged
}

func branchFromRef(ref string) string {
	trimmed := strings.TrimSpace(ref)
	prefix := "refs/heads/"
	if strings.HasPrefix(trimmed, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	}
	return trimmed
}

func selfDeploySignalProjectID(eventProjectID string, configuredProjectID string) (uuid.UUID, error) {
	return firstValidUUID(eventProjectID, configuredProjectID)
}

func optionalSelfDeploySignalUUID(eventID string, configuredID string) (*uuid.UUID, error) {
	id, err := firstValidUUID(eventID, configuredID)
	if err != nil {
		if strings.TrimSpace(eventID) == "" && strings.TrimSpace(configuredID) == "" {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

func firstValidUUID(values ...string) (uuid.UUID, error) {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		id, err := uuid.Parse(trimmed)
		if err == nil && id != uuid.Nil {
			return id, nil
		}
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return uuid.Nil, errs.ErrInvalidArgument
}

func selfDeploySignalCommandMeta() value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: "self_deploy_signal:provider_repository_changed",
		Actor:          value.Actor{Type: "service", ID: selfDeploySignalConsumerActor},
	}
}

func selfDeployPlanInputFromSignal(signal agentservice.SelfDeploySignal) (agentservice.CreateSelfDeployPlanFromSignalInput, error) {
	governanceContext, err := selfDeployGovernanceContextFromSignal(signal.GovernanceRequirement)
	if err != nil {
		return agentservice.CreateSelfDeployPlanFromSignalInput{}, err
	}
	input := agentservice.CreateSelfDeployPlanInput{
		Meta: value.CommandMeta{
			IdempotencyKey: selfDeploySignalPlanIdempotencyKey(signal.ProviderSignalRef),
			Actor:          value.Actor{Type: "service", ID: selfDeploySignalConsumerActor},
		},
		Scope: value.ScopeRef{
			Type: string(enum.AgentScopeTypeProject),
			Ref:  strings.TrimSpace(signal.ProjectRef),
		},
		ProjectRef:              strings.TrimSpace(signal.ProjectRef),
		RepositoryRef:           strings.TrimSpace(signal.RepositoryRef),
		ProviderSignalRef:       strings.TrimSpace(signal.ProviderSignalRef),
		ProviderSlug:            strings.TrimSpace(signal.ProviderSlug),
		RepositoryFullName:      strings.TrimSpace(signal.RepositoryFullName),
		ProviderRepositoryID:    strings.TrimSpace(signal.ProviderRepositoryID),
		SourceRef:               strings.TrimSpace(signal.SourceRef),
		MergeCommitSHA:          strings.TrimSpace(signal.MergeCommitSHA),
		ServicesYAMLRef:         strings.TrimSpace(signal.ServicesYAML.Ref),
		ServicesYAMLDigest:      strings.TrimSpace(signal.ServicesYAML.Digest),
		AffectedServiceKeys:     append([]string(nil), signal.AffectedServiceKeys...),
		PathCategories:          append([]enum.SelfDeployPathCategory(nil), signal.PathCategories...),
		ExpectedRuntimeJobTypes: append([]enum.SelfDeployRuntimeJobType(nil), signal.ExpectedRuntimeJobTypes...),
		GovernanceContext:       governanceContext,
		SafeSummary:             strings.TrimSpace(signal.SafeSummary),
	}
	if input.ProviderSignalRef == "" ||
		input.ProjectRef == "" ||
		input.RepositoryRef == "" ||
		input.ProviderSlug == "" ||
		input.RepositoryFullName == "" ||
		input.SourceRef == "" ||
		input.MergeCommitSHA == "" ||
		input.ServicesYAMLDigest == "" ||
		len(input.AffectedServiceKeys) == 0 ||
		len(input.PathCategories) == 0 ||
		len(input.ExpectedRuntimeJobTypes) == 0 {
		return agentservice.CreateSelfDeployPlanFromSignalInput{}, errs.ErrInvalidArgument
	}
	return agentservice.CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input}, nil
}

func selfDeployGovernanceContextFromSignal(requirement agentservice.SelfDeployGovernanceRequirement) (value.GovernanceContextRef, error) {
	riskProfileRef, err := selfDeployGovernancePolicyRef("risk_profile", requirement.RiskProfileRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	gatePolicyRef, err := selfDeployGovernancePolicyRef("gate_policy", requirement.GatePolicyRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	return value.GovernanceContextRef{
		RiskProfileRef: riskProfileRef,
		GatePolicyRef:  gatePolicyRef,
	}, nil
}

func selfDeployGovernancePolicyRef(kind string, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, ":") {
		if !safeSelfDeploySignalRef(value) {
			return "", errs.ErrInvalidArgument
		}
		return value, nil
	}
	if !safeSelfDeploySignalPolicyKey(value) {
		return "", errs.ErrInvalidArgument
	}
	return "governance:" + kind + "/" + value, nil
}

func safeSelfDeploySignalPolicyKey(value string) bool {
	if value == "" || len(value) > 128 || unsafeSelfDeploySignalRef(value) {
		return false
	}
	for index, char := range value {
		if asciiSelfDeploySignalLetter(char) || asciiSelfDeploySignalDigit(char) || char == '_' || char == '-' || char == '.' || char == '/' {
			if index == 0 && char == '/' {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func safeSelfDeploySignalRef(value string) bool {
	if value == "" || len(value) > 512 || unsafeSelfDeploySignalRef(value) {
		return false
	}
	for _, char := range value {
		if asciiSelfDeploySignalLetter(char) || asciiSelfDeploySignalDigit(char) {
			continue
		}
		switch char {
		case '-', '.', '_', ':', '/', '#', '@', '+', '=', ',':
			continue
		default:
			return false
		}
	}
	return true
}

func unsafeSelfDeploySignalRef(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"raw_provider_payload",
		"provider_payload",
		"workspace_file",
		"workspace_files",
		"prompt_text",
		"prompt_template",
		"flow_file",
		"transcript",
		"session_dump",
		"full_yaml",
		"full_diff",
		"secret",
		"token",
		"authorization",
		"stdout",
		"stderr",
		"logs",
		"kubeconfig",
		"-----begin",
		"bearer",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func asciiSelfDeploySignalLetter(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func asciiSelfDeploySignalDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

func selfDeploySignalPlanIdempotencyKey(providerSignalRef string) string {
	return "self_deploy_signal:" + strings.TrimSpace(providerSignalRef)
}

func selfDeploySignalNotReadyResult(result agentservice.SelfDeploySignalReadResult) eventconsumer.Result {
	status := strings.TrimSpace(string(result.Status))
	if status == "" {
		status = "unknown"
	}
	reason := strings.TrimSpace(result.SafeReason)
	if reason == "" {
		reason = status
	}
	return eventconsumer.Retry(fmt.Errorf("self-deploy signal is not ready: %s: %s", status, reason))
}

func selfDeploySignalStaleProviderResult() eventconsumer.Result {
	return eventconsumer.Poison("stale_provider_signal", "provider repository change signal is unavailable and cannot be enriched")
}

func selfDeploySignalConsumerError(err error) eventconsumer.Result {
	return invalidConflictEventConsumerDomainError(
		err,
		eventconsumer.Poison("invalid_self_deploy_signal", "self-deploy signal metadata is invalid"),
		eventconsumer.Poison("conflicting_self_deploy_plan_signal", "self-deploy signal conflicts with stored plan state"),
	)
}
