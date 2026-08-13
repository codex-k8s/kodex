// Package resource реализует канонические команды и запросы control-plane.
package resource

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func (service *Service) issueRuntimeWorkloadTicket(execution *RuntimeExecution) error {
	if execution == nil || !validSHA256Text(execution.WorkloadTicketSHA256) {
		return errs.ErrStateConflict
	}
	issuedAt := service.now().UTC().Truncate(time.Microsecond)
	type ticketPayload struct {
		Issuer, Audience, TicketID                                string
		IssuedAt, ExpiresAt                                       time.Time
		ExecutionID, OrganizationID, ProjectID, SessionID, TurnID string
		ScheduleOccurrenceID                                      string
		Attempt                                                   uint32
		RuntimeRevisionID, RuntimeRevisionSHA256                  string
		RuntimeRevisionVersion, Version, Fence, GrantGeneration   uint64
		ImmutableInputSHA256, EffectiveRuntimeSHA256              string
		AgentBindingSHA256, CredentialSnapshotSHA256              string
		WorkloadTicketSHA256, ResourceClass, ClusterAccessProfile string
		CodexDeliveryRecoverySourceExecutionID                    string
		RestoreOperationID, RestoreSourceAuthoritySHA256          string
		RestoreOperationGeneration                                uint64
		State                                                     string
	}
	issue := func(issuer, audience string, privateKey ed25519.PrivateKey) (string, error) {
		payload, marshalErr := json.Marshal(ticketPayload{issuer, audience,
			hashString(execution.ID + "\x00" + fmt.Sprint(execution.Version) + "\x00" + fmt.Sprint(execution.Fence) + "\x00" + audience),
			issuedAt, issuedAt.Add(5 * time.Minute),
			execution.ID, execution.OrganizationID, execution.ProjectID,
			execution.SessionID, execution.TurnID, execution.ScheduleOccurrenceID, execution.Attempt,
			execution.RuntimeRevisionID, execution.RuntimeRevisionSHA256,
			execution.RuntimeRevisionVersion, execution.Version, execution.Fence,
			execution.GrantGeneration, execution.ImmutableInputSHA256,
			execution.EffectiveRuntimeSHA256, execution.AgentBindingSHA256,
			execution.CredentialSnapshotSHA256, execution.WorkloadTicketSHA256,
			execution.ResourceClass, execution.ClusterAccessProfile,
			execution.CodexDeliveryRecoverySourceExecutionID,
			execution.RestoreOperationID, execution.RestoreSourceAuthoritySHA256,
			execution.RestoreOperationGeneration, execution.State})
		if marshalErr != nil {
			return "", marshalErr
		}
		signature := ed25519.Sign(privateKey, payload)
		return base64.RawURLEncoding.EncodeToString(payload) + "." +
			base64.RawURLEncoding.EncodeToString(signature), nil
	}
	var err error
	execution.WorkloadTicket, err = issue("mattercodex-control-plane-workload-admission", "mattercodex-runtime-workload-admission", service.runtimeAdmissionSigningKey)
	execution.ArchiveWorkloadTicket = ""
	execution.RestoreWorkloadTicket = ""
	if err == nil && service.archiveRestoreEnabled {
		execution.ArchiveWorkloadTicket, err = issue("mattercodex-control-plane-s3-archive", "mattercodex-runtime-s3-archive", service.runtimeArchiveSigningKey)
	}
	if err == nil && service.archiveRestoreEnabled {
		execution.RestoreWorkloadTicket, err = issue("mattercodex-control-plane-s3-restore", "mattercodex-runtime-s3-restore", service.runtimeRestoreSigningKey)
	}
	if err != nil {
		return errs.ErrInternal
	}
	return nil
}

const (
	auditKindScheduleOccurrence = "SCHEDULE_OCCURRENCE"
	auditKindTurnLease          = "TURN_LEASE"

	permissionCreate                       = "controlplane.resource.create"
	permissionProjectCreate                = "controlplane.project.create"
	permissionProjectUpdate                = "controlplane.project.update"
	permissionProjectDelete                = "controlplane.project.delete"
	permissionUpdate                       = "controlplane.resource.update"
	permissionTransition                   = "controlplane.resource.transition"
	permissionDelete                       = "controlplane.resource.delete"
	permissionAccessManage                 = "controlplane.access.manage"
	permissionAccessDetach                 = "controlplane.access.detach"
	permissionAccessCopy                   = "controlplane.access.copy"
	permissionRead                         = "controlplane.resource.read"
	permissionList                         = "controlplane.resource.list"
	permissionEnqueueTurn                  = "controlplane.turn.enqueue"
	permissionClaimTurn                    = "controlplane.turn.claim"
	permissionRenewTurn                    = "controlplane.turn.renew"
	permissionCompleteTurn                 = "controlplane.turn.complete"
	permissionRetryTurn                    = "controlplane.turn.retry"
	permissionCancelTurn                   = "controlplane.turn.cancel"
	permissionClaimSchedule                = "controlplane.schedule.claim"
	permissionManageSchedule               = "controlplane.schedule.manage"
	permissionUseScheduleCapability        = "controlplane.schedule.capability.use"
	permissionRecoverSchedule              = "controlplane.schedule.recover"
	permissionStartProcess                 = "controlplane.process.start"
	permissionCancelProcess                = "controlplane.process.cancel"
	permissionCompleteProcess              = "controlplane.process.complete"
	permissionRequestGate                  = "controlplane.owner_gate.request"
	permissionResolveGate                  = "controlplane.owner_gate.resolve"
	permissionRegisterArtifact             = "controlplane.artifact.register"
	permissionScanArtifact                 = "controlplane.artifact.scan"
	permissionManageRoleImageRecipe        = "controlplane.role_image_recipe.manage"
	permissionClaimImageBuild              = "controlplane.image_build.claim"
	permissionRenewImageBuild              = "controlplane.image_build.renew"
	permissionReportImageBuild             = "controlplane.image_build.progress"
	permissionCompleteImageBuild           = "controlplane.image_build.complete"
	permissionFailImageBuild               = "controlplane.image_build.fail"
	permissionManageImageBuild             = "controlplane.image_build.manage"
	permissionClaimImageAdmission          = "controlplane.image_admission.claim"
	permissionRecordImageAdmission         = "controlplane.image_admission.record"
	permissionClaimImagePromotion          = "controlplane.image_promotion.claim"
	permissionAuthorizeImagePromotion      = "controlplane.image_promotion.authorize"
	permissionCompleteImagePromotion       = "controlplane.image_promotion.complete"
	permissionReadImageBuild               = "controlplane.image_build.read"
	permissionReadRoleImageRecipe          = "controlplane.role_image_recipe.read"
	permissionManageSession                = "controlplane.session.manage"
	permissionBindSessionMCP               = "controlplane.session.mcp.bind"
	permissionConversationLifecycle        = "controlplane.conversation.lifecycle"
	permissionWriteMemory                  = "controlplane.memory.write"
	permissionWriteProjectMemory           = "controlplane.memory.project.write"
	permissionManageWorkClaim              = "controlplane.work_claim.manage"
	permissionDeliverGate                  = "controlplane.owner_gate.deliver"
	permissionExpireGate                   = "controlplane.owner_gate.expire"
	permissionDeliverInteraction           = "controlplane.interaction.delivery"
	permissionReadRuntimeRevision          = "controlplane.runtime_revision.read"
	permissionIndexMemory                  = "controlplane.memory.index"
	permissionRepairOutbox                 = "controlplane.outbox.repair"
	permissionReadOutbox                   = "controlplane.outbox.read"
	permissionApplyGitConfiguration        = "controlplane.configuration.git.apply"
	permissionDetachConfiguration          = "controlplane.configuration.detach"
	permissionRuntimeClaim                 = "controlplane.runtime_execution.claim"
	permissionRuntimeRead                  = "controlplane.runtime_execution.read"
	permissionRuntimeOutputStage           = "controlplane.runtime_execution.output.stage"
	permissionRuntimeProgress              = "controlplane.runtime_execution.progress"
	permissionRuntimeAdmit                 = "controlplane.runtime_execution.admit"
	permissionRuntimeHeartbeat             = "controlplane.runtime_execution.heartbeat"
	permissionRuntimeIncident              = "controlplane.runtime_execution.incident"
	permissionRuntimeIncidentRead          = "controlplane.runtime_execution.incident.read"
	permissionOwnerSessionAdmit            = "controlplane.owner_session.admit"
	permissionOwnerSessionRevoke           = "controlplane.owner_session.revoke"
	permissionGatewayPublicTLSPrepare      = "controlplane.gateway_public_tls.prepare"
	permissionGatewayPublicTLSConfirm      = "controlplane.gateway_public_tls.confirm"
	permissionGatewayPublicTLSCheck        = "controlplane.gateway_public_tls.check"
	permissionRuntimeComplete              = "controlplane.runtime_execution.complete"
	permissionRuntimeCancel                = "controlplane.runtime_execution.cancel"
	permissionRuntimeRetry                 = "controlplane.runtime_execution.retry"
	permissionRuntimeOwnerAction           = "controlplane.runtime_execution.owner_action"
	permissionRuntimeReschedule            = "controlplane.runtime_execution.reschedule"
	permissionRuntimeExpire                = "controlplane.runtime_execution.expire"
	permissionRuntimeArchive               = "controlplane.runtime_execution.archive"
	permissionRuntimeRestore               = "controlplane.runtime_execution.restore.verify"
	permissionRuntimeRestoreBind           = "controlplane.runtime_execution.restore.bind"
	permissionRuntimeRestoreMaterialize    = "controlplane.runtime_execution.restore.materialize"
	permissionRuntimeRestoreCredential     = "controlplane.runtime_execution.restore.credential"
	permissionRuntimeRehydrate             = "controlplane.runtime_execution.rehydrate.complete"
	permissionRuntimeCleanup               = "controlplane.runtime_execution.cleanup.authorize"
	permissionRuntimeCleanupConsume        = "controlplane.runtime_execution.cleanup.consume"
	permissionRuntimeCleanupExpire         = "controlplane.runtime_execution.cleanup.expire"
	permissionRuntimeRetentionManage       = "controlplane.runtime_retention.manage"
	permissionRuntimeRetentionRead         = "controlplane.runtime_retention.read"
	permissionBackupRead                   = "controlplane.backup.read"
	permissionBackupRestore                = "controlplane.backup.restore"
	permissionIntegrationResolve           = "controlplane.integration_session.read"
	permissionIntegrationSuspend           = "controlplane.integration_continuation.suspend"
	permissionIntegrationDecide            = "controlplane.integration_continuation.decide"
	permissionIntegrationExecute           = "controlplane.integration_continuation.execute"
	permissionIntegrationRead              = "controlplane.integration_continuation.read"
	permissionIntegrationAcknowledge       = "controlplane.integration_continuation.acknowledge"
	permissionRoleDefinitionManage         = "controlplane.role_definition.manage"
	permissionAgentManage                  = "controlplane.agent.manage"
	permissionAgentBotIdentityManage       = "controlplane.agent.bot_identity.manage"
	permissionAgentAssignmentManage        = "controlplane.agent_assignment.manage"
	permissionInstructionSetManage         = "controlplane.instruction_set.manage"
	permissionProviderReferenceManage      = "controlplane.provider_reference.manage"
	permissionProviderPoolManage           = "controlplane.provider_pool.manage"
	permissionScheduleBind                 = "controlplane.schedule.bind"
	permissionScheduleCreateFromSelections = "controlplane.schedule.create_from_owner_selections"
	permissionRunManage                    = "controlplane.run.manage"
	permissionRuntimeIncidentManage        = "controlplane.runtime_execution.incident.manage"
	permissionWorkspaceBackupManage        = "controlplane.workspace_backup.manage"
	permissionWorkspaceRestoreManage       = "controlplane.workspace_restore.manage"
	permissionWorkspaceBackupTerminal      = "controlplane.workspace_backup.terminal"
	permissionWorkspaceRestoreTerminal     = "controlplane.workspace_restore.terminal"
	permissionWorkspaceMappingManage       = "controlplane.workspace_mapping.manage"
	agentRunnerWorkload                    = "agent-runner"
	agentRunnerSPIFFEID                    = "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"
	controlAPIGatewayWorkload              = "control-api-gateway"
	controlAPIGatewaySPIFFEID              = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway"
	runtimeRestoreEffectWorkload           = "runtime-s3-restore-exchanger"
	runtimeRestoreEffectSPIFFEID           = "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-s3-restore-exchanger"
	recoveryReconcilerWorkload             = "control-plane-recovery-reconciler"
	recoveryReconcilerSPIFFEID             = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane-recovery-reconciler"
)

