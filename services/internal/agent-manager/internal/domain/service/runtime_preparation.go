package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	runtimePrepareReasonRetryable = "runtime_prepare_retryable"
	runtimePrepareFailureCode     = "runtime_prepare_failed"
	runtimeMaterializationPending = "runtime_materialization_pending"
	runtimeJobReasonRetryable     = "runtime_job_retryable"
	runtimeJobFailureCode         = "runtime_job_failed"
	runtimePrepareDefaultMode     = RuntimeModeFullEnv
	runtimePrepareSummaryLimit    = 512
	runtimeJobSafeRefLimit        = 512
	runtimeJobSafeKindLimit       = 64
	runtimeJobSecretRefLimit      = 16
	runtimeJobReportingRefLimit   = 8
)

var (
	guidanceLocalNamePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	runtimeJobSHA256DigestPattern = regexp.MustCompile(`(?i)^sha256:[0-9a-f]{64}$`)
)

const runtimeJobUnsafeMarkerList = "raw_provider_payload|provider_payload|prompt_text|prompt_body|prompt_template|transcript|tool_input|tool_output|workspace_path|kubeconfig|secret_value|token=|authorization|stdout|stderr|large_log|-----begin|bearer "

// RuntimePreparationError carries a safe failure classification across the runtime-manager port.
type RuntimePreparationError struct {
	Retryable   bool
	Code        string
	SafeMessage string
}

func (e *RuntimePreparationError) Error() string {
	if e == nil {
		return ""
	}
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "runtime_prepare_failed"
	}
	return code
}

// NewRuntimePreparationError creates a classified runtime preparation error with safe text only.
func NewRuntimePreparationError(retryable bool, code string, safeMessage string) error {
	return &RuntimePreparationError{
		Retryable:   retryable,
		Code:        strings.TrimSpace(code),
		SafeMessage: safeDiagnosticText(safeMessage),
	}
}

// RuntimeJobError переносит безопасную классификацию ошибки через job-порт runtime-manager.
type RuntimeJobError struct {
	Retryable   bool
	Code        string
	SafeMessage string
}

func (e *RuntimeJobError) Error() string {
	if e == nil {
		return ""
	}
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "runtime_job_failed"
	}
	return code
}

// NewRuntimeJobError создаёт классифицированную ошибку постановки runtime job только с безопасным текстом.
func NewRuntimeJobError(retryable bool, code string, safeMessage string) error {
	return &RuntimeJobError{
		Retryable:   retryable,
		Code:        strings.TrimSpace(code),
		SafeMessage: safeDiagnosticText(safeMessage),
	}
}

