// Package platform определяет transport-neutral порт хранилища control-plane.
package platform

import (
	"context"
	"io"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type BootstrapState struct {
	Bootstrapped, OnboardingCompleted bool
	OrganizationRef                   string
	Assistant                         entity.SystemAssistant
	ProjectCount                      int32
	Actor                             entity.User
	PlatformRole                      string
	NextActions                       []string
}

type Overview struct {
	ProjectCount, AgentCount, ActiveRunCount, PendingGateCount int32
	ActiveRuns                                                 []entity.Run
	PendingGates                                               []entity.OwnerGate
	RecentArtifacts                                            []entity.Artifact
}

type Administration struct {
	Profile, CoreSummary string
	CoreReady            bool
	Assistant            entity.SystemAssistant
	OptionalAdapters     []entity.IntegrationDefinition
	Incidents            []entity.Incident
	ObservedAt           time.Time
}

const MaximumArtifactBytes int64 = 16 << 20

type ArtifactUpload struct {
	ProjectRef, RunRef, FileName, MediaType, Digest string
	ScanState, PreviewState                         string
	SizeBytes                                       int64
	Reader                                          io.Reader
}

type ArtifactDownload struct {
	Artifact entity.Artifact
	Reader   io.ReadCloser
	GrantRef string
}

// ProofPrincipalInput содержит только проверенный credential subject либо
// стабильную system identity. Ни один идентификатор из browser payload не
// является authority без повторного разрешения в PostgreSQL.
type ProofPrincipalInput struct {
	ExternalActorID     string
	ExternalTenantID    string
	ExternalDisplayName string
	ExternalEmailHint   string
	CallerWorkload      string
	Operation           string
	ProjectRef          string
}

// ProofAuthority — внутренние UUID, которые допускаются wire-контрактом
// internal-rpc-authority. Opaque refs остаются locator и не попадают в claims.
type ProofAuthority struct {
	ActorID, OrganizationID, ProjectID string
	ActorVersion, OrganizationVersion  uint64
	ProjectVersion                     uint64
}

type WorkerGrantInput struct {
	WorkloadID string
	Revision   uint64
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

type Repository interface {
	Bootstrap(context.Context) error
	ResolveProofAuthority(context.Context, ProofPrincipalInput) (ProofAuthority, error)
	AcceptWorkerGrant(context.Context, WorkerGrantInput) error
	NextAuthorityProofRevision(context.Context) (uint64, error)
	ResolvePrincipal(context.Context, value.Principal) (value.Principal, error)
	GetBootstrapState(context.Context, value.Principal) (BootstrapState, error)
	GetPlatformEventCursor(context.Context, value.Principal) (string, int64, error)
	GetOverview(context.Context, value.Principal, string) (Overview, error)
	ListCapabilities(context.Context, value.Principal) ([]entity.IntegrationCapability, error)
	ListRuntimes(context.Context, value.Principal) ([]entity.RuntimeSelection, error)
	Search(context.Context, value.Principal, query.Filter) ([]entity.SearchResult, error)
	ListProjects(context.Context, value.Principal, query.Filter) ([]entity.Project, string, []string, error)
	GetProject(context.Context, value.Principal, string) (entity.Project, error)
	ListPlatformMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	ListPlatformMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	ListMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	ListMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	ListAgents(context.Context, value.Principal, query.Filter) ([]entity.Agent, string, error)
	GetAgent(context.Context, value.Principal, string) (entity.Agent, error)
	ListWorkflows(context.Context, value.Principal, query.Filter) ([]entity.Workflow, string, error)
	GetWorkflow(context.Context, value.Principal, string) (entity.Workflow, error)
	ListRuns(context.Context, value.Principal, query.Filter) ([]entity.Run, string, error)
	GetRun(context.Context, value.Principal, string) (entity.Run, error)
	GetRunGraph(context.Context, value.Principal, string) (entity.Run, entity.RunGraph, error)
	ListRunEvents(context.Context, value.Principal, query.Filter) ([]entity.RunEvent, int64, bool, error)
	ListOwnerGates(context.Context, value.Principal, query.Filter) ([]entity.OwnerGate, string, error)
	GetOwnerGate(context.Context, value.Principal, string) (entity.OwnerGate, error)
	ListArtifacts(context.Context, value.Principal, query.Filter) ([]entity.Artifact, string, error)
	GetArtifact(context.Context, value.Principal, string) (entity.Artifact, error)
	UploadArtifact(context.Context, value.Principal, value.Mutation, ArtifactUpload) (entity.Artifact, error)
	DownloadArtifact(context.Context, value.Principal, string, string) (ArtifactDownload, error)
	ReadExecutionArtifact(context.Context, value.Principal, string, string, int64, string) (ArtifactDownload, error)
	ListSchedules(context.Context, value.Principal, query.Filter) ([]entity.Schedule, string, error)
	GetSchedule(context.Context, value.Principal, string) (entity.Schedule, error)
	ListIntegrationDefinitions(context.Context, value.Principal, string) ([]entity.IntegrationDefinition, []string, error)
	ListIntegrationConnections(context.Context, value.Principal, query.Filter) ([]entity.IntegrationConnection, string, error)
	GetIntegrationConnection(context.Context, value.Principal, string) (entity.IntegrationConnection, error)
	GetSystemAssistant(context.Context, value.Principal) (entity.SystemAssistant, error)
	ListAssistantConversations(context.Context, value.Principal, query.Filter) ([]entity.AssistantConversation, string, error)
	GetAdministration(context.Context, value.Principal) (Administration, error)
	ListAuditEvents(context.Context, value.Principal, query.Filter) ([]entity.AuditEvent, string, error)
	Execute(context.Context, command.Command) (command.Result, error)
	ReconcileWarmRuntime(context.Context, value.Principal, string) (entity.SystemAssistant, map[string]any, bool, error)
	ReportWarmRuntime(context.Context, value.Principal, command.WarmRuntimeInput) (entity.SystemAssistant, error)
	ClaimDueSchedules(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	ClaimIntegrationConnectionTests(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	ResolveIntegrationInvocation(context.Context, value.Principal, map[string]string, map[string]any) (map[string]any, error)
	ClaimIntegrationInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	GetIntegrationInvocation(context.Context, value.Principal, string) (map[string]any, error)
	ListInteractionSources(context.Context, value.Principal) ([]map[string]any, error)
	ClaimInteractionDeliveries(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	Ready(context.Context) error
}
