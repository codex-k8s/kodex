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
	SpeechTranscription               entity.SpeechTranscriptionAvailability
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

const MaximumArtifactBytes int64 = 512 << 20

// ArtifactReader даёт policy и storage повторно прочитать один bounded
// file-backed поток без материализации полного содержимого в памяти.
type ArtifactReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type ArtifactUpload struct {
	ProjectRef, RunRef, FileName, MediaType, Digest string
	ScanState, PreviewState                         string
	SizeBytes                                       int64
	Reader                                          ArtifactReader
}

type AgentAvatarUpload struct {
	ArtifactUpload
	AgentRef        string
	ExpectedVersion int64
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
	ExternalActorID         string
	ExternalTenantID        string
	ExternalDisplayName     string
	ExternalEmailHint       string
	ExternalIssuer          string
	ExternalGroups          []string
	ExternalSessionRevision uint64
	ExternalAuthenticatedAt time.Time
	ExternalACR             string
	ExternalAMR             []string
	OwnerClaim              bool
	CallerWorkload          string
	Operation               string
	ProjectRef              string
}

// ProofAuthority — внутренние UUID, которые допускаются wire-контрактом
// internal-rpc-authority. Opaque refs остаются locator и не попадают в claims.
type ProofAuthority struct {
	ActorID, OrganizationID, ProjectID string
	ActorVersion, OrganizationVersion  uint64
	ProjectVersion                     uint64
}

type WorkerGrantInput struct {
	WorkloadID           string
	CredentialGeneration uint64
	Revision             uint64
	IssuedAt             time.Time
	ExpiresAt            time.Time
}

type RuntimeSecretPrepareInput struct {
	Kind, ProjectRef, SecretRef, Name, Description, ValueType, ExpectedContentSHA256 string
	Mutation                                                                         value.Mutation
}

type RuntimeSecretPrepareResult struct {
	OperationRef, OperationGrant, State, ValueType, FailureCode string
	ExpiresAt                                                   time.Time
	TerminalSecret                                              *entity.RuntimeSecret
}

type RuntimeSecretCompleteInput struct {
	OperationRef, ClaimantID string
	ClaimGeneration          int64
	Materialization          *entity.RuntimeSecretMaterialization
}

type RuntimeSecretConsumeInput struct {
	OperationGrant, ClaimantID string
}

type RuntimeSecretFailInput struct {
	OperationRef, ClaimantID, FailureCode string
	ClaimGeneration                       int64
}

// ProviderCredentialCleanupTask несёт только immutable snapshot, который
// materializer должен удалить по exact descriptor и fencing generation.
type ProviderCredentialCleanupTask struct {
	Ref, AccountRef string
	Attempt         int32
	Generation      int64
	LeaseExpiresAt  time.Time
	Credential      entity.ProviderCredentialDescriptor
}

type ProviderCredentialCleanupResult struct {
	Ref, State, SafeErrorCode, TerminalReceipt string
	RetryScheduled                             bool
}

type RuntimeSecretFailureResult struct {
	OperationRef, State, FailureCode string
}

type RuntimeSecretRecoveryInput struct {
	OperationRef    string
	Materialization entity.RuntimeSecretMaterialization
}

type RuntimeSecretRecoveryResult struct {
	Action, OperationState string
	Secret                 *entity.RuntimeSecret
}

type RuntimeSecretRecoveryPage struct {
	Size  int32
	Token string
}

type CredentialProjectionAuthority struct {
	ActorID, TenantID, ProjectID             string
	SourceDigestSHA256, ProofJTI             string
	CallerWorkloadID, CallerFullMethod       string
	SourceRevision, CallerCredentialRevision uint64
	ExpiresAt                                time.Time
}

type ProviderCredentialBinding struct {
	AccountRef, CredentialRevisionRef            string
	SecretName, SecretUID, SecretResourceVersion string
	ContentSHA256                                string
	CredentialRevision                           int64
}

type RuntimeSecretProjectionBinding struct {
	Name, SecretRef string
	Descriptor      entity.RuntimeSecretRevisionDescriptor
}