func (s *Service) prepareRuntimeForRun(
	ctx context.Context,
	meta value.CommandMeta,
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
) (entity.AgentRun, error) {
	if !s.runtimePreparationEnabled {
		return run, nil
	}
	request, err := s.runtimePreparationInputForRun(ctx, meta, session, role, promptVersion, run)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	result, err := s.runtimePreparer.PrepareRuntime(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	if s.runtimeJobDispatchEnabled {
		return s.dispatchRuntimeJobForRun(ctx, meta, session, role, promptVersion, run, result)
	}
	return s.recordRuntimePreparationSuccess(ctx, meta, run, result)
}

func (s *Service) retryRuntimeJobDispatchForReplay(ctx context.Context, meta value.CommandMeta, run entity.AgentRun) (entity.AgentRun, error) {
	if !s.runtimePreparationEnabled || !s.runtimeJobDispatchEnabled || strings.TrimSpace(run.RuntimeContext.JobRef) != "" || terminalRunStatus(run.Status) {
		return run, nil
	}
	session, role, promptVersion, err := s.runtimeReplayContext(ctx, run)
	if err != nil {
		return run, nil
	}
	request, err := s.runtimePreparationInputForRun(ctx, meta, session, role, promptVersion, run)
	if err != nil {
		return run, nil
	}
	result, err := s.runtimePreparer.PrepareRuntime(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		failure := classifyRuntimePreparationFailure(err)
		if failure.retryable {
			return run, nil
		}
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	if !runtimePreparationReadyForJob(result) {
		if err := runtimePreparationTerminalFailure(result); err != nil {
			runtimeContext, contextErr := runtimeContextFromPreparation(result)
			if contextErr != nil {
				return s.recordRuntimePreparationFailure(ctx, meta, run, err)
			}
			return s.recordRuntimePreparationFailureWithContext(ctx, meta, run, runtimeContext, err)
		}
		return run, nil
	}
	return s.dispatchRuntimeJobForRun(ctx, meta, session, role, promptVersion, run, result)
}

func terminalRunStatus(status enum.AgentRunStatus) bool {
	switch status {
	case enum.AgentRunStatusCompleted, enum.AgentRunStatusFailed, enum.AgentRunStatusCancelled:
		return true
	default:
		return false
	}
}

func (s *Service) runtimeReplayContext(ctx context.Context, run entity.AgentRun) (entity.AgentSession, entity.RoleProfile, entity.PromptTemplateVersion, error) {
	session, err := s.repository.GetAgentSession(ctx, run.SessionID)
	if err != nil {
		return entity.AgentSession{}, entity.RoleProfile{}, entity.PromptTemplateVersion{}, err
	}
	role, err := s.repository.GetRoleProfile(ctx, run.RoleProfileID)
	if err != nil {
		return entity.AgentSession{}, entity.RoleProfile{}, entity.PromptTemplateVersion{}, err
	}
	promptVersion, err := s.repository.GetPromptTemplateVersion(ctx, run.PromptTemplateVersionID)
	if err != nil {
		return entity.AgentSession{}, entity.RoleProfile{}, entity.PromptTemplateVersion{}, err
	}
	return session, role, promptVersion, nil
}

func (s *Service) runtimePreparationInputForRun(
	ctx context.Context,
	meta value.CommandMeta,
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
) (RuntimePreparationInput, error) {
	selection, err := workspacePolicySelection(session.Scope)
	if err != nil {
		return RuntimePreparationInput{}, err
	}
	policy, err := s.workspacePolicyResolver.ResolveWorkspacePolicy(ctx, WorkspacePolicyResolutionInput{
		Meta:                    meta,
		ProjectID:               selection.ProjectID,
		RepositoryIDs:           selection.RepositoryIDs,
		ServiceKeys:             selection.ServiceKeys,
		IncludeGuidancePackages: true,
	})
	if err != nil {
		return RuntimePreparationInput{}, err
	}
	if err := validateGuidanceAllowedByPolicy(policy, run.GuidanceRefs); err != nil {
		return RuntimePreparationInput{}, err
	}
	return runtimePreparationRequest(meta, session, role, promptVersion, run, policy)
}

type workspacePolicySelectionResult struct {
	ProjectID     uuid.UUID
	RepositoryIDs []uuid.UUID
	ServiceKeys   []string
}

func workspacePolicySelection(scope value.ScopeRef) (workspacePolicySelectionResult, error) {
	if strings.TrimSpace(scope.Type) != string(enum.AgentScopeTypeProject) {
		return workspacePolicySelectionResult{}, errs.ErrPreconditionFailed
	}
	projectID, err := uuid.Parse(strings.TrimSpace(scope.Ref))
	if err != nil || projectID == uuid.Nil {
		return workspacePolicySelectionResult{}, errs.ErrPreconditionFailed
	}
	return workspacePolicySelectionResult{ProjectID: projectID}, nil
}

func runtimePreparationRequest(
	meta value.CommandMeta,
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
	policy WorkspacePolicySnapshot,
) (RuntimePreparationInput, error) {
	runtimeProfile := strings.TrimSpace(role.RuntimeProfile)
	if runtimeProfile == "" {
		return RuntimePreparationInput{}, errs.ErrPreconditionFailed
	}
	runtimePolicy, err := runtimeWorkspacePolicy(session, role, promptVersion, run, policy)
	if err != nil {
		return RuntimePreparationInput{}, err
	}
	return RuntimePreparationInput{
		Meta:            runtimePrepareCommandMeta(meta, run.ID),
		AgentRunID:      run.ID,
		RuntimeProfile:  runtimeProfile,
		RuntimeMode:     runtimePrepareDefaultMode,
		WorkspacePolicy: runtimePolicy,
		PlacementConstraints: RuntimePlacementConstraints{
			ProjectID:      policy.ProjectID,
			RepositoryIDs:  repositoryIDsFromRuntimeSources(runtimePolicy.Sources),
			RuntimeProfile: runtimeProfile,
			MetadataJSON:   mustSafeJSON(placementMetadata(run, role)),
		},
	}, nil
}

func runtimeWorkspacePolicy(
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
	policy WorkspacePolicySnapshot,
) (RuntimeWorkspacePolicy, error) {
	sources := make([]RuntimeWorkspaceSource, 0, len(policy.CodeSources)+len(policy.DocumentationSources)+len(run.GuidanceRefs)+1)
	for _, source := range policy.CodeSources {
		runtimeSource, err := codeRuntimeSource(source)
		if err != nil {
			return RuntimeWorkspacePolicy{}, err
		}
		sources = append(sources, runtimeSource)
	}
	for _, source := range policy.DocumentationSources {
		runtimeSource, err := documentationRuntimeSource(source)
		if err != nil {
			return RuntimeWorkspacePolicy{}, err
		}
		sources = append(sources, runtimeSource)
	}
	guidanceSources, err := guidanceRuntimeSources(run.GuidanceRefs)
	if err != nil {
		return RuntimeWorkspacePolicy{}, err
	}
	sources = append(sources, guidanceSources...)
	generated, err := generatedContextRuntimeSource(session, role, promptVersion, run, guidanceSources)
	if err != nil {
		return RuntimeWorkspacePolicy{}, err
	}
	sources = append(sources, generated)
	sources, err = normalizeRuntimeSources(sources)
	if err != nil {
		return RuntimeWorkspacePolicy{}, err
	}
	result := RuntimeWorkspacePolicy{
		ProjectID:               policy.ProjectID,
		PolicyVersion:           policy.PolicyVersion,
		Sources:                 sources,
		ActivePolicyOverrideIDs: activePolicyOverrideIDs(policy.ActivePolicyOverrides),
	}
	digest, err := runtimeWorkspacePolicyDigest(result)
	if err != nil {
		return RuntimeWorkspacePolicy{}, err
	}
	result.PolicyDigest = digest
	return result, nil
}

func codeRuntimeSource(source WorkspaceCodeSource) (RuntimeWorkspaceSource, error) {
	if source.RepositoryID == uuid.Nil {
		return RuntimeWorkspaceSource{}, errs.ErrPreconditionFailed
	}
	metadata, err := safeJSON(codeSourceMetadata{
		Owner:         "project-catalog",
		SourceKind:    WorkspaceSourceKindCode,
		DefaultBranch: strings.TrimSpace(source.DefaultBranch),
	})
	if err != nil {
		return RuntimeWorkspaceSource{}, err
	}
	return RuntimeWorkspaceSource{
		SourceID:      source.RepositoryID.String(),
		Kind:          WorkspaceSourceKindCode,
		RepositoryID:  uuidPtr(source.RepositoryID),
		Provider:      strings.TrimSpace(source.Provider),
		ProviderOwner: strings.TrimSpace(source.ProviderOwner),
		ProviderName:  strings.TrimSpace(source.ProviderName),
		SourceRef:     strings.TrimSpace(source.DefaultBranch),
		LocalPath:     strings.TrimSpace(source.LocalPath),
		AccessMode:    normalizeAccessMode(source.AccessMode),
		MetadataJSON:  metadata,
	}, nil
}

func documentationRuntimeSource(source WorkspaceDocumentationSource) (RuntimeWorkspaceSource, error) {
	if source.DocumentationSourceID == uuid.Nil {
		return RuntimeWorkspaceSource{}, errs.ErrPreconditionFailed
	}
	metadata, err := safeJSON(documentationSourceMetadata{
		Owner:      "project-catalog",
		SourceKind: WorkspaceSourceKindDocumentation,
		ScopeType:  strings.TrimSpace(source.ScopeType),
		ScopeID:    strings.TrimSpace(source.ScopeID),
	})
	if err != nil {
		return RuntimeWorkspaceSource{}, err
	}
	return RuntimeWorkspaceSource{
		SourceID:     source.DocumentationSourceID.String(),
		Kind:         WorkspaceSourceKindDocumentation,
		RepositoryID: source.RepositoryID,
		LocalPath:    strings.TrimSpace(source.LocalPath),
		AccessMode:   normalizeAccessMode(source.AccessMode),
		MetadataJSON: metadata,
	}, nil
}

func guidanceRuntimeSources(refs []value.GuidanceRef) ([]RuntimeWorkspaceSource, error) {
	result := make([]RuntimeWorkspaceSource, 0, len(refs))
	names := make(map[string]string, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.PackageInstallationRef) == "" ||
			strings.TrimSpace(ref.PackageVersionRef) == "" ||
			strings.TrimSpace(ref.ManifestDigest) == "" {
			return nil, errs.ErrPreconditionFailed
		}
		localName := guidanceSafeLocalName(ref)
		if previous, ok := names[localName]; ok && previous != ref.PackageInstallationRef {
			return nil, errs.ErrPreconditionFailed
		}
		names[localName] = ref.PackageInstallationRef
		metadata, err := safeJSON(guidanceSourceMetadata{
			PackageInstallationRef: strings.TrimSpace(ref.PackageInstallationRef),
			PackageVersionRef:      strings.TrimSpace(ref.PackageVersionRef),
			ManifestDigest:         strings.TrimSpace(ref.ManifestDigest),
			PackageRef:             strings.TrimSpace(ref.PackageRef),
			PackageSlug:            strings.TrimSpace(ref.PackageSlug),
			PackageVersionLabel:    strings.TrimSpace(ref.PackageVersionLabel),
			SafeLocalName:          localName,
			CapabilityRef:          strings.TrimSpace(ref.CapabilityRef),
			CapabilityKind:         strings.TrimSpace(ref.CapabilityKind),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, RuntimeWorkspaceSource{
			SourceID:     "guidance:" + strings.TrimSpace(ref.PackageInstallationRef),
			Kind:         WorkspaceSourceKindGuidancePackage,
			SourceRef:    strings.TrimSpace(ref.SourceRef),
			LocalPath:    ".kodex/guidance/" + localName,
			AccessMode:   WorkspaceSourceAccessRead,
			Digest:       strings.TrimSpace(ref.ManifestDigest),
			MetadataJSON: metadata,
		})
	}
	return result, nil
}

func generatedContextRuntimeSource(
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
	guidanceSources []RuntimeWorkspaceSource,
) (RuntimeWorkspaceSource, error) {
	guidancePaths := make([]string, 0, len(guidanceSources))
	for _, source := range guidanceSources {
		guidancePaths = append(guidancePaths, source.LocalPath)
	}
	sort.Strings(guidancePaths)
	metadata, err := safeJSON(generatedContextMetadata{
		AgentRunID:              run.ID.String(),
		AgentSessionID:          session.ID.String(),
		FlowVersionID:           optionalUUIDText(run.FlowVersionID),
		StageID:                 optionalUUIDText(run.StageID),
		RoleProfileID:           role.ID.String(),
		RoleProfileVersion:      role.Version,
		RoleProfileDigest:       run.RoleProfileDigest,
		RoleKind:                string(role.RoleKind),
		RuntimeProfile:          strings.TrimSpace(role.RuntimeProfile),
		AllowedMCPTools:         sortedStrings(role.AllowedMCPTools),
		PromptTemplateVersionID: promptVersion.ID.String(),
		PromptTemplateDigest:    run.PromptTemplateDigest,
		ProviderTarget:          run.ProviderTarget,
		GuidanceLocalPaths:      guidancePaths,
		GuidanceRefCount:        len(run.GuidanceRefs),
	})
	if err != nil {
		return RuntimeWorkspaceSource{}, err
	}
	return RuntimeWorkspaceSource{
		SourceID:     "agent-run:" + run.ID.String(),
		Kind:         WorkspaceSourceKindGeneratedContext,
		SourceRef:    run.ID.String(),
		LocalPath:    ".kodex/context/agent-run.json",
		AccessMode:   WorkspaceSourceAccessRead,
		Digest:       digestText(metadata),
		MetadataJSON: metadata,
	}, nil
}

func validateGuidanceAllowedByPolicy(policy WorkspacePolicySnapshot, refs []value.GuidanceRef) error {
	if len(policy.GuidancePackageRefs) == 0 || len(refs) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(policy.GuidancePackageRefs))
	for _, ref := range policy.GuidancePackageRefs {
		trimmed := strings.TrimSpace(ref)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, ref := range refs {
		if _, ok := allowed[strings.TrimSpace(ref.PackageInstallationRef)]; !ok {
			return errs.ErrPreconditionFailed
		}
	}
	return nil
}

func (s *Service) recordRuntimePreparationSuccess(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	result RuntimePreparationResult,
) (entity.AgentRun, error) {
	runtimeContext, err := runtimeContextFromPreparation(result)
	if err != nil {
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	summary := runtimePreparationSuccessSummary(result)
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusStarting, runtimeContext, summary, "", "")
}

func (s *Service) dispatchRuntimeJobForRun(
	ctx context.Context,
	meta value.CommandMeta,
	session entity.AgentSession,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	run entity.AgentRun,
	result RuntimePreparationResult,
) (entity.AgentRun, error) {
	runtimeContext, err := runtimeContextFromPreparation(result)
	if err != nil {
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	if strings.TrimSpace(run.RuntimeContext.JobRef) != "" {
		runtimeContext.JobRef = strings.TrimSpace(run.RuntimeContext.JobRef)
		return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusStarting, runtimeContext, runtimePreparationSuccessSummary(result), "", "")
	}
	if err := runtimePreparationTerminalFailure(result); err != nil {
		return s.recordRuntimePreparationFailureWithContext(ctx, meta, run, runtimeContext, err)
	}
	if !runtimePreparationReadyForJob(result) {
		return s.recordRuntimeMaterializationPending(ctx, meta, run, runtimeContext, result)
	}
	spec, err := s.agentRunExecutionSpec(ctx, session, run, role, promptVersion, result)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimeOperationFailure(ctx, meta, run, runtimeContext, classifyRuntimeJobFailure(err), runtimeJobReasonRetryable, runtimeJobFailureCode)
	}
	job, err := s.runtimeJobCreator.CreateAgentRunJob(ctx, RuntimeJobInput{
		Meta:          runtimeJobCommandMeta(meta, run.ID),
		AgentRunID:    run.ID,
		SlotRef:       runtimeContext.SlotRef,
		ExecutionSpec: spec,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimeOperationFailure(ctx, meta, run, runtimeContext, classifyRuntimeJobFailure(err), runtimeJobReasonRetryable, runtimeJobFailureCode)
	}
	return s.recordRuntimeJobSuccess(ctx, meta, run, runtimeContext, result, job)
}

func runtimePreparationReadyForJob(result RuntimePreparationResult) bool {
	return strings.TrimSpace(result.SlotStatus) == RuntimeSlotStatusReady &&
		strings.TrimSpace(result.WorkspaceMaterializationStatus) == RuntimeWorkspaceMaterializationStatusCompleted
}

func runtimePreparationTerminalFailure(result RuntimePreparationResult) error {
	if strings.TrimSpace(result.SlotStatus) == RuntimeSlotStatusFailed {
		return NewRuntimePreparationError(false, "runtime_slot_failed", "runtime-manager reported failed runtime slot")
	}
	switch strings.TrimSpace(result.WorkspaceMaterializationStatus) {
	case RuntimeWorkspaceMaterializationStatusFailed:
		return NewRuntimePreparationError(false, "runtime_materialization_failed", "runtime-manager reported failed workspace materialization")
	case RuntimeWorkspaceMaterializationStatusCancelled:
		return NewRuntimePreparationError(false, "runtime_materialization_cancelled", "runtime-manager reported cancelled workspace materialization")
	default:
		return nil
	}
}

func (s *Service) agentRunExecutionSpec(
	ctx context.Context,
	session entity.AgentSession,
	run entity.AgentRun,
	role entity.RoleProfile,
	promptVersion entity.PromptTemplateVersion,
	result RuntimePreparationResult,
) (AgentRunExecutionSpec, error) {
	slotID, err := requiredRuntimeUUID(result.SlotRef)
	if err != nil {
		return AgentRunExecutionSpec{}, missingRuntimeJobSpecDependency()
	}
	materializationID, err := requiredRuntimeUUID(result.WorkspaceRef)
	if err != nil {
		return AgentRunExecutionSpec{}, missingRuntimeJobSpecDependency()
	}
	fingerprint := strings.TrimSpace(result.MaterializationFingerprint)
	contextDigest := strings.TrimSpace(result.ContextDigest)
	if !safeRuntimeJobRef(fingerprint, true) || !safeRuntimeJobRef(contextDigest, true) {
		return AgentRunExecutionSpec{}, missingRuntimeJobSpecDependency()
	}
	runnerImageRef := strings.TrimSpace(s.runtimeJobRunnerImageRef)
	if !safeRuntimeJobRef(runnerImageRef, true) {
		return AgentRunExecutionSpec{}, NewRuntimeJobError(false, "failed_precondition", "agent run runner image ref is not configured")
	}
	allowedSecretRefs, err := normalizeRuntimeJobRefs(s.runtimeJobAllowedSecretRefs, runtimeJobSecretRefLimit)
	if err != nil {
		return AgentRunExecutionSpec{}, NewRuntimeJobError(false, "failed_precondition", "agent run allowed secret refs are invalid")
	}
	spec := AgentRunExecutionSpec{
		AgentRunID:                         run.ID,
		SlotID:                             slotID,
		ExpectedMaterializationID:          materializationID,
		ExpectedMaterializationFingerprint: fingerprint,
		WorkspaceRef:                       runtimeWorkspaceRef(materializationID),
		WorkspaceMountRef:                  runtimeWorkspaceMountRef(slotID),
		ContextRef:                         runtimeContextFileRef(materializationID),
		ContextDigest:                      contextDigest,
		RunnerProfileRef:                   runnerProfileRef(role.RuntimeProfile),
		RunnerImageRef:                     runnerImageRef,
		RunnerMode:                         RuntimeJobRunnerModeCodexAgent,
		AllowedSecretRefs:                  allowedSecretRefs,
		ReportingTargetRefs:                runtimeJobReportingTargetRefs(run.ID),
	}
	if contextRef := strings.TrimSpace(result.ContextRef); contextRef != "" && !strings.HasPrefix(contextRef, "sha256:") {
		spec.ContextRef = contextRef
	}
	codexSpec, err := s.codexSessionExecutionSpec(ctx, session, run, promptVersion, materializationID, spec)
	if err != nil {
		return AgentRunExecutionSpec{}, err
	}
	spec.CodexSessionExecutionSpec = &codexSpec
	if err := validateAgentRunExecutionSpec(spec); err != nil {
		return AgentRunExecutionSpec{}, err
	}
	return spec, nil
}

func validateAgentRunExecutionSpec(spec AgentRunExecutionSpec) error {
	if spec.AgentRunID == uuid.Nil || spec.SlotID == uuid.Nil || spec.ExpectedMaterializationID == uuid.Nil {
		return missingRuntimeJobSpecDependency()
	}
	requiredRefs := []string{
		spec.ExpectedMaterializationFingerprint,
		spec.WorkspaceRef,
		spec.WorkspaceMountRef,
		spec.ContextRef,
		spec.ContextDigest,
		spec.RunnerProfileRef,
		spec.RunnerImageRef,
	}
	for _, ref := range requiredRefs {
		if !safeRuntimeJobRef(ref, true) {
			return missingRuntimeJobSpecDependency()
		}
	}
	if spec.RunnerMode != RuntimeJobRunnerModeCodexAgent {
		return NewRuntimeJobError(false, "failed_precondition", "agent run runner mode is invalid")
	}
	if _, err := normalizeRuntimeJobRefs(spec.AllowedSecretRefs, runtimeJobSecretRefLimit); err != nil {
		return NewRuntimeJobError(false, "failed_precondition", "agent run allowed secret refs are invalid")
	}
	if _, err := normalizeRuntimeJobRefs(spec.ReportingTargetRefs, runtimeJobReportingRefLimit); err != nil {
		return missingRuntimeJobSpecDependency()
	}
	if spec.CodexSessionExecutionSpec == nil {
		return missingCodexSessionExecutionDependency()
	}
	if err := validateCodexSessionExecutionSpec(*spec.CodexSessionExecutionSpec, spec); err != nil {
		return err
	}
	return nil
}

func missingRuntimeJobSpecDependency() error {
	return NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned incomplete agent run execution refs")
}

func missingCodexSessionExecutionDependency() error {
	return NewRuntimeJobError(true, "execution_input_unavailable", "codex session execution input is unavailable")
}

func (s *Service) codexSessionExecutionSpec(
	ctx context.Context,
	session entity.AgentSession,
	run entity.AgentRun,
	promptVersion entity.PromptTemplateVersion,
	materializationID uuid.UUID,
	agentRunSpec AgentRunExecutionSpec,
) (CodexSessionExecutionSpec, error) {
	cfg := s.codexSessionExecution
	instructionRef := strings.TrimSpace(promptVersion.TemplateObject.ObjectURI)
	instructionDigest := strings.TrimSpace(promptVersion.TemplateObject.ObjectDigest)
	resultSchemaRef := strings.TrimSpace(cfg.ResultSchemaRef)
	resultSchemaDigest := strings.TrimSpace(cfg.ResultSchemaDigest)
	hookEndpointRef := strings.TrimSpace(cfg.HookEndpointRef)
	if instructionRef == "" || instructionDigest == "" || resultSchemaRef == "" || resultSchemaDigest == "" || hookEndpointRef == "" {
		return CodexSessionExecutionSpec{}, missingCodexSessionExecutionDependency()
	}
	sessionSnapshotRef, err := s.codexSessionSnapshotRef(ctx, session)
	if err != nil {
		return CodexSessionExecutionSpec{}, err
	}
	workspaceSnapshotRef := ""
	if sessionSnapshotRef == "" {
		workspaceSnapshotRef = runtimeWorkspaceSnapshotRef(materializationID)
	}
	spec := CodexSessionExecutionSpec{
		CodexSessionExecutionInputRefs: CodexSessionExecutionInputRefs{
			InstructionObjectRef:    instructionRef,
			InstructionObjectDigest: instructionDigest,
			ResultSchemaRef:         resultSchemaRef,
			ResultSchemaDigest:      resultSchemaDigest,
			SessionSnapshotRef:      sessionSnapshotRef,
			WorkspaceSnapshotRef:    workspaceSnapshotRef,
			HookEndpointRef:         hookEndpointRef,
			CallbackRefs:            runtimeJobCallbackRefs(run.ID),
		},
		CodexSessionExecutionIORefs: CodexSessionExecutionIORefs{
			TimeoutSeconds:    cfg.TimeoutSeconds,
			RunnerProfileRef:  agentRunSpec.RunnerProfileRef,
			RunnerMode:        RuntimeJobRunnerModeCodexAgent,
			OutputRefs:        runtimeJobOutputRefs(run.ID),
			ResultRefs:        runtimeJobResultRefs(run.ID),
			AllowedSecretRefs: append([]AgentRunExecutionRef(nil), agentRunSpec.AllowedSecretRefs...),
		},
	}
	if err := validateCodexSessionExecutionSpec(spec, agentRunSpec); err != nil {
		return CodexSessionExecutionSpec{}, err
	}
	return spec, nil
}

func (s *Service) codexSessionSnapshotRef(ctx context.Context, session entity.AgentSession) (string, error) {
	if session.LatestStateSnapshotID == nil {
		return "", nil
	}
	snapshot, err := s.repository.GetSessionStateSnapshot(ctx, *session.LatestStateSnapshotID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", missingCodexSessionExecutionDependency()
	}
	ref := strings.TrimSpace(snapshot.Object.ObjectURI)
	if ref == "" {
		return "", missingCodexSessionExecutionDependency()
	}
	if !safeRuntimeJobRef(ref, true) {
		return "", invalidCodexSessionExecutionSpec()
	}
	return ref, nil
}

func validateCodexSessionExecutionSpec(spec CodexSessionExecutionSpec, agentRunSpec AgentRunExecutionSpec) error {
	requiredRefs := []string{
		spec.InstructionObjectRef,
		spec.ResultSchemaRef,
		spec.HookEndpointRef,
		spec.RunnerProfileRef,
	}
	for _, ref := range requiredRefs {
		if !safeRuntimeJobRef(ref, true) {
			return invalidCodexSessionExecutionSpec()
		}
	}
	if !validRuntimeJobSHA256Digest(spec.InstructionObjectDigest) || !validRuntimeJobSHA256Digest(spec.ResultSchemaDigest) {
		return invalidCodexSessionExecutionSpec()
	}
	if spec.SessionSnapshotRef == "" && spec.WorkspaceSnapshotRef == "" {
		return missingCodexSessionExecutionDependency()
	}
	if !safeRuntimeJobRef(spec.SessionSnapshotRef, false) || !safeRuntimeJobRef(spec.WorkspaceSnapshotRef, false) {
		return invalidCodexSessionExecutionSpec()
	}
	if spec.TimeoutSeconds <= 0 {
		return missingCodexSessionExecutionDependency()
	}
	if spec.TimeoutSeconds > 24*60*60 {
		return invalidCodexSessionExecutionSpec()
	}
	if spec.RunnerProfileRef != agentRunSpec.RunnerProfileRef || spec.RunnerMode != RuntimeJobRunnerModeCodexAgent {
		return invalidCodexSessionExecutionSpec()
	}
	if _, err := normalizeRuntimeJobRefs(spec.CallbackRefs, runtimeJobReportingRefLimit); err != nil || len(spec.CallbackRefs) == 0 {
		return invalidCodexSessionExecutionSpec()
	}
	if _, err := normalizeRuntimeJobRefs(spec.OutputRefs, runtimeJobReportingRefLimit); err != nil || len(spec.OutputRefs) == 0 {
		return invalidCodexSessionExecutionSpec()
	}
	if _, err := normalizeRuntimeJobRefs(spec.ResultRefs, runtimeJobReportingRefLimit); err != nil || len(spec.ResultRefs) == 0 {
		return invalidCodexSessionExecutionSpec()
	}
	if _, err := normalizeRuntimeJobRefs(spec.AllowedSecretRefs, runtimeJobSecretRefLimit); err != nil {
		return invalidCodexSessionExecutionSpec()
	}
	return nil
}

func invalidCodexSessionExecutionSpec() error {
	return NewRuntimeJobError(false, "failed_precondition", "codex session execution refs are invalid")
}

func requiredRuntimeUUID(ref string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(ref))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func runtimeWorkspaceRef(materializationID uuid.UUID) string {
	return "runtime://workspace-materializations/" + materializationID.String()
}

func runtimeWorkspaceMountRef(slotID uuid.UUID) string {
	return "runtime://slots/" + slotID.String() + "/workspace-mount"
}

func runtimeContextFileRef(materializationID uuid.UUID) string {
	return "runtime://workspace-materializations/" + materializationID.String() + "/context/agent-run.json"
}

func runtimeWorkspaceSnapshotRef(materializationID uuid.UUID) string {
	return "runtime://workspace-materializations/" + materializationID.String() + "/snapshots/workspace"
}

func runnerProfileRef(runtimeProfile string) string {
	return "runner-profile://" + strings.TrimSpace(runtimeProfile)
}

func runtimeJobReportingTargetRefs(runID uuid.UUID) []AgentRunExecutionRef {
	return []AgentRunExecutionRef{
		{Kind: "agent_run_state", Ref: "agent-manager://runs/" + runID.String()},
		{Kind: "agent_activity", Ref: "agent-manager://runs/" + runID.String() + "/activities"},
	}
}

func runtimeJobCallbackRefs(runID uuid.UUID) []AgentRunExecutionRef {
	return []AgentRunExecutionRef{
		{Kind: "agent_run_state", Ref: "agent-manager://runs/" + runID.String()},
		{Kind: "agent_activity", Ref: "agent-manager://runs/" + runID.String() + "/activities"},
	}
}

func runtimeJobOutputRefs(runID uuid.UUID) []AgentRunExecutionRef {
	return []AgentRunExecutionRef{
		{Kind: "codex_output", Ref: "agent-manager://runs/" + runID.String() + "/codex-output"},
	}
}

func runtimeJobResultRefs(runID uuid.UUID) []AgentRunExecutionRef {
	return []AgentRunExecutionRef{
		{Kind: "codex_result", Ref: "agent-manager://runs/" + runID.String() + "/codex-result"},
	}
}

func normalizeRuntimeJobRefs(refs []AgentRunExecutionRef, limit int) ([]AgentRunExecutionRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if len(refs) > limit {
		return nil, errs.ErrInvalidArgument
	}
	normalized := make([]AgentRunExecutionRef, 0, len(refs))
	for _, ref := range refs {
		item := AgentRunExecutionRef{Kind: strings.TrimSpace(ref.Kind), Ref: strings.TrimSpace(ref.Ref)}
		if !safeRuntimeJobKind(item.Kind) || !safeRuntimeJobRef(item.Ref, true) {
			return nil, errs.ErrInvalidArgument
		}
		normalized = append(normalized, item)
	}
	sortAgentRunExecutionRefs(normalized)
	return normalized, nil
}

func sortAgentRunExecutionRefs(refs []AgentRunExecutionRef) {
	sort.SliceStable(refs, func(left, right int) bool {
		return agentRunExecutionRefSortKey(refs[left]) < agentRunExecutionRefSortKey(refs[right])
	})
}

func agentRunExecutionRefSortKey(ref AgentRunExecutionRef) string {
	return ref.Kind + ":" + ref.Ref
}

func safeRuntimeJobKind(value string) bool {
	if value == "" || len(value) > runtimeJobSafeKindLimit || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t {}") {
		return false
	}
	return !unsafeRuntimeJobText(value)
}

func safeRuntimeJobRef(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > runtimeJobSafeRefLimit || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t{}") {
		return false
	}
	return !unsafeRuntimeJobText(value)
}