// Config задаёт критичную для безопасности ограниченную политику выполнения.
type Config struct {
	LeaseSigningKey             []byte
	RuntimeAdmissionSigningKey  ed25519.PrivateKey
	ArchiveRestoreEnabled       bool
	RuntimeArchiveSigningKey    ed25519.PrivateKey
	RuntimeRestoreSigningKey    ed25519.PrivateKey
	TurnLeaseDuration           time.Duration
	MaximumScheduleClaims       int
	ImagePolicyRevision         uint64
	ImagePolicySHA256           string
	ImageBuildLeaseDuration     time.Duration
	ImageAdmissionClaimTTL      time.Duration
	ImagePromotionClaimTTL      time.Duration
	ImageMaximumAttempts        uint32
	StagingImageRepository      string
	PromotedImageRepository     string
	RoleImageInputRepository    string
	TrustedRoleBaseRepository   string
	TrustedRoleBaseDigest       string
	RoleRuntimeContractRevision uint64
	RoleRuntimeContractSHA256   string
	ImageBuilderWorkload        string
	ImageBuilderSPIFFEID        string
	ImageAdmissionWorkload      string
	ImageAdmissionSPIFFEID      string
	ImagePromotionWorkload      string
	ImagePromotionSPIFFEID      string
	AuthorityPolicyRevision     uint64
	AuthorityPolicySHA256       string
	OwnerGateDeliveryWorkload   string
	OwnerGateDeliverySPIFFEID   string
	ScannerWorkload             string
	ScannerSPIFFEID             string
	SchedulerWorkload           string
	SchedulerSPIFFEID           string
	MemoryIndexerWorkload       string
	MemoryIndexerSPIFFEID       string
	RuntimeControllerWorkload   string
	RuntimeControllerSPIFFEID   string
	ArchiveWorkload             string
	ArchiveSPIFFEID             string
	IntegrationGatewayWorkload  string
	IntegrationGatewaySPIFFEID  string
	RestoreVerifierWorkload     string
	RestoreVerifierSPIFFEID     string
	CleanupAuthorizerWorkload   string
	CleanupAuthorizerSPIFFEID   string
	PendingRescheduleDelay      time.Duration
	InteractionReadbackIssuer   InteractionReadbackIssuer
	Observer                    Observer
	InstructionObjects          domainobjectstore.Client
}

type InteractionReadbackClaims struct {
	Subject, OrganizationID, ProjectID, DeliveryID, JTI string
	Readiness                                           bool
	IssuedAt, ExpiresAt                                 time.Time
}

type InteractionReadbackCredential struct {
	Compact, SHA256, ProducerID, Purpose, WorkloadID, CallerSPIFFEID string
	Operation, Permission, KeysetSHA256                              string
	Generation, KeysetRevision, KeysetHighWatermark                  uint64
}

type InteractionReadbackIssuer interface {
	Issue(context.Context, InteractionReadbackClaims) (InteractionReadbackCredential, error)
	Check(context.Context) error
}

// Service владеет прикладными переходами; адаптер только сохраняет намерение.
type Service struct {
	repository                  domainrepo.Repository
	leaseSigningKey             []byte
	runtimeAdmissionSigningKey  ed25519.PrivateKey
	archiveRestoreEnabled       bool
	runtimeArchiveSigningKey    ed25519.PrivateKey
	runtimeRestoreSigningKey    ed25519.PrivateKey
	turnLeaseDuration           time.Duration
	maximumScheduleClaims       int
	imagePolicyRevision         uint64
	imagePolicySHA256           string
	imageBuildLeaseDuration     time.Duration
	imageAdmissionClaimTTL      time.Duration
	imagePromotionClaimTTL      time.Duration
	imageMaximumAttempts        uint32
	stagingImageRepository      string
	promotedImageRepository     string
	roleImageInputRepository    string
	trustedRoleBaseRepository   string
	trustedRoleBaseDigest       string
	roleRuntimeContractRevision uint64
	roleRuntimeContractSHA256   string
	imageBuilderWorkload        string
	imageBuilderSPIFFEID        string
	imageAdmissionWorkload      string
	imageAdmissionSPIFFEID      string
	imagePromotionWorkload      string
	imagePromotionSPIFFEID      string
	authorityPolicyRevision     uint64
	authorityPolicySHA256       string
	ownerGateDeliveryWorkload   string
	ownerGateDeliverySPIFFEID   string
	scannerWorkload             string
	scannerSPIFFEID             string
	schedulerWorkload           string
	schedulerSPIFFEID           string
	memoryIndexerWorkload       string
	memoryIndexerSPIFFEID       string
	runtimeControllerWorkload   string
	runtimeControllerSPIFFEID   string
	archiveWorkload             string
	archiveSPIFFEID             string
	integrationGatewayWorkload  string
	integrationGatewaySPIFFEID  string
	restoreVerifierWorkload     string
	restoreVerifierSPIFFEID     string
	cleanupAuthorizerWorkload   string
	cleanupAuthorizerSPIFFEID   string
	pendingRescheduleDelay      time.Duration
	observer                    Observer
	interactionReadbackIssuer   InteractionReadbackIssuer
	instructionObjects          domainobjectstore.Client
	now                         func() time.Time
}

