package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentrepo "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/repository/agent"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type repositoryPort interface{ agentrepo.Repository }
type clockPort interface{ agentrepo.Clock }
type idGeneratorPort interface{ agentrepo.IDGenerator }

// Config contains dependencies required by the agent-manager service.
type Config struct {
	Repository                 agentrepo.Repository
	Clock                      agentrepo.Clock
	IDGenerator                agentrepo.IDGenerator
	GuidanceResolver           GuidanceResolver
	WorkspacePolicyResolver    WorkspacePolicyResolver
	RuntimePreparer            RuntimePreparer
	RuntimeJobCreator          RuntimeJobCreator
	RuntimeJobReader           RuntimeJobReader
	ProviderFollowUpDispatcher ProviderFollowUpDispatcher
	HumanGateRequester         HumanGateInteractionRequester
	RuntimePreparationEnabled  bool
	RuntimeJobDispatchEnabled  bool
	HumanGateRequestEnabled    bool
	// EventPublisher is a future outbox-backed publisher for agent domain events.
	EventPublisher EventPublisher
}

// Service is the agent-manager domain entry point.
type Service struct {
	repository repositoryPort
	clock      clockPort

	idGenerator    idGeneratorPort
	eventPublisher EventPublisher

	runtimePreparationEnabled bool
	runtimeJobDispatchEnabled bool
	humanGateRequestEnabled   bool

	guidanceResolver           GuidanceResolver
	workspacePolicyResolver    WorkspacePolicyResolver
	runtimePreparer            RuntimePreparer
	runtimeJobCreator          RuntimeJobCreator
	runtimeJobReader           RuntimeJobReader
	providerFollowUpDispatcher ProviderFollowUpDispatcher
	humanGateRequester         HumanGateInteractionRequester
}

// GuidanceResolver resolves guidance package selections into safe frozen refs.
type GuidanceResolver interface {
	ResolveGuidanceRefs(context.Context, GuidanceResolutionInput) ([]value.GuidanceRef, error)
}

// GuidanceResolutionInput describes package guidance context needed for one run.
type GuidanceResolutionInput struct {
	Meta  value.CommandMeta
	Scope value.ScopeRef
	Hints []value.GuidanceSelectionHint
}

// WorkspacePolicyResolver reads checked project-catalog workspace policy snapshots.
type WorkspacePolicyResolver interface {
	ResolveWorkspacePolicy(context.Context, WorkspacePolicyResolutionInput) (WorkspacePolicySnapshot, error)
}

// WorkspacePolicyResolutionInput selects one checked project workspace policy.
type WorkspacePolicyResolutionInput struct {
	Meta                    value.CommandMeta
	ProjectID               uuid.UUID
	RepositoryIDs           []uuid.UUID
	ServiceKeys             []string
	IncludeGuidancePackages bool
}

// RuntimePreparer calls runtime-manager to reserve a slot and start workspace preparation.
type RuntimePreparer interface {
	PrepareRuntime(context.Context, RuntimePreparationInput) (RuntimePreparationResult, error)
}

// RuntimeJobCreator вызывает runtime-manager для постановки agent run job.
type RuntimeJobCreator interface {
	CreateAgentRunJob(context.Context, RuntimeJobInput) (RuntimeJobResult, error)
}

// RuntimeJobReader читает безопасное состояние runtime job через runtime-manager.
type RuntimeJobReader interface {
	GetAgentRunJob(context.Context, RuntimeJobReadInput) (RuntimeJobReadResult, error)
}

// ProviderFollowUpDispatcher calls provider-hub typed follow-up write operations.
type ProviderFollowUpDispatcher interface {
	CreateIssue(context.Context, ProviderCreateIssueInput) (ProviderCommandResult, error)
	UpdateIssue(context.Context, ProviderUpdateIssueInput) (ProviderCommandResult, error)
	CreateComment(context.Context, ProviderCreateCommentInput) (ProviderCommandResult, error)
	UpdateComment(context.Context, ProviderUpdateCommentInput) (ProviderCommandResult, error)
	UpdatePullRequest(context.Context, ProviderUpdatePullRequestInput) (ProviderCommandResult, error)
	CreateReviewSignal(context.Context, ProviderCreateReviewSignalInput) (ProviderCommandResult, error)
}

// HumanGateInteractionRequester creates owner-visible requests in interaction-hub.
type HumanGateInteractionRequester interface {
	RequestHumanGate(context.Context, HumanGateInteractionRequestInput) (HumanGateInteractionRequestResult, error)
}

// DisabledGuidanceResolver keeps agent-manager runnable before package-hub is wired.
type DisabledGuidanceResolver struct{}

// ResolveGuidanceRefs rejects explicit hints when package-hub resolution is disabled.
func (DisabledGuidanceResolver) ResolveGuidanceRefs(_ context.Context, input GuidanceResolutionInput) ([]value.GuidanceRef, error) {
	if len(input.Hints) > 0 {
		return nil, errs.ErrPreconditionFailed
	}
	return nil, nil
}

// DisabledWorkspacePolicyResolver keeps runtime preparation opt-in at composition time.
type DisabledWorkspacePolicyResolver struct{}

// ResolveWorkspacePolicy reports that workspace policy reads are unavailable.
func (DisabledWorkspacePolicyResolver) ResolveWorkspacePolicy(context.Context, WorkspacePolicyResolutionInput) (WorkspacePolicySnapshot, error) {
	return WorkspacePolicySnapshot{}, errs.ErrDependencyUnavailable
}