func validRuntimeJobSHA256Digest(value string) bool {
	return runtimeJobSHA256DigestPattern.MatchString(strings.TrimSpace(value))
}

func unsafeRuntimeJobText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range strings.Split(runtimeJobUnsafeMarkerList, "|") {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func runtimeContextFromPreparation(result RuntimePreparationResult) (value.RuntimeContextRef, error) {
	contextRef := strings.TrimSpace(result.ContextRef)
	if contextRef == "" {
		contextRef = strings.TrimSpace(result.MaterializationFingerprint)
	}
	runtimeContext := value.RuntimeContextRef{
		SlotRef:      strings.TrimSpace(result.SlotRef),
		WorkspaceRef: strings.TrimSpace(result.WorkspaceRef),
		ContextRef:   contextRef,
	}
	if strings.TrimSpace(runtimeContext.SlotRef) == "" {
		return value.RuntimeContextRef{}, errs.ErrDependencyUnavailable
	}
	return runtimeContext, nil
}

func (s *Service) recordRuntimeJobSuccess(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	runtimeContext value.RuntimeContextRef,
	preparation RuntimePreparationResult,
	job RuntimeJobResult,
) (entity.AgentRun, error) {
	runtimeContext.JobRef = strings.TrimSpace(job.JobRef)
	if strings.TrimSpace(runtimeContext.JobRef) == "" {
		return s.recordRuntimeOperationFailure(ctx, meta, run, runtimeContext, classifyRuntimeJobFailure(errs.ErrDependencyUnavailable), runtimeJobReasonRetryable, runtimeJobFailureCode)
	}
	summary := runtimeJobSuccessSummary(preparation, job)
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusStarting, runtimeContext, summary, "", "")
}