// New создаёт сервис только с полноценными устойчивыми границами.
func New(repository domainrepo.Repository, config Config) (*Service, error) {
	if repository == nil || len(config.LeaseSigningKey) < 32 ||
		len(config.RuntimeAdmissionSigningKey) != ed25519.PrivateKeySize ||
		(config.ArchiveRestoreEnabled &&
			(len(config.RuntimeArchiveSigningKey) != ed25519.PrivateKeySize ||
				len(config.RuntimeRestoreSigningKey) != ed25519.PrivateKeySize ||
				bytes.Equal(config.RuntimeAdmissionSigningKey, config.RuntimeArchiveSigningKey) ||
				bytes.Equal(config.RuntimeAdmissionSigningKey, config.RuntimeRestoreSigningKey) ||
				bytes.Equal(config.RuntimeArchiveSigningKey, config.RuntimeRestoreSigningKey))) ||
		(!config.ArchiveRestoreEnabled &&
			(len(config.RuntimeArchiveSigningKey) != 0 || len(config.RuntimeRestoreSigningKey) != 0)) ||
		config.TurnLeaseDuration < 30*time.Second ||
		!validImageRepository(config.RoleImageInputRepository) ||
		config.TurnLeaseDuration > 30*time.Minute ||
		config.MaximumScheduleClaims < 1 ||
		config.MaximumScheduleClaims > 100 ||
		config.ImagePolicyRevision == 0 || !validSHA256Text(config.ImagePolicySHA256) ||
		config.ImageBuildLeaseDuration < 30*time.Second || config.ImageBuildLeaseDuration > 30*time.Minute ||
		config.ImageAdmissionClaimTTL < 30*time.Second || config.ImageAdmissionClaimTTL > 30*time.Minute ||
		config.ImagePromotionClaimTTL < 30*time.Second || config.ImagePromotionClaimTTL > 15*time.Minute ||
		config.ImageMaximumAttempts == 0 || config.ImageMaximumAttempts > 10 ||
		!validImageRepository(config.StagingImageRepository) ||
		!validImageRepository(config.PromotedImageRepository) ||
		config.StagingImageRepository == config.PromotedImageRepository ||
		!validImageRepository(config.TrustedRoleBaseRepository) || !validDigest(config.TrustedRoleBaseDigest) ||
		config.RoleRuntimeContractRevision == 0 || !validSHA256Text(config.RoleRuntimeContractSHA256) ||
		value.ValidateStableKey(config.ImageBuilderWorkload) != nil || !validSPIFFEID(config.ImageBuilderSPIFFEID) ||
		value.ValidateStableKey(config.ImageAdmissionWorkload) != nil || !validSPIFFEID(config.ImageAdmissionSPIFFEID) ||
		value.ValidateStableKey(config.ImagePromotionWorkload) != nil || !validSPIFFEID(config.ImagePromotionSPIFFEID) ||
		config.ImageBuilderWorkload == config.ImageAdmissionWorkload ||
		config.ImageBuilderSPIFFEID == config.ImageAdmissionSPIFFEID ||
		config.ImagePromotionWorkload == config.ImageAdmissionWorkload ||
		config.ImagePromotionSPIFFEID == config.ImageAdmissionSPIFFEID ||
		config.AuthorityPolicyRevision == 0 ||
		!validSHA256Text(config.AuthorityPolicySHA256) ||
		value.ValidateStableKey(config.OwnerGateDeliveryWorkload) != nil ||
		!validSPIFFEID(config.OwnerGateDeliverySPIFFEID) ||
		value.ValidateStableKey(config.ScannerWorkload) != nil ||
		!validSPIFFEID(config.ScannerSPIFFEID) ||
		value.ValidateStableKey(config.SchedulerWorkload) != nil ||
		!validSPIFFEID(config.SchedulerSPIFFEID) ||
		value.ValidateStableKey(config.MemoryIndexerWorkload) != nil ||
		!validSPIFFEID(config.MemoryIndexerSPIFFEID) ||
		value.ValidateStableKey(config.RuntimeControllerWorkload) != nil ||
		!validSPIFFEID(config.RuntimeControllerSPIFFEID) ||
		config.RuntimeControllerWorkload == agentRunnerWorkload ||
		config.RuntimeControllerSPIFFEID == agentRunnerSPIFFEID ||
		value.ValidateStableKey(config.ArchiveWorkload) != nil ||
		!validSPIFFEID(config.ArchiveSPIFFEID) ||
		config.ArchiveWorkload == config.RuntimeControllerWorkload ||
		config.ArchiveSPIFFEID == config.RuntimeControllerSPIFFEID ||
		value.ValidateStableKey(config.IntegrationGatewayWorkload) != nil ||
		!validSPIFFEID(config.IntegrationGatewaySPIFFEID) ||
		value.ValidateStableKey(config.RestoreVerifierWorkload) != nil ||
		!validSPIFFEID(config.RestoreVerifierSPIFFEID) ||
		value.ValidateStableKey(config.CleanupAuthorizerWorkload) != nil ||
		!validSPIFFEID(config.CleanupAuthorizerSPIFFEID) ||
		config.RestoreVerifierWorkload == config.RuntimeControllerWorkload ||
		config.RestoreVerifierSPIFFEID == config.RuntimeControllerSPIFFEID ||
		config.RestoreVerifierWorkload == config.CleanupAuthorizerWorkload ||
		config.RestoreVerifierSPIFFEID == config.CleanupAuthorizerSPIFFEID ||
		config.RestoreVerifierWorkload == config.IntegrationGatewayWorkload ||
		config.RestoreVerifierSPIFFEID == config.IntegrationGatewaySPIFFEID ||
		config.RestoreVerifierWorkload == controlAPIGatewayWorkload ||
		config.RestoreVerifierSPIFFEID == controlAPIGatewaySPIFFEID ||
		config.CleanupAuthorizerWorkload == config.RuntimeControllerWorkload ||
		config.CleanupAuthorizerSPIFFEID == config.RuntimeControllerSPIFFEID ||
		config.CleanupAuthorizerWorkload == config.IntegrationGatewayWorkload ||
		config.CleanupAuthorizerSPIFFEID == config.IntegrationGatewaySPIFFEID ||
		config.CleanupAuthorizerWorkload == controlAPIGatewayWorkload ||
		config.CleanupAuthorizerSPIFFEID == controlAPIGatewaySPIFFEID ||
		config.PendingRescheduleDelay < 5*time.Second || config.PendingRescheduleDelay > 5*time.Minute ||
		config.InteractionReadbackIssuer == nil || config.Observer == nil {
		return nil, errors.New("control-plane service configuration is invalid")
	}
	return &Service{
		repository:                  repository,
		leaseSigningKey:             slices.Clone(config.LeaseSigningKey),
		runtimeAdmissionSigningKey:  slices.Clone(config.RuntimeAdmissionSigningKey),
		archiveRestoreEnabled:       config.ArchiveRestoreEnabled,
		runtimeArchiveSigningKey:    slices.Clone(config.RuntimeArchiveSigningKey),
		runtimeRestoreSigningKey:    slices.Clone(config.RuntimeRestoreSigningKey),
		turnLeaseDuration:           config.TurnLeaseDuration,
		maximumScheduleClaims:       config.MaximumScheduleClaims,
		imagePolicyRevision:         config.ImagePolicyRevision,
		imagePolicySHA256:           config.ImagePolicySHA256,
		imageBuildLeaseDuration:     config.ImageBuildLeaseDuration,
		imageAdmissionClaimTTL:      config.ImageAdmissionClaimTTL,
		imagePromotionClaimTTL:      config.ImagePromotionClaimTTL,
		imageMaximumAttempts:        config.ImageMaximumAttempts,
		stagingImageRepository:      config.StagingImageRepository,
		promotedImageRepository:     config.PromotedImageRepository,
		roleImageInputRepository:    config.RoleImageInputRepository,
		trustedRoleBaseRepository:   config.TrustedRoleBaseRepository,
		trustedRoleBaseDigest:       config.TrustedRoleBaseDigest,
		roleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		roleRuntimeContractSHA256:   config.RoleRuntimeContractSHA256,
		imageBuilderWorkload:        config.ImageBuilderWorkload,
		imageBuilderSPIFFEID:        config.ImageBuilderSPIFFEID,
		imageAdmissionWorkload:      config.ImageAdmissionWorkload,
		imageAdmissionSPIFFEID:      config.ImageAdmissionSPIFFEID,
		imagePromotionWorkload:      config.ImagePromotionWorkload,
		imagePromotionSPIFFEID:      config.ImagePromotionSPIFFEID,
		authorityPolicyRevision:     config.AuthorityPolicyRevision,
		authorityPolicySHA256:       config.AuthorityPolicySHA256,
		ownerGateDeliveryWorkload:   config.OwnerGateDeliveryWorkload,
		ownerGateDeliverySPIFFEID:   config.OwnerGateDeliverySPIFFEID,
		scannerWorkload:             config.ScannerWorkload,
		scannerSPIFFEID:             config.ScannerSPIFFEID,
		schedulerWorkload:           config.SchedulerWorkload,
		schedulerSPIFFEID:           config.SchedulerSPIFFEID,
		memoryIndexerWorkload:       config.MemoryIndexerWorkload,
		memoryIndexerSPIFFEID:       config.MemoryIndexerSPIFFEID,
		runtimeControllerWorkload:   config.RuntimeControllerWorkload,
		runtimeControllerSPIFFEID:   config.RuntimeControllerSPIFFEID,
		archiveWorkload:             config.ArchiveWorkload,
		archiveSPIFFEID:             config.ArchiveSPIFFEID,
		integrationGatewayWorkload:  config.IntegrationGatewayWorkload,
		integrationGatewaySPIFFEID:  config.IntegrationGatewaySPIFFEID,
		restoreVerifierWorkload:     config.RestoreVerifierWorkload,
		restoreVerifierSPIFFEID:     config.RestoreVerifierSPIFFEID,
		cleanupAuthorizerWorkload:   config.CleanupAuthorizerWorkload,
		cleanupAuthorizerSPIFFEID:   config.CleanupAuthorizerSPIFFEID,
		pendingRescheduleDelay:      config.PendingRescheduleDelay,
		observer:                    config.Observer,
		interactionReadbackIssuer:   config.InteractionReadbackIssuer,
		instructionObjects:          config.InstructionObjects,
		now:                         time.Now,
	}, nil
}

