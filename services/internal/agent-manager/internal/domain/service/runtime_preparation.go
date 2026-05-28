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

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	runtimePrepareReasonRetryable = "runtime_prepare_retryable"
	runtimePrepareFailureCode     = "runtime_prepare_failed"
	runtimeJobReasonRetryable     = "runtime_job_retryable"
	runtimeJobFailureCode         = "runtime_job_failed"
	runtimePrepareDefaultMode     = RuntimeModeFullEnv
	runtimePrepareSummaryLimit    = 512
)

var guidanceLocalNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

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
	selection, err := workspacePolicySelection(session.Scope)
	if err != nil {
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	policy, err := s.workspacePolicyResolver.ResolveWorkspacePolicy(ctx, WorkspacePolicyResolutionInput{
		Meta:                    meta,
		ProjectID:               selection.ProjectID,
		RepositoryIDs:           selection.RepositoryIDs,
		ServiceKeys:             selection.ServiceKeys,
		IncludeGuidancePackages: true,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	if err := validateGuidanceAllowedByPolicy(policy, run.GuidanceRefs); err != nil {
		return s.recordRuntimePreparationFailure(ctx, meta, run, err)
	}
	request, err := runtimePreparationRequest(meta, session, role, promptVersion, run, policy)
	if err != nil {
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
		return s.dispatchRuntimeJobForRun(ctx, meta, run, result)
	}
	return s.recordRuntimePreparationSuccess(ctx, meta, run, result)
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
	job, err := s.runtimeJobCreator.CreateAgentRunJob(ctx, RuntimeJobInput{
		Meta:       runtimeJobCommandMeta(meta, run.ID),
		AgentRunID: run.ID,
		SlotRef:    runtimeContext.SlotRef,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return run, err
		}
		return s.recordRuntimeJobFailure(ctx, meta, run, runtimeContext, err)
	}
	return s.recordRuntimeJobSuccess(ctx, meta, run, runtimeContext, result, job)
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
		return s.recordRuntimeJobFailure(ctx, meta, run, runtimeContext, errs.ErrDependencyUnavailable)
	}
	summary := runtimeJobSuccessSummary(preparation, job)
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusStarting, runtimeContext, summary, "", "")
}

func (s *Service) recordRuntimeJobFailure(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	runtimeContext value.RuntimeContextRef,
	err error,
) (entity.AgentRun, error) {
	failure := classifyRuntimeJobFailure(err)
	if failure.retryable {
		return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusWaiting, runtimeContext, failure.summary(), "", runtimeJobReasonRetryable)
	}
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusFailed, runtimeContext, failure.summary(), runtimeJobFailureCode, "")
}

func (s *Service) recordRuntimePreparationFailure(
	ctx context.Context,
	meta value.CommandMeta,
	run entity.AgentRun,
	err error,
) (entity.AgentRun, error) {
	failure := classifyRuntimePreparationFailure(err)
	if failure.retryable {
		return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusWaiting, value.RuntimeContextRef{}, failure.summary(), "", runtimePrepareReasonRetryable)
	}
	return s.recordRuntimePreparationState(ctx, meta, run, enum.AgentRunStatusFailed, value.RuntimeContextRef{}, failure.summary(), runtimePrepareFailureCode, "")
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
	sort.SliceStable(normalized, func(left, right int) bool {
		leftKey := normalized[left].Kind + ":" + normalized[left].SourceID
		rightKey := normalized[right].Kind + ":" + normalized[right].SourceID
		return leftKey < rightKey
	})
	return normalized, nil
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