func (s *Service) recordRuntimeMaterializationPending(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	runtimeContext value.RuntimeContextRef,
	preparation RuntimePreparationResult,
) (entity.AgentRun, error) {
	summary := safeDiagnosticText(runtimePreparationSuccessSummary(preparation) + "; runtime materialization pending")
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusWaiting, runtimeContext, summary, "", runtimeMaterializationPending)
}

func (s *Service) recordRuntimePreparationFailure(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	err error,
) (entity.AgentRun, error) {
	return s.recordRuntimePreparationFailureWithContext(ctx, meta, run, value.RuntimeContextRef{}, err)
}

func (s *Service) recordRuntimePreparationFailureWithContext(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	runtimeContext value.RuntimeContextRef,
	err error,
) (entity.AgentRun, error) {
	failure := classifyRuntimePreparationFailure(err)
	return s.recordRuntimeOperationFailure(ctx, meta, run, runtimeContext, failure, runtimePrepareReasonRetryable, runtimePrepareFailureCode)
}

func (s *Service) recordRuntimeOperationFailure(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	runtimeContext value.RuntimeContextRef,
	failure runtimeOperationFailure,
	retryReasonCode string,
	failureCode string,
) (entity.AgentRun, error) {
	if failure.retryable {
		return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusWaiting, runtimeContext, failure.summary(), "", retryReasonCode)
	}
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusFailed, runtimeContext, failure.summary(), failureCode, "")
}