// DisabledRuntimePreparer keeps agent-manager runnable before runtime-manager is wired.
type DisabledRuntimePreparer struct{}

// PrepareRuntime reports that runtime preparation is unavailable.
func (DisabledRuntimePreparer) PrepareRuntime(context.Context, RuntimePreparationInput) (RuntimePreparationResult, error) {
	return RuntimePreparationResult{}, errs.ErrDependencyUnavailable
}

// DisabledRuntimeJobCreator оставляет runtime job dispatch явным switch на уровне сборки сервиса.
type DisabledRuntimeJobCreator struct{}

// CreateAgentRunJob сообщает, что постановка runtime job недоступна.
func (DisabledRuntimeJobCreator) CreateAgentRunJob(context.Context, RuntimeJobInput) (RuntimeJobResult, error) {
	return RuntimeJobResult{}, errs.ErrDependencyUnavailable
}

// DisabledRuntimeJobReader оставляет чтение runtime job явным на уровне сборки сервиса.
type DisabledRuntimeJobReader struct{}

// GetAgentRunJob сообщает, что чтение runtime job недоступно.
func (DisabledRuntimeJobReader) GetAgentRunJob(context.Context, RuntimeJobReadInput) (RuntimeJobReadResult, error) {
	return RuntimeJobReadResult{}, errs.ErrDependencyUnavailable
}

// DisabledProviderFollowUpDispatcher keeps follow-up provider dispatch opt-in at composition time.
type DisabledProviderFollowUpDispatcher struct{}

// CreateIssue reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) CreateIssue(context.Context, ProviderCreateIssueInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// UpdateIssue reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) UpdateIssue(context.Context, ProviderUpdateIssueInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// CreateComment reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) CreateComment(context.Context, ProviderCreateCommentInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// UpdateComment reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) UpdateComment(context.Context, ProviderUpdateCommentInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// UpdatePullRequest reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) UpdatePullRequest(context.Context, ProviderUpdatePullRequestInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// CreateReviewSignal reports that provider-hub write operations are unavailable.
func (DisabledProviderFollowUpDispatcher) CreateReviewSignal(context.Context, ProviderCreateReviewSignalInput) (ProviderCommandResult, error) {
	return ProviderCommandResult{}, errs.ErrDependencyUnavailable
}

// DisabledHumanGateInteractionRequester keeps interaction request creation opt-in.
type DisabledHumanGateInteractionRequester struct{}

// RequestHumanGate reports that interaction-hub request creation is unavailable.
func (DisabledHumanGateInteractionRequester) RequestHumanGate(context.Context, HumanGateInteractionRequestInput) (HumanGateInteractionRequestResult, error) {
	return HumanGateInteractionRequestResult{}, errs.ErrDependencyUnavailable
}

// New creates an agent-manager service scaffold.
func New(cfg Config) *Service {
	if cfg.EventPublisher == nil {
		cfg.EventPublisher = DisabledEventPublisher{}
	}
	if cfg.GuidanceResolver == nil {
		cfg.GuidanceResolver = DisabledGuidanceResolver{}
	}
	if cfg.WorkspacePolicyResolver == nil {
		cfg.WorkspacePolicyResolver = DisabledWorkspacePolicyResolver{}
	}
	if cfg.RuntimePreparer == nil {
		cfg.RuntimePreparer = DisabledRuntimePreparer{}
	}
	if cfg.RuntimeJobCreator == nil {
		cfg.RuntimeJobCreator = DisabledRuntimeJobCreator{}
	}
	if cfg.RuntimeJobReader == nil {
		cfg.RuntimeJobReader = DisabledRuntimeJobReader{}
	}
	if cfg.ProviderFollowUpDispatcher == nil {
		cfg.ProviderFollowUpDispatcher = DisabledProviderFollowUpDispatcher{}
	}
	if cfg.HumanGateRequester == nil {
		cfg.HumanGateRequester = DisabledHumanGateInteractionRequester{}
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = zeroIDGenerator{}
	}
	service := &Service{
		repository:  cfg.Repository,
		clock:       cfg.Clock,
		idGenerator: cfg.IDGenerator,
	}
	service.guidanceResolver = cfg.GuidanceResolver
	service.workspacePolicyResolver = cfg.WorkspacePolicyResolver
	service.runtimePreparer = cfg.RuntimePreparer
	service.runtimeJobCreator = cfg.RuntimeJobCreator
	service.runtimeJobReader = cfg.RuntimeJobReader
	service.providerFollowUpDispatcher = cfg.ProviderFollowUpDispatcher
	service.humanGateRequester = cfg.HumanGateRequester
	service.runtimePreparationEnabled = cfg.RuntimePreparationEnabled
	service.runtimeJobDispatchEnabled = cfg.RuntimeJobDispatchEnabled
	service.humanGateRequestEnabled = cfg.HumanGateRequestEnabled
	service.eventPublisher = cfg.EventPublisher
	return service
}

// Ready reports whether the process has the minimal composed dependencies.
func (s *Service) Ready() bool {
	return s != nil && s.eventPublisher != nil
}

// EventPublisher returns the configured event publisher boundary.
func (s *Service) EventPublisher() EventPublisher {
	if s == nil {
		return DisabledEventPublisher{}
	}
	return s.eventPublisher
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type zeroIDGenerator struct{}

func (zeroIDGenerator) New() uuid.UUID {
	return uuid.New()
}