// Create создаёт назначенные сервером ID, владельца, область проекта, состояние
// и версию OCC.
func (service *Service) Create(ctx context.Context, input CreateInput) (entity.Resource, error) {
	permission := permissionCreate
	if input.Administrative {
		permission = permissionAccessManage
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	return service.create(ctx, input, false)
}

// create выполняет общую атомарную запись после выбора закрытой специализированной
// authority. specializedProject выставляется только CreateProject и никогда
// не принимается из generic transport request.
func (service *Service) create(
	ctx context.Context,
	input CreateInput,
	specializedProject bool,
) (entity.Resource, error) {
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		!input.Kind.Valid() || input.Kind == enum.KindTurn ||
		(input.Kind == enum.KindProject) != input.TenantProject ||
		value.ValidateName(input.Name) != nil ||
		input.Spec == nil || input.Spec.Kind() != input.Kind ||
		input.Spec.Validate() != nil ||
		(input.ParentID != "" && value.ValidateID(input.ParentID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if !createCommandAllowed(input.Kind, input.Administrative, specializedProject) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if err := validateTemporalCreation(input.Spec, now); err != nil {
		return entity.Resource{}, err
	}
	resourceID := uuid.NewString()
	projectID, err := authoritativeProject(input.Principal, input.Kind, resourceID)
	if err != nil {
		return entity.Resource{}, err
	}
	resource, err := entity.New(
		resourceID,
		input.Principal.OrganizationID,
		projectID,
		input.ParentID,
		input.Principal.ActorID,
		input.Kind,
		input.Name,
		input.Spec,
		now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Administrative {
		resource, err = resource.Transition(enum.StatePaused, now)
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Kind     enum.Kind
		Name     string
		ParentID string
		Spec     entity.Spec
	}{identity(input.Principal), input.Kind, input.Name, input.ParentID, input.Spec})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	receiptScope := "create"
	if specializedProject {
		receiptScope = "create_project"
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		receiptScope,
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			if err := validateConfigurationCreate(
				ctx,
				tx,
				input.Principal,
				input.Spec,
			); err != nil {
				return entity.Resource{}, err
			}
			if input.Administrative {
				if err := service.validateAccessMutation(
					ctx,
					tx,
					input.Principal,
					resource.Kind,
					resource.Spec,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			if err := service.validateReferences(ctx, tx, resource); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, resource); err != nil {
				return entity.Resource{}, err
			}
			return resource, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				receiptScope,
				resource,
			)
		},
	)
}

// Update обновляет ресурс только после определения организации, проекта и OCC.
func (service *Service) Update(ctx context.Context, input UpdateInput) (entity.Resource, error) {
	permission := permissionUpdate
	if input.Administrative {
		permission = permissionAccessManage
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateName(input.Name) != nil ||
		input.Spec == nil || input.Spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if err := validateTemporalCreation(input.Spec, now); err != nil {
		return entity.Resource{}, err
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.Spec
		DetachGit       bool
	}{
		identity(input.Principal),
		input.ResourceID,
		input.ExpectedVersion,
		input.Name,
		input.Spec,
		input.DetachGitManagement,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"update",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if accessKind(current.Kind) != input.Administrative ||
				(!input.Administrative && protectedMutationKind(current.Kind)) {
				return entity.Resource{}, errs.ErrPermissionDenied
			}
			if input.Administrative {
				if err := service.validateAccessMutation(
					ctx,
					tx,
					input.Principal,
					current.Kind,
					input.Spec,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			if err := validateGenericUpdate(current, input.Spec); err != nil {
				return entity.Resource{}, err
			}
			nextSpec, err := configurationUpdateSpec(
				ctx,
				tx,
				input.Principal,
				current.Spec,
				input.Spec,
				input.DetachGitManagement,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			updated, err := current.Update(input.Name, nextSpec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := service.validateReferences(ctx, tx, updated); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"update",
				updated,
			)
		},
	)
}

// Transition выполняет закрытый автомат состояний; повтор хода увеличивает попытку.
func (service *Service) Transition(
	ctx context.Context,
	input TransitionInput,
) (entity.Resource, error) {
	permission := permissionTransition
	if input.Administrative {
		permission = permissionAccessManage
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil ||
		input.ExpectedVersion == 0 ||
		len(input.ReasonCode) < 1 || len(input.ReasonCode) > 96 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		Target          enum.State
		ReasonCode      string
	}{
		identity(input.Principal),
		input.ResourceID,
		input.ExpectedVersion,
		input.Target,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"transition",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if input.Administrative {
				if !accessKind(current.Kind) ||
					(input.Target != enum.StateActive &&
						input.Target != enum.StatePaused &&
						input.Target != enum.StateArchived) {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := service.validateAccessMutation(
					ctx,
					tx,
					input.Principal,
					current.Kind,
					current.Spec,
				); err != nil {
					return entity.Resource{}, err
				}
			} else if protectedTransitionKind(current.Kind) {
				return entity.Resource{}, errs.ErrPermissionDenied
			}
			if err := authorizeGitManagedMutation(
				ctx,
				tx,
				input.Principal,
				current.Spec,
			); err != nil {
				return entity.Resource{}, err
			}
			updated, err := service.transitionResource(current, input.Target)
			if err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"transition",
				updated,
			)
		},
	)
}

// Delete переводит ресурс через явный жизненный цикл удаления.
func (service *Service) Delete(ctx context.Context, input DeleteInput) (entity.Resource, error) {
	permission := permissionDelete
	if input.Administrative {
		permission = permissionAccessManage
	}
	if err := authorize(input.Principal, permission); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
	}{identity(input.Principal), input.ResourceID, input.ExpectedVersion})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"delete",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if accessKind(current.Kind) != input.Administrative ||
				(!input.Administrative && protectedMutationKind(current.Kind)) {
				return entity.Resource{}, errs.ErrPermissionDenied
			}
			if err := authorizeGitManagedMutation(
				ctx,
				tx,
				input.Principal,
				current.Spec,
			); err != nil {
				return entity.Resource{}, err
			}
			if input.Administrative {
				if err := service.validateAccessMutation(
					ctx,
					tx,
					input.Principal,
					current.Kind,
					current.Spec,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			target := enum.StateDeletionPending
			if current.State == enum.StateDeletionPending {
				target = enum.StateDeleted
			}
			updated, err := current.Transition(target, service.now())
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"delete",
				updated,
			)
		},
	)
}

// Get одинаково скрывает отсутствующий, чужой, удалённый ресурс и неверный вид.
func (service *Service) Get(ctx context.Context, input GetInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionRead); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateID(input.ResourceID) != nil || !input.Kind.Valid() {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Kind == enum.KindMemoryRecord {
		resource, err := service.getEligibleMemory(
			ctx, input.Principal, input.ResourceID,
		)
		if err != nil {
			return entity.Resource{}, err
		}
		if input.ExpectedVersion != 0 && resource.Version != input.ExpectedVersion {
			return entity.Resource{}, errs.ErrNotFound
		}
		return resource, nil
	}
	resource, err := service.repository.Get(
		ctx,
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		input.ResourceID,
		input.Kind,
	)
	if errors.Is(err, errs.ErrNotFound) && input.Principal.CallerWorkload == service.runtimeControllerWorkload &&
		(input.Kind == enum.KindRole || input.Kind == enum.KindPromptProfile) && input.ExpectedVersion != 0 {
		projectionRepository, ok := service.repository.(domainrepo.RuntimeProjectionRepository)
		if !ok {
			return entity.Resource{}, errs.ErrUnavailable
		}
		resource, err = projectionRepository.GetDerivedRuntimeResource(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ResourceID, input.Kind, input.ExpectedVersion)
	}
	if err != nil {
		return entity.Resource{}, err
	}
	if resource.Kind != input.Kind || resource.State == enum.StateDeleted {
		return entity.Resource{}, errs.ErrNotFound
	}
	if ownerBoundLifecycleKind(resource.Kind) &&
		resource.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if (input.Principal.CallerWorkload == "runtime-controller" ||
		input.Principal.CallerWorkload == "automation-scheduler") &&
		input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.ExpectedVersion != 0 && resource.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

// List заменяет фильтры вызывающего проверенной границей владения.
func (service *Service) List(ctx context.Context, input ListInput) ([]entity.Resource, error) {
	if err := authorize(input.Principal, permissionList); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	input.Filter.ActorID = input.Principal.ActorID
	if err := input.Filter.Validate(); err != nil {
		return nil, errs.ErrInvalidInput
	}
	if input.TenantProjects {
		if input.Principal.ProjectID != "" || input.Filter.Kind != enum.KindProject {
			return nil, errs.ErrPermissionDenied
		}
		return service.repository.ListEligibleProjects(
			ctx,
			input.Principal.OrganizationID,
			input.Principal.ActorID,
			input.Filter.AfterID,
			input.Filter.Limit,
		)
	}
	if input.Filter.Kind == enum.KindProject || input.Principal.ProjectID == "" {
		return nil, errs.ErrPermissionDenied
	}
	if input.Filter.Kind == enum.KindMemoryRecord {
		hits, err := service.searchEligibleMemory(
			ctx,
			input.Principal,
			domainrepo.MemorySearch{
				ParentID:     input.Filter.ParentID,
				States:       input.Filter.States,
				AfterID:      input.Filter.AfterID,
				Limit:        input.Filter.Limit,
				GenericOrder: true,
			},
		)
		if err != nil {
			return nil, err
		}
		resources := make([]entity.Resource, 0, len(hits))
		for _, hit := range hits {
			resources = append(resources, hit.Resource)
		}
		return resources, nil
	}
	resources, err := service.repository.List(ctx, input.Filter)
	if err != nil {
		return nil, err
	}
	return filterOwnerBoundResources(resources, input.Principal.ActorID), nil
}

type resourceMutation func(domainrepo.Transaction) (entity.Resource, error)

type resourceReceiptValidation func(
	domainrepo.Transaction,
) (lifecycleReceiptDisposition, error)

func resourceReceiptMatchesCurrent(current, stored entity.Resource) error {
	currentHash, err := canonicalHash(current)
	if err != nil {
		return errs.ErrInternal
	}
	storedHash, err := canonicalHash(stored)
	if err != nil {
		return errs.ErrInternal
	}
	if currentHash != storedHash {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) withValidatedResourceReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
	scope string,
	requestHash string,
	validate resourceReceiptValidation,
	validateReplay func(domainrepo.Transaction, entity.Resource) error,
	apply resourceMutation,
) (entity.Resource, error) {
	keyHash := hashString(idempotencyKey)
	var result entity.Resource
	mutated := false
	err := service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: principal.OrganizationID,
			ProjectID:      principal.ProjectID,
			ActorID:        principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			disposition, err := validate(tx)
			if err != nil {
				return err
			}
			receipt, err := tx.GetReceipt(
				ctx, principal.OrganizationID, scope, keyHash,
			)
			if err == nil {
				if (disposition != lifecycleReceiptReplay &&
					disposition != lifecycleReceiptApplyOrReplay) ||
					receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				result = receipt.Result
				if err := result.Validate(); err != nil {
					return errs.ErrInternal
				}
				return validateReplay(tx, result)
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			if disposition != lifecycleReceiptApply &&
				disposition != lifecycleReceiptApplyOrReplay {
				return errs.ErrStateConflict
			}
			result, err = apply(tx)
			if err != nil {
				return err
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: principal.OrganizationID,
				ProjectID:      principal.ProjectID,
				Scope:          scope,
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         result,
				CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
			}); err != nil {
				return err
			}
			mutated = true
			return nil
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(result.Kind, scope)
	}
	return result, err
}

func (service *Service) withResourceReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
	scope string,
	requestHash string,
	apply resourceMutation,
) (entity.Resource, error) {
	keyHash := hashString(idempotencyKey)
	var result entity.Resource
	mutated := false
	err := service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: principal.OrganizationID,
			ProjectID:      principal.ProjectID,
			ActorID:        principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, err := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
			if err == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				result = receipt.Result
				return result.Validate()
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			applied, err := apply(tx)
			if err != nil {
				return err
			}
			receipt = domainrepo.Receipt{
				OrganizationID: principal.OrganizationID,
				ProjectID:      principal.ProjectID,
				Scope:          scope,
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         applied,
				CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
			}
			if err := tx.SaveReceipt(ctx, receipt); err != nil {
				return err
			}
			result = applied
			mutated = true
			return nil
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(result.Kind, scope)
	}
	return result, err
}