func (s *Service) recordRuntimePreparationState(
	ctx context.Context,
	baseMeta value.CommandMeta,
	run entity.AgentRun,
	status enum.AgentRunStatus,
	runtimeContext value.RuntimeContextRef,
	resultSummary string,
	failureCode string,
	reasonCode string,
) (entity.AgentRun, error) {
	if err := validateRunStatusTransition(run.Status, status); err != nil {
		return entity.AgentRun{}, err
	}
	now := s.clock.Now()
	previousVersion := run.Version
	previousStatus := string(run.Status)
	run.Status = status
	if runtimeContext != (value.RuntimeContextRef{}) {
		run.RuntimeContext = runtimeContext
	}
	run.ResultSummary = safeDiagnosticText(resultSummary)
	run.FailureCode = strings.TrimSpace(failureCode)
	if run.StartedAt == nil && (status == enum.AgentRunStatusStarting || status == enum.AgentRunStatusRunning) {
		run.StartedAt = &now
	}
	if run.FinishedAt == nil && isTerminalRunStatus(status) {
		run.FinishedAt = &now
	}
	if err := validateRunStatePayload(run, reasonCode); err != nil {
		return entity.AgentRun{}, err
	}
	run.Version++
	run.UpdatedAt = now
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run})
	if err != nil {
		return entity.AgentRun{}, err
	}
	result, err := commandResult(runtimeStateCommandMeta(baseMeta, run.ID, status, reasonCode), operationRecordRunState, enum.CommandAggregateTypeRun, run.ID, payload, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	event, err := runStateEvent(s.idGenerator.New(), previousStatus, run, reasonCode, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	return run, s.repository.UpdateAgentRunWithResult(ctx, run, previousVersion, result, event)
}

type runtimeOperationFailure struct {
	operation string
	retryable bool
	code      string
	message   string
}

func (f runtimeOperationFailure) summary() string {
	class := "permanent"
	if f.retryable {
		class = "retryable"
	}
	return fmt.Sprintf("%s %s: code=%s; message=%s", f.operation, class, f.code, f.message)
}

type runtimeFailureDefaults struct {
	operation           string
	defaultCode         string
	defaultMessage      string
	dependencyMessage   string
	conflictMessage     string
	invalidMessage      string
	notFoundMessage     string
	preconditionMessage string
}

func classifyRuntimePreparationFailure(err error) runtimeOperationFailure {
	var classified *RuntimePreparationError
	if errors.As(err, &classified) {
		return classifiedRuntimeFailure(runtimePrepareFailureDefaults(), classified.Retryable, classified.Code, classified.SafeMessage)
	}
	return classifyRuntimeFailure(err, runtimePrepareFailureDefaults())
}

func classifyRuntimeJobFailure(err error) runtimeOperationFailure {
	var classified *RuntimeJobError
	if errors.As(err, &classified) {
		return classifiedRuntimeFailure(runtimeJobFailureDefaults(), classified.Retryable, classified.Code, classified.SafeMessage)
	}
	return classifyRuntimeFailure(err, runtimeJobFailureDefaults())
}

func classifiedRuntimeFailure(defaults runtimeFailureDefaults, retryable bool, code string, message string) runtimeOperationFailure {
	return runtimeOperationFailure{
		operation: defaults.operation,
		retryable: retryable,
		code:      fallbackText(code, defaults.defaultCode),
		message:   fallbackText(message, defaults.defaultMessage),
	}
}

func classifyRuntimeFailure(err error, defaults runtimeFailureDefaults) runtimeOperationFailure {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, errs.ErrDependencyUnavailable):
		return runtimeOperationFailure{operation: defaults.operation, retryable: true, code: "dependency_unavailable", message: defaults.dependencyMessage}
	case errors.Is(err, errs.ErrConflict):
		return runtimeOperationFailure{operation: defaults.operation, retryable: true, code: "conflict", message: defaults.conflictMessage}
	case errors.Is(err, errs.ErrInvalidArgument):
		return runtimeOperationFailure{operation: defaults.operation, code: "invalid_argument", message: defaults.invalidMessage}
	case errors.Is(err, errs.ErrNotFound):
		return runtimeOperationFailure{operation: defaults.operation, code: "not_found", message: defaults.notFoundMessage}
	case errors.Is(err, errs.ErrPreconditionFailed):
		return runtimeOperationFailure{operation: defaults.operation, code: "failed_precondition", message: defaults.preconditionMessage}
	default:
		return runtimeOperationFailure{operation: defaults.operation, retryable: true, code: defaults.defaultCode, message: defaults.defaultMessage}
	}
}