type RuntimeCredentialProjectionInput struct {
	Authority                                 CredentialProjectionAuthority
	WorkloadInstance, LeaseRef, Fence         string
	RuntimeRevisionRef, RuntimeRevisionDigest string
	SessionRef, TurnRef, InputDigest          string
	Generation                                int64
	Attempt                                   int32
	ProviderCredential                        ProviderCredentialBinding
	RuntimeSecrets                            []RuntimeSecretProjectionBinding
}

type RuntimeCredentialProjection struct {
	ProviderCredential ProviderCredentialBinding
	RuntimeSecrets     []RuntimeSecretProjectionBinding
	ExpiresAt          time.Time
}

type TranscriptionCredentialProjectionInput struct {
	Authority                    CredentialProjectionAuthority
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ConfigRevision               uint64
	ConfigDigestSHA256           string
}

type TranscriptionCredentialProjection struct {
	ProviderCredential ProviderCredentialBinding
	ExpiresAt          time.Time
}

type Repository interface {
	GetRuntimeSecretDraft(context.Context, value.Principal, string) (entity.RuntimeSecretDraft, error)
	PrepareRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error)
	ConsumeRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error)
	FinishRuntimeSecretDraft(context.Context, value.Principal, RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, error)
	ListRuntimeSecretDraftRecovery(context.Context, value.Principal, query.Page) ([]entity.RuntimeSecretDraftWork, string, error)
	CheckRuntimeSecretDraftWork(context.Context, value.Principal) error
	GetRuntimeRevisionPublicPair(context.Context, value.Principal, string, string) (entity.RuntimeRevisionPublicProjection, *entity.RuntimeRevisionPublicProjection, error)
	GetEmailEffectReceipt(context.Context, value.Principal, string) (entity.EmailEffectReceiptView, error)
	ResolveEmailAuthorization(context.Context, value.Principal, query.EmailAuthorization) (entity.EmailAuthorization, error)
	ResolveEmailReconciliation(context.Context, value.Principal, string, string, string, string) (entity.EmailEffectReceiptView, error)
	GetMemoryRecord(context.Context, value.Principal, string) (entity.KodexMemoryRecord, error)
	GetSkillBundle(context.Context, value.Principal, string) (entity.SkillBundle, error)
	ListSkillBundles(context.Context, value.Principal, query.Filter) ([]entity.SkillBundle, int64, string, error)
	ListSkillBundleRevisions(context.Context, value.Principal, string, query.Page) ([]entity.SkillBundleRevision, int64, string, error)
	ListMemoryRecords(context.Context, value.Principal, query.Filter) ([]entity.KodexMemoryRecord, int64, string, error)
	ListMemoryRecordRevisions(context.Context, value.Principal, string, query.Page) ([]entity.MemoryRecordRevision, int64, string, error)
	GetRuntimeEnvironmentDraft(context.Context, value.Principal, string) (entity.RuntimeEnvironmentDraft, error)
	GetRuntimeSecretImpact(context.Context, value.Principal, string, int64, string, query.Page) (entity.RuntimeSecretImpact, error)
	GetRuntimeEnvironmentImpact(context.Context, value.Principal, string, string, string, query.Page) (entity.RuntimeEnvironmentImpact, error)
	ListInteractionIdentities(context.Context, value.Principal, string, query.Page) ([]entity.InteractionIdentity, string, error)
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
	Search(context.Context, value.Principal, query.Filter) ([]entity.SearchResult, int64, string, error)
	ListVFSNodes(context.Context, value.Principal, query.Filter) ([]entity.VFSNode, int64, string, error)
	SearchVFS(context.Context, value.Principal, query.Filter) ([]entity.VFSNode, int64, string, error)
	ListProjects(context.Context, value.Principal, query.Filter) ([]entity.Project, string, []string, error)
	GetProject(context.Context, value.Principal, string) (entity.Project, error)
	ListPlatformMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	ListPlatformMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	ListMemberships(context.Context, value.Principal, query.Filter) ([]entity.Membership, string, error)
	ListMembershipCandidates(context.Context, value.Principal, query.Filter) ([]entity.User, string, error)
	ListAgents(context.Context, value.Principal, query.Filter) ([]entity.Agent, string, error)
	GetAgent(context.Context, value.Principal, string) (entity.Agent, error)
	GetEffectivePromptTemplate(context.Context, value.Principal, string) (entity.InstructionVersion, error)
	GetPromptMaterializationSnapshot(context.Context, value.Principal, string, string) (entity.PromptMaterializationSnapshot, error)
	GetAgentRuntimeConfiguration(context.Context, value.Principal, string) (entity.AgentRuntimeConfigurationView, error)
	ListAgentRuntimeConfigurations(context.Context, value.Principal, query.Filter) ([]entity.AgentRuntimeConfiguration, string, error)
	ListRuntimeEnvironments(context.Context, value.Principal, query.Filter) ([]entity.RuntimeEnvironmentSet, string, error)
	GetRuntimeEnvironment(context.Context, value.Principal, string) (entity.RuntimeEnvironmentSet, error)
	ListRuntimeEnvironmentVersions(context.Context, value.Principal, query.Filter) ([]entity.RuntimeEnvironmentVersion, string, error)
	GetRuntimeEnvironmentReadiness(context.Context, value.Principal, string) (entity.RuntimeEnvironmentReadiness, error)
	ListRuntimeEnvironmentAgents(context.Context, value.Principal, query.Filter) ([]entity.Agent, string, error)
	ListRuntimeSecrets(context.Context, value.Principal, query.Filter) ([]entity.RuntimeSecret, string, error)
	GetRuntimeSecret(context.Context, value.Principal, string) (entity.RuntimeSecret, error)
	PrepareRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretPrepareInput) (RuntimeSecretPrepareResult, error)
	ListRuntimeSecretRecoveryWork(context.Context, value.Principal, RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error)
	ConsumeRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error)
	CompleteRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretCompleteInput) (entity.RuntimeSecret, error)
	FailRuntimeSecretOperation(context.Context, value.Principal, RuntimeSecretFailInput) (RuntimeSecretFailureResult, error)
	RecoverRuntimeSecretMaterialization(context.Context, value.Principal, RuntimeSecretRecoveryInput) (RuntimeSecretRecoveryResult, error)
	ResolveRuntimeCredentialProjection(context.Context, value.Principal, RuntimeCredentialProjectionInput) (RuntimeCredentialProjection, error)
	ValidateRuntimeCredentialProjection(context.Context, value.Principal, RuntimeCredentialProjectionInput) (bool, error)
	ResolveTranscriptionCredentialProjection(context.Context, value.Principal, TranscriptionCredentialProjectionInput) (TranscriptionCredentialProjection, error)
	ListTemplateVariables(context.Context, value.Principal, query.Filter) ([]entity.TemplateVariable, int64, string, error)
	ListProviderDefinitions(context.Context, value.Principal, query.Filter) ([]entity.ProviderDefinition, string, error)
	ListModelCapabilities(context.Context, value.Principal, string, string, query.Filter) (entity.ModelCatalog, error)
	ListManagedConfigurationHistory(context.Context, value.Principal, string, query.Page) (entity.ManagedConfigurationSet, []entity.ManagedConfigurationRevision, int64, string, error)
	ListManagedConfigurations(context.Context, value.Principal, query.Filter) ([]entity.ManagedConfigurationSet, int64, string, error)
	GetManagedConfigurationImpact(context.Context, value.Principal, string, string, query.Filter) (entity.ManagedConfigurationImpact, error)
	GetEffectiveManagedConfiguration(context.Context, value.Principal, string, string, string) (entity.ManagedConfigurationBindingSnapshot, error)
	GetSystemSTTConfiguration(context.Context, value.Principal) (entity.SystemSTTConfiguration, error)
	ListProviderAccounts(context.Context, value.Principal, query.Filter) ([]entity.ProviderAccount, string, []string, error)
	GetProviderAccount(context.Context, value.Principal, string) (entity.ProviderAccount, error)
	ListRoleImageRecipeRevisions(context.Context, value.Principal, query.Filter) ([]entity.RoleImageRecipeRevision, string, error)
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
	GetArtifactImpact(context.Context, value.Principal, string, string) (entity.ArtifactImpact, error)
	GetAttachmentSet(context.Context, value.Principal, string, query.Page) (entity.AttachmentSet, string, error)
	UploadArtifact(context.Context, value.Principal, value.Mutation, ArtifactUpload) (entity.Artifact, error)
	UploadAgentAvatar(context.Context, value.Principal, value.Mutation, AgentAvatarUpload) (entity.Agent, error)
	CleanupExpiredAgentAvatarUploads(context.Context, int32) error
	DownloadArtifact(context.Context, value.Principal, string, string) (ArtifactDownload, error)
	PurgeArtifact(context.Context, value.Principal, value.Mutation, string, string) (string, error)
	ReadExecutionArtifact(context.Context, value.Principal, string, string, int64, string) (ArtifactDownload, error)
	ListSchedules(context.Context, value.Principal, query.Filter) ([]entity.Schedule, string, error)
	GetSchedule(context.Context, value.Principal, string) (entity.Schedule, error)
	ListScheduleRevisions(context.Context, value.Principal, query.Filter) ([]entity.ScheduleRevision, string, error)
	ListScheduleRuns(context.Context, value.Principal, query.Filter) ([]entity.ScheduleRunOccurrence, string, error)
	ListIntegrationDefinitions(context.Context, value.Principal, query.Filter) ([]entity.IntegrationDefinition, string, []string, error)
	ListIntegrationConnections(context.Context, value.Principal, query.Filter) ([]entity.IntegrationConnection, string, error)
	GetIntegrationConnection(context.Context, value.Principal, string) (entity.IntegrationConnection, error)
	GetSystemAssistant(context.Context, value.Principal) (entity.SystemAssistant, error)
	ListAssistantConversations(context.Context, value.Principal, query.Filter) ([]entity.AssistantConversation, string, error)
	GetAdministration(context.Context, value.Principal) (Administration, error)
	ListAuditEvents(context.Context, value.Principal, query.Filter) ([]entity.AuditEvent, string, error)
	ListPermissionRegistry(context.Context, value.Principal) ([]entity.PermissionDefinition, error)
	ListAccessSubjects(context.Context, value.Principal, query.Filter, string) ([]entity.AccessSubject, string, error)
	ListOIDCGroups(context.Context, value.Principal, query.Filter) ([]entity.OIDCGroup, string, error)
	ListAccessRoles(context.Context, value.Principal, query.Page, bool) ([]entity.AccessRole, string, error)
	ListAccessRoleVersions(context.Context, value.Principal, string, query.Page) (entity.AccessRole, []entity.AccessRoleVersion, string, error)
	ListAccessBindings(context.Context, value.Principal, query.AccessBindingFilter) ([]entity.AccessBinding, string, error)
	QueryEffectiveAccess(context.Context, value.Principal, string, entity.AccessScope, []string, time.Time) (entity.EffectiveAccess, error)
	SimulateAccess(context.Context, value.Principal, command.AccessSimulationInput) (entity.AccessSimulation, error)
	Execute(context.Context, command.Command) (command.Result, error)
	ReconcileWarmRuntime(context.Context, value.Principal, string) (entity.SystemAssistant, map[string]any, bool, error)
	ReportWarmRuntime(context.Context, value.Principal, command.WarmRuntimeInput) (entity.SystemAssistant, error)
	ClaimSessionArchiveTasks(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	RenewSessionArchiveTask(context.Context, value.Principal, command.SessionArchiveTaskInput) (map[string]any, error)
	ClaimProviderCredentialCleanupTasks(context.Context, string, int32) ([]ProviderCredentialCleanupTask, error)
	CompleteProviderCredentialCleanupTask(context.Context, string, string, int64, string) (ProviderCredentialCleanupResult, error)
	FailProviderCredentialCleanupTask(context.Context, string, string, int64, string) (ProviderCredentialCleanupResult, error)
	ClaimDueSchedules(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	RenewScheduleOccurrence(context.Context, value.Principal, command.OccurrenceInput) (map[string]any, error)
	ClaimIntegrationConnectionTests(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	ResolveIntegrationInvocation(context.Context, value.Principal, map[string]string, map[string]any) (map[string]any, error)
	ClaimIntegrationInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	GetIntegrationInvocation(context.Context, value.Principal, string) (map[string]any, error)
	ListInteractionSources(context.Context, value.Principal) ([]map[string]any, error)
	ClaimInteractionDeliveries(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	Ready(context.Context) error
}