// withOwnerLockedResourceReceipt всегда разрешает существующий источник внутри
// проверенной owner boundary до чтения idempotency receipt. Тот же intent может
// вернуть исторический receipt и после последующего перехода ресурса.
func (service *Service) withOwnerLockedResourceReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey, scope, requestHash, sourceResourceID string,
	sourceKind enum.Kind,
	expectedSourceVersion uint64,
	validateStored func(entity.Resource) error,
	apply resourceMutation,
) (entity.Resource, error) {
	return service.withValidatedResourceReceipt(ctx, principal, idempotencyKey,
		scope, requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			// Bind двух Schedule может затронуть одну Session reference. Общий
			// project fence берётся до любого Schedule row lock, чтобы один writer
			// не держал свой Schedule, ожидая fence, пока владелец fence блокирует
			// этот же row через проверку shared references.
			if scope == "bind_schedule_configuration" && sourceKind == enum.KindSchedule {
				if _, err := lockScheduleSessionProjectFence(ctx, tx, principal); err != nil {
					return 0, err
				}
			}
			current, err := tx.GetForUpdateIncludingDeleted(ctx, principal.OrganizationID,
				principal.ProjectID, sourceResourceID)
			if err != nil {
				return 0, err
			}
			if current.Kind != sourceKind || current.OwnerActorID != principal.ActorID {
				return 0, errs.ErrNotFound
			}
			if current.Version < expectedSourceVersion {
				return 0, errs.ErrVersionMismatch
			}
			if current.Version == expectedSourceVersion {
				return lifecycleReceiptApplyOrReplay, nil
			}
			return lifecycleReceiptReplay, nil
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			if err := stored.Validate(); err != nil {
				return errs.ErrInternal
			}
			return validateStored(stored)
		},
		apply,
	)
}

func (service *Service) appendMutationRecords(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	action string,
	resource entity.Resource,
) error {
	if resource.Kind == enum.KindSchedule && !scheduleResourceMutationAction(action) {
		return errs.ErrInternal
	}
	if action == "renew_turn" {
		return errs.ErrInternal
	}
	audit := domainrepo.Audit{
		ID:              uuid.NewString(),
		OrganizationID:  resource.OrganizationID,
		ProjectID:       resource.ProjectID,
		ActorID:         principal.ActorID,
		Action:          action,
		ResourceID:      resource.ID,
		ResourceKind:    string(resource.Kind),
		ResourceVersion: resource.Version,
		Outcome:         "succeeded",
		CorrelationID:   principal.CorrelationID,
		PolicyRevision:  principal.PolicyRevision,
		OccurredAt:      resource.UpdatedAt,
	}
	if err := tx.AppendAudit(ctx, audit); err != nil {
		return err
	}
	eventName, published := event.EventNameForKind(resource.Kind)
	if !published {
		return nil
	}
	return tx.AppendEvent(ctx, event.Change{
		EventID:         uuid.NewString(),
		EventName:       eventName,
		OrganizationID:  resource.OrganizationID,
		ProjectID:       resource.ProjectID,
		ResourceID:      resource.ID,
		ResourceKind:    resource.Kind,
		ResourceState:   resource.State,
		ResourceVersion: resource.Version,
		EventSequence:   resource.Version,
		OccurredAt:      resource.UpdatedAt,
		CorrelationID:   principal.CorrelationID,
	})
}

// scheduleResourceMutationAction — закрытый реестр команд, которые реально
// увеличивают Schedule.Version. Изменение occurrence/run не маскируется
// повторным событием неизменённого Schedule с уже занятой sequence.
func scheduleResourceMutationAction(action string) bool {
	switch action {
	case "create_schedule", "claim_due_schedule",
		"manage_schedule_UPDATE", "manage_schedule_ACTIVATE",
		"manage_schedule_PAUSE", "manage_schedule_ARCHIVE",
		"manage_schedule_DELETE_ARCHIVE", "manage_schedule_DELETE_PENDING",
		"manage_schedule_DELETE":
		return true
	default:
		return false
	}
}