func runtimePrepareFailureDefaults() runtimeFailureDefaults {
	return runtimeFailureDefaults{
		operation:           "runtime prepare",
		defaultCode:         "runtime_prepare_failed",
		defaultMessage:      "workspace preparation failed",
		dependencyMessage:   "workspace preparation dependency is temporarily unavailable",
		conflictMessage:     "workspace preparation is waiting for conflicting state to clear",
		invalidMessage:      "workspace preparation request is invalid",
		notFoundMessage:     "workspace preparation dependency was not found",
		preconditionMessage: "workspace preparation precondition failed",
	}
}

func runtimeJobFailureDefaults() runtimeFailureDefaults {
	return runtimeFailureDefaults{
		operation:           "runtime job",
		defaultCode:         "runtime_job_failed",
		defaultMessage:      "agent run job creation failed",
		dependencyMessage:   "runtime job dependency is temporarily unavailable",
		conflictMessage:     "runtime job creation is waiting for conflicting state to clear",
		invalidMessage:      "runtime job request is invalid",
		notFoundMessage:     "runtime job dependency was not found",
		preconditionMessage: "runtime job precondition failed",
	}
}

func runtimePreparationSuccessSummary(result RuntimePreparationResult) string {
	parts := []string{"runtime prepare started"}
	if slot := strings.TrimSpace(result.SlotRef); slot != "" {
		parts = append(parts, "slot="+slot)
	}
	if workspace := strings.TrimSpace(result.WorkspaceRef); workspace != "" {
		parts = append(parts, "workspace="+workspace)
	}
	if fingerprint := strings.TrimSpace(result.MaterializationFingerprint); fingerprint != "" {
		parts = append(parts, "fingerprint="+fingerprint)
	}
	if diagnostic := strings.TrimSpace(result.DiagnosticSummary); diagnostic != "" {
		parts = append(parts, "diagnostic="+diagnostic)
	}
	return safeDiagnosticText(strings.Join(parts, "; "))
}

func runtimeJobSuccessSummary(preparation RuntimePreparationResult, job RuntimeJobResult) string {
	parts := []string{runtimePreparationSuccessSummary(preparation), "runtime job created"}
	if ref := strings.TrimSpace(job.JobRef); ref != "" {
		parts = append(parts, "job="+ref)
	}
	if status := strings.TrimSpace(job.Status); status != "" {
		parts = append(parts, "job_status="+status)
	}
	if diagnostic := strings.TrimSpace(job.DiagnosticSummary); diagnostic != "" {
		parts = append(parts, "job_diagnostic="+diagnostic)
	}
	return safeDiagnosticText(strings.Join(parts, "; "))
}

func runtimePrepareCommandMeta(meta value.CommandMeta, runID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		CommandID: deterministicUUID("kodex-agent-manager:runtime-prepare:" + runID.String()),
		Actor:     meta.Actor,
	}
}

func runtimeJobCommandMeta(meta value.CommandMeta, runID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		CommandID: deterministicUUID("kodex-agent-manager:runtime-job:" + runID.String()),
		Actor:     meta.Actor,
	}
}

func runtimeStateCommandMeta(meta value.CommandMeta, runID uuid.UUID, status enum.AgentRunStatus, reasonCode string) value.CommandMeta {
	return value.CommandMeta{
		CommandID: deterministicUUID("kodex-agent-manager:runtime-prepare-state:" + runID.String() + ":" + string(status) + ":" + strings.TrimSpace(reasonCode)),
		Actor:     meta.Actor,
	}
}

func deterministicUUID(seed string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed))
}