// appendOwnerStateAudit фиксирует изменение owner-row, у которого нет
// отдельного domain-event контракта. resourceVersion является монотонным
// attempt/fence владельца; outbox для неизменённого родительского Resource не
// создаётся, а authoritative read остаётся частью той же транзакции.
func appendOwnerStateAudit(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	action, organizationID, projectID, resourceID, resourceKind string,
	resourceVersion uint64,
	occurredAt time.Time,
) error {
	if value.ValidateID(resourceID) != nil || resourceVersion == 0 ||
		occurredAt.IsZero() {
		return errs.ErrInternal
	}
	return tx.AppendAudit(ctx, domainrepo.Audit{
		ID: uuid.NewString(), OrganizationID: organizationID, ProjectID: projectID,
		ActorID: principal.ActorID, Action: action, ResourceID: resourceID,
		ResourceKind: resourceKind, ResourceVersion: resourceVersion,
		Outcome: "succeeded", CorrelationID: principal.CorrelationID,
		PolicyRevision: principal.PolicyRevision, OccurredAt: occurredAt,
	})
}

func appendScheduleOccurrenceAudit(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	action string,
	occurrence domainrepo.ScheduleOccurrence,
) error {
	return appendOwnerStateAudit(
		ctx, tx, principal, action, occurrence.OrganizationID,
		occurrence.ProjectID, occurrence.ID, auditKindScheduleOccurrence,
		uint64(occurrence.Attempt), occurrence.UpdatedAt,
	)
}

func (service *Service) transitionResource(
	current entity.Resource,
	target enum.State,
) (entity.Resource, error) {
	if current.Kind == enum.KindTurn && target == enum.StateQueued {
		spec, ok := current.Spec.(entity.TurnSpec)
		if !ok || spec.Attempt >= 100 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.Attempt++
		spec.Outcome = ""
		spec.ResultArtifactID = ""
		spec.ResultArtifactVersion = 0
		spec.ResultArtifactSHA256 = ""
		updated, err := current.ReplaceAndTransition(spec, target, service.now())
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return updated, nil
	}
	updated, err := current.Transition(target, service.now())
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return updated, nil
}

func validateGenericUpdate(current entity.Resource, next entity.Spec) error {
	if next.Kind() != current.Kind || current.Kind == enum.KindTurn {
		return errs.ErrStateConflict
	}
	switch currentSpec := current.Spec.(type) {
	case entity.ProjectSpec:
		nextSpec, ok := next.(entity.ProjectSpec)
		if !ok || currentSpec.Slug != nextSpec.Slug {
			return errs.ErrStateConflict
		}
	case entity.TeamSpec:
		nextSpec, ok := next.(entity.TeamSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey {
			return errs.ErrStateConflict
		}
	case entity.ChatSpec:
		nextSpec, ok := next.(entity.ChatSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey ||
			currentSpec.RoomType != nextSpec.RoomType ||
			currentSpec.ExternalChannelRef != nextSpec.ExternalChannelRef {
			return errs.ErrStateConflict
		}
	case entity.RoleSpec:
		nextSpec, ok := next.(entity.RoleSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey {
			return errs.ErrStateConflict
		}
	case entity.PromptProfileSpec:
		nextSpec, ok := next.(entity.PromptProfileSpec)
		if !ok || currentSpec.Revision == ^uint64(0) ||
			nextSpec.Revision != currentSpec.Revision+1 {
			return errs.ErrStateConflict
		}
	case entity.CredentialBindingSpec:
		nextSpec, ok := next.(entity.CredentialBindingSpec)
		if !ok || currentSpec.Purpose != nextSpec.Purpose ||
			currentSpec.PrincipalRef != nextSpec.PrincipalRef ||
			currentSpec.Revision == ^uint64(0) ||
			nextSpec.Revision != currentSpec.Revision+1 {
			return errs.ErrStateConflict
		}
	case entity.RepositoryWorkspaceSpec:
		nextSpec, ok := next.(entity.RepositoryWorkspaceSpec)
		if !ok || currentSpec.RepositoryRef != nextSpec.RepositoryRef ||
			currentSpec.WorkspaceMode != nextSpec.WorkspaceMode {
			return errs.ErrStateConflict
		}
	case entity.IntegrationSpec:
		nextSpec, ok := next.(entity.IntegrationSpec)
		if !ok || currentSpec.DefinitionRef != nextSpec.DefinitionRef ||
			nextSpec.DefinitionVersion <= currentSpec.DefinitionVersion {
			return errs.ErrStateConflict
		}
	case entity.RuntimeRevisionSpec:
		return errs.ErrStateConflict
	case entity.RoleImageRecipeSpec, entity.ImageBuildSpec, entity.ImageArtifactSpec:
		return errs.ErrStateConflict
	case entity.SessionSpec:
		nextSpec, ok := next.(entity.SessionSpec)
		if !ok || currentSpec.AgentID != nextSpec.AgentID ||
			currentSpec.ProviderAccountBindingID != nextSpec.ProviderAccountBindingID ||
			currentSpec.LastTurnSequence != nextSpec.LastTurnSequence ||
			(currentSpec.ConversationID != "" &&
				currentSpec.ConversationID != nextSpec.ConversationID) {
			return errs.ErrStateConflict
		}
	case entity.OwnerGateSpec:
		nextSpec, ok := next.(entity.OwnerGateSpec)
		if !ok || currentSpec.ProcessRunID != nextSpec.ProcessRunID ||
			currentSpec.ResultRef != nextSpec.ResultRef ||
			currentSpec.ResultSHA256 != nextSpec.ResultSHA256 ||
			currentSpec.ExpiresAt != nextSpec.ExpiresAt ||
			currentSpec.Decision != nextSpec.Decision ||
			currentSpec.DecisionReason != nextSpec.DecisionReason {
			return errs.ErrStateConflict
		}
	case entity.ProcessRunSpec:
		nextSpec, ok := next.(entity.ProcessRunSpec)
		if !ok || currentSpec.ParentProcessRunID != nextSpec.ParentProcessRunID ||
			currentSpec.RootTriggerRef != nextSpec.RootTriggerRef ||
			currentSpec.PlaybookRef != nextSpec.PlaybookRef ||
			currentSpec.PolicyRevision != nextSpec.PolicyRevision ||
			(currentSpec.ResultArtifactID != "" &&
				currentSpec.ResultArtifactID != nextSpec.ResultArtifactID) {
			return errs.ErrStateConflict
		}
	case entity.ScheduleSpec:
		nextSpec, ok := next.(entity.ScheduleSpec)
		if !ok || currentSpec.TargetResourceID != nextSpec.TargetResourceID {
			return errs.ErrStateConflict
		}
	case entity.MemoryRecordSpec:
		nextSpec, ok := next.(entity.MemoryRecordSpec)
		if !ok || currentSpec.Scope != nextSpec.Scope ||
			currentSpec.RoleID != nextSpec.RoleID ||
			currentSpec.Provenance != nextSpec.Provenance {
			return errs.ErrStateConflict
		}
	case entity.WorkClaimSpec:
		nextSpec, ok := next.(entity.WorkClaimSpec)
		if !ok || currentSpec.ProcessRunID != nextSpec.ProcessRunID ||
			currentSpec.TurnID != nextSpec.TurnID {
			return errs.ErrStateConflict
		}
	case entity.ArtifactSpec:
		return errs.ErrStateConflict
	}
	return nil
}

func validateTemporalCreation(spec entity.Spec, now time.Time) error {
	switch typed := spec.(type) {
	case entity.SessionSpec:
		if typed.LastTurnSequence != 0 {
			return errs.ErrInvalidInput
		}
	case entity.ProcessRunSpec:
		if typed.ResultArtifactID != "" {
			return errs.ErrInvalidInput
		}
	case entity.ArtifactSpec:
		if typed.ScanStatus != "PENDING" {
			return errs.ErrInvalidInput
		}
	case entity.CredentialBindingSpec:
		if !typed.ExpiresAt.IsZero() && !typed.ExpiresAt.After(now) {
			return errs.ErrInvalidInput
		}
	case entity.ScheduleSpec:
		if !typed.NextRunAt.After(now) {
			return errs.ErrInvalidInput
		}
		if typed.Cron != "" {
			if _, err := scheduleParser.Parse(typed.Cron); err != nil {
				return errs.ErrInvalidInput
			}
		}
	case entity.OwnerGateSpec:
		if !typed.ExpiresAt.After(now) ||
			typed.Decision != "" || typed.DecisionReason != "" {
			return errs.ErrInvalidInput
		}
	}
	return nil
}

func accessKind(kind enum.Kind) bool {
	return kind == enum.KindTeam
}

// createCommandAllowed связывает защищённый PROJECT только со
// специализированной командой. Универсальный и административный create не могут
// выставить specializedProject через transport.
func createCommandAllowed(kind enum.Kind, administrative, specializedProject bool) bool {
	return specializedProject == (kind == enum.KindProject) &&
		accessKind(kind) == administrative &&
		(administrative || !protectedCreateKind(kind) || specializedProject)
}

func protectedCreateKind(kind enum.Kind) bool {
	switch kind {
	case enum.KindProject, enum.KindRuntimeRevision, enum.KindProcessRun, enum.KindSchedule,
		enum.KindOwnerGate, enum.KindArtifact, enum.KindSession,
		enum.KindMemoryRecord, enum.KindWorkClaim, enum.KindRoleImageRecipe,
		enum.KindImageBuild, enum.KindImageArtifact, enum.KindRole, enum.KindPromptProfile,
		enum.KindRoleDefinition, enum.KindAgent, enum.KindAgentAssignment,
		enum.KindInstructionSet, enum.KindProviderReference, enum.KindProviderPool,
		enum.KindWorkspaceBackup, enum.KindWorkspaceRestore, enum.KindWorkspaceMapping:
		return true
	default:
		return false
	}
}

func protectedMutationKind(kind enum.Kind) bool {
	switch kind {
	case enum.KindProject, enum.KindRuntimeRevision, enum.KindSession, enum.KindTurn,
		enum.KindProcessRun, enum.KindSchedule, enum.KindOwnerGate,
		enum.KindArtifact, enum.KindMemoryRecord, enum.KindWorkClaim,
		enum.KindRoleImageRecipe, enum.KindImageBuild, enum.KindImageArtifact,
		enum.KindRole, enum.KindPromptProfile, enum.KindRoleDefinition, enum.KindAgent,
		enum.KindAgentAssignment, enum.KindInstructionSet, enum.KindProviderReference,
		enum.KindProviderPool, enum.KindWorkspaceBackup, enum.KindWorkspaceRestore,
		enum.KindWorkspaceMapping:
		return true
	default:
		return false
	}
}

func protectedTransitionKind(kind enum.Kind) bool {
	return accessKind(kind) || protectedMutationKind(kind)
}

func ownerBoundLifecycleKind(kind enum.Kind) bool {
	switch kind {
	case enum.KindProject, enum.KindSession, enum.KindTurn, enum.KindProcessRun,
		enum.KindSchedule, enum.KindOwnerGate, enum.KindWorkClaim,
		enum.KindRoleImageRecipe, enum.KindImageBuild, enum.KindImageArtifact,
		enum.KindRoleDefinition, enum.KindAgent, enum.KindAgentAssignment,
		enum.KindInstructionSet, enum.KindProviderReference, enum.KindProviderPool,
		enum.KindWorkspaceBackup, enum.KindWorkspaceRestore, enum.KindWorkspaceMapping:
		return true
	default:
		return false
	}
}

func filterOwnerBoundResources(
	resources []entity.Resource,
	actorID string,
) []entity.Resource {
	filtered := resources[:0]
	for _, resource := range resources {
		if !ownerBoundLifecycleKind(resource.Kind) ||
			resource.OwnerActorID == actorID {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func kindAdminPermission(kind enum.Kind) string {
	switch kind {
	case enum.KindTeam:
		return "controlplane.team.admin"
	case enum.KindRole:
		return "controlplane.role.admin"
	case enum.KindPromptProfile:
		return "controlplane.prompt_profile.admin"
	default:
		return ""
	}
}

func (service *Service) validateAccessMutation(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	kind enum.Kind,
	spec entity.Spec,
) error {
	required := kindAdminPermission(kind)
	if required == "" || spec == nil || spec.Kind() != kind {
		return errs.ErrStateConflict
	}
	permissions, err := tx.ActorPermissions(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
		principal.ActorID,
	)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "*") &&
		!slices.Contains(permissions, required) {
		return errs.ErrPermissionDenied
	}
	switch typed := spec.(type) {
	case entity.PromptProfileSpec:
		return nil
	case entity.RoleSpec:
		if err := ensureAssignable(permissions, typed.Capabilities); err != nil {
			return err
		}
		prompt, err := tx.GetForUpdate(
			ctx,
			principal.OrganizationID,
			principal.ProjectID,
			typed.PromptProfileID,
		)
		if err != nil {
			return err
		}
		if prompt.Kind != enum.KindPromptProfile ||
			prompt.State != enum.StateActive {
			return errs.ErrStateConflict
		}
		for _, roleID := range typed.AllowedTargetRoleIDs {
			target, err := tx.GetForUpdate(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				roleID,
			)
			if err != nil {
				return err
			}
			targetSpec, ok := target.Spec.(entity.RoleSpec)
			if !ok || target.Kind != enum.KindRole ||
				target.State != enum.StateActive {
				return errs.ErrStateConflict
			}
			if err := ensureAssignable(permissions, targetSpec.Capabilities); err != nil {
				return err
			}
		}
		return nil
	case entity.TeamSpec:
		if slices.Contains(typed.MemberActorIDs, principal.ActorID) {
			return errs.ErrPermissionDenied
		}
		for _, roleID := range typed.RoleIDs {
			role, err := tx.GetForUpdate(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				roleID,
			)
			if err != nil {
				return err
			}
			roleSpec, ok := role.Spec.(entity.RoleSpec)
			if !ok || role.Kind != enum.KindRole ||
				role.State != enum.StateActive {
				return errs.ErrStateConflict
			}
			if err := ensureAssignable(permissions, roleSpec.Capabilities); err != nil {
				return err
			}
		}
		return nil
	default:
		return errs.ErrStateConflict
	}
}

func ensureAssignable(callerPermissions, requested []string) error {
	if slices.Contains(callerPermissions, "*") {
		return nil
	}
	for _, permission := range requested {
		if !slices.Contains(callerPermissions, permission) {
			return errs.ErrPermissionDenied
		}
	}
	return nil
}

type reference struct {
	id    string
	kinds []enum.Kind
}

func (service *Service) validateReferences(
	ctx context.Context,
	tx domainrepo.Transaction,
	resource entity.Resource,
) error {
	references := make([]reference, 0, 16)
	add := func(identifier string, kinds ...enum.Kind) {
		if identifier != "" {
			references = append(references, reference{id: identifier, kinds: kinds})
		}
	}
	add(resource.ParentID)
	switch spec := resource.Spec.(type) {
	case entity.TeamSpec:
		for _, identifier := range spec.RoleIDs {
			add(identifier, enum.KindRole)
		}
	case entity.ChatSpec:
		add(spec.DefaultAgentID, enum.KindAgent, enum.KindRole)
	case entity.RoleSpec:
		add(spec.PromptProfileID, enum.KindPromptProfile)
		add(spec.RoleImageRecipeID, enum.KindRoleImageRecipe)
		for _, identifier := range spec.AllowedTargetRoleIDs {
			add(identifier, enum.KindRole)
		}
		for _, identifier := range spec.ProviderCredentialBindingIDs {
			add(identifier, enum.KindCredentialBinding)
		}
		for _, identifier := range spec.RepositoryWorkspaceIDs {
			add(identifier, enum.KindRepositoryWorkspace)
		}
		for _, identifier := range spec.IntegrationIDs {
			add(identifier, enum.KindIntegration)
		}
	case entity.RepositoryWorkspaceSpec:
		add(spec.CredentialBindingID, enum.KindCredentialBinding)
	case entity.IntegrationSpec:
		for _, identifier := range spec.CredentialBindingIDs {
			add(identifier, enum.KindCredentialBinding)
		}
	case entity.RuntimeRevisionSpec:
		add(spec.PromptProfileID, enum.KindPromptProfile)
		add(spec.RoleImageRecipeID, enum.KindRoleImageRecipe)
		add(spec.ImageBuildID, enum.KindImageBuild)
		add(spec.ImageArtifactID, enum.KindImageArtifact)
		for _, identifier := range spec.CredentialBindingIDs {
			add(identifier, enum.KindCredentialBinding)
		}
		for _, identifier := range spec.IntegrationIDs {
			add(identifier, enum.KindIntegration)
		}
	case entity.SessionSpec:
		add(spec.AgentID, enum.KindRole)
		add(spec.ProviderAccountBindingID, enum.KindCredentialBinding)
		add(spec.ConversationID, enum.KindChat)
	case entity.TurnSpec:
		add(spec.SessionID, enum.KindSession)
		add(spec.PromptArtifactID, enum.KindArtifact)
		add(spec.RuntimeRevisionID, enum.KindRuntimeRevision)
		add(spec.ProcessRunID, enum.KindProcessRun)
		add(spec.ResultArtifactID, enum.KindArtifact)
	case entity.ProcessRunSpec:
		add(spec.ParentProcessRunID, enum.KindProcessRun)
		add(spec.ResultArtifactID, enum.KindArtifact)
		add(spec.RuntimeRevisionID, enum.KindRuntimeRevision)
		add(spec.LaunchingProcessRunID, enum.KindProcessRun)
		add(spec.LaunchingTurnID, enum.KindTurn)
	case entity.ScheduleSpec:
		add(spec.TargetResourceID)
		add(spec.PromptProfileID, enum.KindPromptProfile)
		add(spec.PromptArtifactID, enum.KindArtifact)
		add(spec.ExecutionSessionID, enum.KindSession)
		add(spec.RoomID, enum.KindChat)
		add(spec.RuntimeRevisionID, enum.KindRuntimeRevision)
	case entity.OwnerGateSpec:
		add(spec.ProcessRunID, enum.KindProcessRun)
	case entity.MemoryRecordSpec:
		add(spec.RoleID, enum.KindRole)
	case entity.WorkClaimSpec:
		add(spec.ProcessRunID, enum.KindProcessRun)
		add(spec.TurnID, enum.KindTurn)
	case entity.ImageBuildSpec:
		add(spec.RecipeID, enum.KindRoleImageRecipe)
	case entity.ImageArtifactSpec:
		add(spec.RecipeID, enum.KindRoleImageRecipe)
		add(spec.BuildID, enum.KindImageBuild)
	}
	slices.SortFunc(references, func(left, right reference) int {
		if left.id < right.id {
			return -1
		}
		if left.id > right.id {
			return 1
		}
		return 0
	})
	for _, expected := range references {
		if expected.id == resource.ID {
			return errs.ErrStateConflict
		}
		referenced, err := tx.GetForUpdate(
			ctx,
			resource.OrganizationID,
			resource.ProjectID,
			expected.id,
		)
		if err != nil {
			return err
		}
		if referenced.State == enum.StateDeleted ||
			(len(expected.kinds) > 0 && !slices.Contains(expected.kinds, referenced.Kind)) {
			return errs.ErrNotFound
		}
	}
	return nil
}

type commandIdentity struct {
	ActorID             string
	OrganizationID      string
	ProjectID           string
	Permission          string
	PolicyRevision      uint64
	AuthorityGeneration uint64
	CallerWorkload      string
	CallerSPIFFEID      string
	AuthoritySource     string
	AuthorityReference  string
	AuthorityRevision   uint64
	AuthorityDigest     string
	GrantGeneration     uint64
}

func identity(principal value.Principal) commandIdentity {
	result := commandIdentity{
		ActorID:             principal.ActorID,
		OrganizationID:      principal.OrganizationID,
		ProjectID:           principal.ProjectID,
		Permission:          principal.Permission,
		PolicyRevision:      principal.PolicyRevision,
		AuthorityGeneration: principal.AuthorityGeneration,
		CallerWorkload:      principal.CallerWorkload,
		CallerSPIFFEID:      principal.CallerSPIFFEID,
		AuthoritySource:     principal.AuthoritySource,
		AuthorityReference:  principal.AuthorityReference,
		AuthorityRevision:   principal.AuthorityRevision,
		AuthorityDigest:     principal.AuthorityDigest,
		GrantGeneration:     principal.AuthorityGrantGeneration,
	}
	// У короткоживущего application grant reference/revision/digest описывают
	// только текущий bearer. Они участвуют в transport replay protection, но не
	// меняют долговечный semantic intent уже проверенной workload-команды.
	// Bound grants несут generation и сохраняют exact session/turn lineage.
	if (principal.AuthoritySource == "AUTOMATION_OCCURRENCE" &&
		principal.AuthorityGrantGeneration == 0) ||
		principal.AuthoritySource == "WORKLOAD_READINESS" {
		result.AuthorityReference = ""
		result.AuthorityRevision = 0
		result.AuthorityDigest = ""
	}
	return result
}

// semanticCommandHash связывает долговечный intent только со стабильной
// проверенной authority и бизнес-полями. CorrelationID/JTI и прочие
// одноразовые transport proof artifacts намеренно не входят в receipt hash.
func semanticCommandHash(principal value.Principal, command any) (string, error) {
	return canonicalHash(struct {
		Identity commandIdentity
		Command  any
	}{identity(principal), command})
}

func canonicalHash(input any) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal command: %w", err)
	}
	return hashBytes(encoded), nil
}

func hashString(input string) string {
	return hashBytes([]byte(input))
}

func hashBytes(input []byte) string {
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}

func validSHA256Text(input string) bool {
	if len(input) != 64 || input != strings.ToLower(input) {
		return false
	}
	for _, symbol := range input {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func validOpaqueRuntimeIdentifier(input string) bool {
	return len(input) >= 1 && len(input) <= 256 && strings.TrimSpace(input) == input &&
		!strings.ContainsAny(input, "\x00\r\n")
}

func validSPIFFEID(input string) bool {
	return strings.HasPrefix(input, "spiffe://mattercodex.local/") &&
		len(input) <= 512 && !strings.ContainsAny(input, " \t\r\n")
}

func (service *Service) leaseToken(
	turnID string,
	fence uint64,
	attempt uint32,
	authorityGeneration uint64,
	workloadID string,
	idempotencyKey string,
) string {
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = fmt.Fprintf(
		mac,
		"%s\x00%d\x00%d\x00%d\x00%s\x00%s",
		turnID,
		fence,
		attempt,
		authorityGeneration,
		workloadID,
		idempotencyKey,
	)
	return hex.EncodeToString(mac.Sum(nil))
}

func authorize(principal value.Principal, permission string) error {
	if err := principal.Validate(); err != nil {
		return errs.ErrUnauthenticated
	}
	if principal.Permission != permission {
		return errs.ErrPermissionDenied
	}
	return nil
}

func validateConfigurationCreate(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	spec entity.Spec,
) error {
	configured, ok := spec.(entity.ConfiguredSpec)
	if !ok {
		return nil
	}
	ownership := configured.ConfigurationOwnership()
	if ownership.ManagedBy == "UI" {
		if ownership.SourceRef != "" || ownership.SourceRevision != 0 || ownership.SourceSHA256 != "" {
			return errs.ErrInvalidInput
		}
		return nil
	}
	if ownership.ManagedBy != "GIT" || ownership.Validate() != nil ||
		!validSHA256Text(ownership.SourceSHA256) {
		return errs.ErrInvalidInput
	}
	return requireDurablePermission(
		ctx,
		tx,
		principal,
		permissionApplyGitConfiguration,
	)
}

func configurationUpdateSpec(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	current entity.Spec,
	next entity.Spec,
	detach bool,
) (entity.Spec, error) {
	currentConfigured, currentOK := current.(entity.ConfiguredSpec)
	nextConfigured, nextOK := next.(entity.ConfiguredSpec)
	if currentOK != nextOK {
		return nil, errs.ErrStateConflict
	}
	if !currentOK {
		if detach {
			return nil, errs.ErrInvalidInput
		}
		return next, nil
	}
	currentOwnership := currentConfigured.ConfigurationOwnership()
	nextOwnership := nextConfigured.ConfigurationOwnership()
	if detach {
		if currentOwnership.ManagedBy != "GIT" {
			return nil, errs.ErrStateConflict
		}
		if err := requireDurablePermission(
			ctx,
			tx,
			principal,
			permissionDetachConfiguration,
		); err != nil {
			return nil, err
		}
		detached, err := entity.WithConfigurationOwnership(
			next,
			entity.ConfigurationOwnership{ManagedBy: "UI"},
		)
		if err != nil {
			return nil, errs.ErrStateConflict
		}
		return detached, nil
	}
	switch currentOwnership.ManagedBy {
	case "UI":
		if nextOwnership.ManagedBy == "UI" {
			// SourceRef/SourceRevision — server-owned copy/detach lineage. Обычный
			// UI update не может стереть или переписать происхождение.
			if nextOwnership.SourceRef != "" &&
				(nextOwnership.SourceRef != currentOwnership.SourceRef ||
					nextOwnership.SourceRevision != currentOwnership.SourceRevision) {
				return nil, errs.ErrStateConflict
			}
			return entity.WithConfigurationOwnership(next, currentOwnership)
		}
		return nil, errs.ErrStateConflict
	case "GIT":
		if nextOwnership.ManagedBy != "GIT" ||
			nextOwnership.SourceRef != currentOwnership.SourceRef ||
			nextOwnership.SourceRevision <= currentOwnership.SourceRevision ||
			nextOwnership.SourceSHA256 == currentOwnership.SourceSHA256 {
			return nil, errs.ErrStateConflict
		}
	default:
		return nil, errs.ErrStateConflict
	}
	if err := requireDurablePermission(
		ctx,
		tx,
		principal,
		permissionApplyGitConfiguration,
	); err != nil {
		return nil, err
	}
	return next, nil
}

func authorizeGitManagedMutation(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	spec entity.Spec,
) error {
	configured, ok := spec.(entity.ConfiguredSpec)
	if !ok || configured.ConfigurationOwnership().ManagedBy == "UI" {
		return nil
	}
	if configured.ConfigurationOwnership().ManagedBy != "GIT" {
		return errs.ErrStateConflict
	}
	return requireDurablePermission(
		ctx,
		tx,
		principal,
		permissionApplyGitConfiguration,
	)
}

func requireDurablePermission(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	permission string,
) error {
	permissions, err := tx.ActorPermissions(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
		principal.ActorID,
	)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, permission) {
		return errs.ErrPermissionDenied
	}
	return nil
}

func authoritativeProject(
	principal value.Principal,
	kind enum.Kind,
	resourceID string,
) (string, error) {
	if kind == enum.KindProject {
		if principal.ProjectID != "" {
			return "", errs.ErrPermissionDenied
		}
		return resourceID, nil
	}
	if principal.ProjectID == "" {
		return "", errs.ErrPermissionDenied
	}
	return principal.ProjectID, nil
}