func normalizeRuntimeSources(sources []RuntimeWorkspaceSource) ([]RuntimeWorkspaceSource, error) {
	normalized := make([]RuntimeWorkspaceSource, 0, len(sources))
	for _, source := range sources {
		item := source
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.Kind = strings.TrimSpace(item.Kind)
		item.Provider = strings.TrimSpace(item.Provider)
		item.ProviderOwner = strings.TrimSpace(item.ProviderOwner)
		item.ProviderName = strings.TrimSpace(item.ProviderName)
		item.SourceRef = strings.TrimSpace(item.SourceRef)
		item.CommitSHA = strings.TrimSpace(item.CommitSHA)
		item.LocalPath = strings.TrimSpace(item.LocalPath)
		item.AccessMode = normalizeAccessMode(item.AccessMode)
		item.Digest = strings.TrimSpace(item.Digest)
		metadata, err := normalizeJSONText(item.MetadataJSON)
		if err != nil {
			return nil, err
		}
		item.MetadataJSON = metadata
		if item.SourceID == "" || item.Kind == "" || item.LocalPath == "" || item.AccessMode == "" {
			return nil, errs.ErrPreconditionFailed
		}
		normalized = append(normalized, item)
	}
	sortRuntimeWorkspaceSources(normalized)
	return normalized, nil
}

func sortRuntimeWorkspaceSources(sources []RuntimeWorkspaceSource) {
	sort.SliceStable(sources, func(left, right int) bool {
		return runtimeWorkspaceSourceSortKey(sources[left]) < runtimeWorkspaceSourceSortKey(sources[right])
	})
}

func runtimeWorkspaceSourceSortKey(source RuntimeWorkspaceSource) string {
	return source.Kind + ":" + source.SourceID
}

func runtimeWorkspacePolicyDigest(policy RuntimeWorkspacePolicy) (string, error) {
	payload := workspacePolicyDigestPayload{
		ProjectID:               policy.ProjectID.String(),
		PolicyVersion:           policy.PolicyVersion,
		Sources:                 policy.Sources,
		ActivePolicyOverrideIDs: policy.ActivePolicyOverrideIDs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func digestText(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func repositoryIDsFromRuntimeSources(sources []RuntimeWorkspaceSource) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(sources))
	result := make([]uuid.UUID, 0, len(sources))
	for _, source := range sources {
		if source.RepositoryID == nil || *source.RepositoryID == uuid.Nil {
			continue
		}
		if _, ok := seen[*source.RepositoryID]; ok {
			continue
		}
		seen[*source.RepositoryID] = struct{}{}
		result = append(result, *source.RepositoryID)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].String() < result[right].String()
	})
	return result
}

func activePolicyOverrideIDs(overrides []PolicyOverrideRef) []string {
	result := make([]string, 0, len(overrides))
	for _, override := range overrides {
		id := strings.TrimSpace(override.ID)
		if id != "" {
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

func guidanceSafeLocalName(ref value.GuidanceRef) string {
	slug := strings.TrimSpace(ref.PackageSlug)
	if guidanceLocalNamePattern.MatchString(slug) {
		return slug
	}
	seed := strings.TrimSpace(ref.PackageRef) + "|" + strings.TrimSpace(ref.PackageVersionRef) + "|" + slug
	sum := sha256.Sum256([]byte(seed))
	return "guidance-" + hex.EncodeToString(sum[:8])
}

func normalizeAccessMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case WorkspaceSourceAccessWrite:
		return WorkspaceSourceAccessWrite
	default:
		return WorkspaceSourceAccessRead
	}
}

func safeJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return normalizeJSONText(string(payload))
}

func mustSafeJSON(value any) string {
	payload, err := safeJSON(value)
	if err != nil {
		return `{}`
	}
	return payload
}

func normalizeJSONText(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "{}", nil
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, []byte(trimmed)); err != nil {
		return "", errs.ErrInvalidArgument
	}
	return buffer.String(), nil
}

func sortedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	sort.Strings(result)
	return result
}

func safeDiagnosticText(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= runtimePrepareSummaryLimit {
		return trimmed
	}
	return trimmed[:runtimePrepareSummaryLimit]
}

func fallbackText(text string, fallback string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func optionalUUIDText(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return id.String()
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

type codeSourceMetadata struct {
	Owner         string `json:"owner"`
	SourceKind    string `json:"source_kind"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type documentationSourceMetadata struct {
	Owner      string `json:"owner"`
	SourceKind string `json:"source_kind"`
	ScopeType  string `json:"scope_type,omitempty"`
	ScopeID    string `json:"scope_id,omitempty"`
}

type guidanceSourceMetadata struct {
	PackageInstallationRef string `json:"package_installation_ref"`
	PackageVersionRef      string `json:"package_version_ref"`
	ManifestDigest         string `json:"manifest_digest"`
	PackageRef             string `json:"package_ref,omitempty"`
	PackageSlug            string `json:"package_slug,omitempty"`
	PackageVersionLabel    string `json:"package_version_label,omitempty"`
	SafeLocalName          string `json:"safe_local_name"`
	CapabilityRef          string `json:"capability_ref,omitempty"`
	CapabilityKind         string `json:"capability_kind,omitempty"`
}

type generatedContextMetadata struct {
	AgentRunID              string                  `json:"agent_run_id"`
	AgentSessionID          string                  `json:"agent_session_id"`
	FlowVersionID           string                  `json:"flow_version_id,omitempty"`
	StageID                 string                  `json:"stage_id,omitempty"`
	RoleProfileID           string                  `json:"role_profile_id"`
	RoleProfileVersion      int64                   `json:"role_profile_version"`
	RoleProfileDigest       string                  `json:"role_profile_digest"`
	RoleKind                string                  `json:"role_kind"`
	RuntimeProfile          string                  `json:"runtime_profile"`
	AllowedMCPTools         []string                `json:"allowed_mcp_tools,omitempty"`
	PromptTemplateVersionID string                  `json:"prompt_template_version_id"`
	PromptTemplateDigest    string                  `json:"prompt_template_digest"`
	ProviderTarget          value.ProviderTargetRef `json:"provider_target,omitempty"`
	GuidanceLocalPaths      []string                `json:"guidance_local_paths,omitempty"`
	GuidanceRefCount        int                     `json:"guidance_ref_count"`
}

type placementMetadataPayload struct {
	AgentRunID     string `json:"agent_run_id"`
	RoleProfileID  string `json:"role_profile_id"`
	RoleKind       string `json:"role_kind"`
	RuntimeProfile string `json:"runtime_profile"`
}

type workspacePolicyDigestPayload struct {
	ProjectID               string                   `json:"project_id"`
	PolicyVersion           int64                    `json:"policy_version"`
	Sources                 []RuntimeWorkspaceSource `json:"sources"`
	ActivePolicyOverrideIDs []string                 `json:"active_policy_override_ids,omitempty"`
}

func placementMetadata(run entity.AgentRun, role entity.RoleProfile) placementMetadataPayload {
	return placementMetadataPayload{
		AgentRunID:     run.ID.String(),
		RoleProfileID:  role.ID.String(),
		RoleKind:       string(role.RoleKind),
		RuntimeProfile: strings.TrimSpace(role.RuntimeProfile),
	}
}
