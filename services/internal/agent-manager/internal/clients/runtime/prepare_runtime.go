// Package runtime adapts runtime-manager workspace preparation to agent-manager.
package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	callerID              = "agent-manager"
	defaultPrepareTimeout = 10 * time.Second
)

// Config contains runtime-manager client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type runtimeManagerClient interface {
	PrepareRuntime(context.Context, *runtimev1.PrepareRuntimeRequest, ...grpc.CallOption) (*runtimev1.PrepareRuntimeResponse, error)
	CreateJob(context.Context, *runtimev1.CreateJobRequest, ...grpc.CallOption) (*runtimev1.JobResponse, error)
	GetJob(context.Context, *runtimev1.GetJobRequest, ...grpc.CallOption) (*runtimev1.JobResponse, error)
}

// Preparer вызывает API runtime-manager для подготовки runtime и постановки job.
type Preparer struct {
	client    runtimeManagerClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.RuntimePreparer = (*Preparer)(nil)
var _ agentservice.RuntimeJobCreator = (*Preparer)(nil)
var _ agentservice.RuntimeJobReader = (*Preparer)(nil)
var _ agentservice.SelfDeployBuildJobCreator = (*Preparer)(nil)

// NewConnection creates a gRPC connection to runtime-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "runtime-manager")
}

// NewPreparer creates a runtime-manager PrepareRuntime client.
func NewPreparer(client runtimev1.RuntimeManagerServiceClient, cfg Config) (*Preparer, error) {
	return newPreparer(client, cfg)
}

func newPreparer(client runtimeManagerClient, cfg Config) (*Preparer, error) {
	settings, err := grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultPrepareTimeout, "runtime-manager")
	if err != nil {
		return nil, err
	}
	return &Preparer{
		client:    client,
		authToken: settings.AuthToken,
		timeout:   settings.Timeout,
	}, nil
}

// PrepareRuntime reserves a runtime slot and starts workspace materialization.
func (p *Preparer) PrepareRuntime(ctx context.Context, input agentservice.RuntimePreparationInput) (agentservice.RuntimePreparationResult, error) {
	return runtimeOperation(ctx, p, input, prepareRuntimeRPC, mapRuntimeError, prepareRuntimeResult)
}

// CreateAgentRunJob ставит agent run job в подготовленный runtime slot.
func (p *Preparer) CreateAgentRunJob(ctx context.Context, input agentservice.RuntimeJobInput) (agentservice.RuntimeJobResult, error) {
	return runtimeOperation(ctx, p, input, createAgentRunJobRPC, mapRuntimeJobError, runtimeJobResult)
}

// GetAgentRunJob читает безопасное состояние agent run job в runtime-manager.
func (p *Preparer) GetAgentRunJob(ctx context.Context, input agentservice.RuntimeJobReadInput) (agentservice.RuntimeJobReadResult, error) {
	return runtimeOperation(ctx, p, input, getAgentRunJobRPC, mapRuntimeJobError, runtimeJobReadResult)
}

// CreateSelfDeployBuildJob ставит build job для approved self-deploy plan.
func (p *Preparer) CreateSelfDeployBuildJob(ctx context.Context, input agentservice.SelfDeployBuildJobInput) (agentservice.RuntimeJobResult, error) {
	return runtimeOperation(ctx, p, input, createSelfDeployBuildJobRPC, mapRuntimeJobError, selfDeployBuildJobResult)
}

func runtimeOperation[I any, R any, O any](
	ctx context.Context,
	p *Preparer,
	input I,
	call func(context.Context, runtimeManagerClient, I) (R, error),
	mapErr func(error) error,
	result func(I, R) (O, error),
) (O, error) {
	var zero O
	if p == nil || p.client == nil {
		return zero, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(p.outgoingContext(ctx), p.timeout)
	defer cancel()
	response, err := call(callCtx, p.client, input)
	if err != nil {
		return zero, mapErr(err)
	}
	return result(input, response)
}

func prepareRuntimeRPC(ctx context.Context, client runtimeManagerClient, input agentservice.RuntimePreparationInput) (*runtimev1.PrepareRuntimeResponse, error) {
	return client.PrepareRuntime(ctx, prepareRuntimeRequest(input))
}

func createAgentRunJobRPC(ctx context.Context, client runtimeManagerClient, input agentservice.RuntimeJobInput) (*runtimev1.JobResponse, error) {
	return client.CreateJob(ctx, createAgentRunJobRequest(input))
}

func createSelfDeployBuildJobRPC(ctx context.Context, client runtimeManagerClient, input agentservice.SelfDeployBuildJobInput) (*runtimev1.JobResponse, error) {
	return client.CreateJob(ctx, createSelfDeployBuildJobRequest(input))
}

func getAgentRunJobRPC(ctx context.Context, client runtimeManagerClient, input agentservice.RuntimeJobReadInput) (*runtimev1.JobResponse, error) {
	return client.GetJob(ctx, getAgentRunJobRequest(input))
}

func (p *Preparer) outgoingContext(ctx context.Context) context.Context {
	return grpcclient.OutgoingContext(ctx, p.authToken, callerID)
}

func prepareRuntimeRequest(input agentservice.RuntimePreparationInput) *runtimev1.PrepareRuntimeRequest {
	agentRunID := input.AgentRunID.String()
	return &runtimev1.PrepareRuntimeRequest{
		AgentRunId:           &agentRunID,
		RuntimeProfile:       strings.TrimSpace(input.RuntimeProfile),
		RuntimeMode:          runtimeMode(input.RuntimeMode),
		WorkspacePolicy:      workspacePolicy(input.WorkspacePolicy),
		PlacementConstraints: placementConstraints(input.PlacementConstraints),
		Meta:                 commandMeta(input.Meta),
	}
}

func createAgentRunJobRequest(input agentservice.RuntimeJobInput) *runtimev1.CreateJobRequest {
	agentRunID := input.AgentRunID.String()
	slotRef := strings.TrimSpace(input.SlotRef)
	return &runtimev1.CreateJobRequest{
		JobType:               runtimev1.JobType_JOB_TYPE_AGENT_RUN,
		Priority:              runtimev1.JobPriority_JOB_PRIORITY_NORMAL,
		SlotId:                optionalString(slotRef),
		AgentRunId:            &agentRunID,
		JobInputJson:          "{}",
		AgentRunExecutionSpec: agentRunExecutionSpec(input.ExecutionSpec),
		Meta:                  commandMeta(input.Meta),
	}
}

func createSelfDeployBuildJobRequest(input agentservice.SelfDeployBuildJobInput) *runtimev1.CreateJobRequest {
	projectID := input.ProjectID.String()
	repositoryID := input.RepositoryID.String()
	return &runtimev1.CreateJobRequest{
		JobType:            runtimev1.JobType_JOB_TYPE_BUILD,
		Priority:           runtimev1.JobPriority_JOB_PRIORITY_NORMAL,
		ProjectId:          &projectID,
		RepositoryId:       &repositoryID,
		JobInputJson:       "{}",
		BuildExecutionSpec: buildExecutionSpec(input.BuildExecutionSpec),
		Meta:               commandMeta(input.Meta),
	}
}

func buildExecutionSpec(spec agentservice.SelfDeployBuildExecutionSpec) *runtimev1.BuildExecutionSpec {
	converted := &runtimev1.BuildExecutionSpec{
		ServiceKey:           strings.TrimSpace(spec.ServiceKey),
		SourceRef:            strings.TrimSpace(spec.SourceRef),
		SourceCommitSha:      strings.TrimSpace(spec.SourceCommitSHA),
		BuildPlanFingerprint: strings.TrimSpace(spec.BuildPlanFingerprint),
	}
	converted.ImageRef = strings.TrimSpace(spec.ImageRef)
	converted.ImageTag = strings.TrimSpace(spec.ImageTag)
	converted.ImageDigest = optionalString(strings.TrimSpace(spec.ImageDigest))
	converted.BuildContextRef = strings.TrimSpace(spec.BuildContextRef)
	converted.BuildContextDigest = strings.TrimSpace(spec.BuildContextDigest)
	converted.DockerfileRef = strings.TrimSpace(spec.DockerfileRef)
	converted.DockerfileDigest = optionalString(strings.TrimSpace(spec.DockerfileDigest))
	converted.DockerfileTarget = strings.TrimSpace(spec.DockerfileTarget)
	converted.BuilderImageRef = strings.TrimSpace(spec.BuilderImageRef)
	converted.AllowedSecretRefs = runtimeBuildAllowedSecretRefs(spec.AllowedSecretRefs)
	converted.OutputRefs = runtimeBuildOutputRefs(spec.OutputRefs)
	return converted
}

func runtimeBuildAllowedSecretRefs(refs []agentservice.RuntimeJobAllowedSecretRef) []*runtimev1.RuntimeJobAllowedSecretRef {
	result := make([]*runtimev1.RuntimeJobAllowedSecretRef, 0, len(refs))
	for index := range refs {
		ref := refs[index]
		result = append(result, &runtimev1.RuntimeJobAllowedSecretRef{
			SecretRef: strings.TrimSpace(ref.SecretRef),
			Purpose:   strings.TrimSpace(ref.Purpose),
		})
	}
	return result
}

func runtimeBuildOutputRefs(refs []agentservice.RuntimeJobOutputRef) []*runtimev1.RuntimeJobOutputRef {
	if len(refs) == 0 {
		return nil
	}
	result := make([]*runtimev1.RuntimeJobOutputRef, 0, len(refs))
	for _, ref := range refs {
		item := &runtimev1.RuntimeJobOutputRef{}
		item.Kind = strings.TrimSpace(ref.Kind)
		item.Ref = strings.TrimSpace(ref.Ref)
		result = append(result, item)
	}
	return result
}

func agentRunExecutionSpec(spec agentservice.AgentRunExecutionSpec) *runtimev1.AgentRunExecutionSpec {
	if spec.AgentRunID == uuid.Nil || spec.SlotID == uuid.Nil || spec.ExpectedMaterializationID == uuid.Nil {
		return nil
	}
	return &runtimev1.AgentRunExecutionSpec{
		AgentRunId:                         spec.AgentRunID.String(),
		SlotId:                             spec.SlotID.String(),
		ExpectedMaterializationId:          spec.ExpectedMaterializationID.String(),
		ExpectedMaterializationFingerprint: strings.TrimSpace(spec.ExpectedMaterializationFingerprint),
		WorkspaceRef:                       strings.TrimSpace(spec.WorkspaceRef),
		WorkspaceMountRef:                  strings.TrimSpace(spec.WorkspaceMountRef),
		WorkspacePvcRef:                    strings.TrimSpace(spec.WorkspacePVCRef),
		ContextRef:                         strings.TrimSpace(spec.ContextRef),
		ContextDigest:                      strings.TrimSpace(spec.ContextDigest),
		RunnerProfileRef:                   strings.TrimSpace(spec.RunnerProfileRef),
		RunnerImageRef:                     strings.TrimSpace(spec.RunnerImageRef),
		RunnerMode:                         agentRunRunnerMode(spec.RunnerMode),
		AllowedSecretRefs:                  agentRunAllowedSecretRefs(spec.AllowedSecretRefs),
		ReportingTargetRefs:                agentRunReportingTargetRefs(spec.ReportingTargetRefs),
		CodexSessionExecutionSpec:          codexSessionExecutionSpec(spec.CodexSessionExecutionSpec),
	}
}

func agentRunRunnerMode(mode string) runtimev1.AgentRunRunnerMode {
	switch strings.TrimSpace(mode) {
	case agentservice.RuntimeJobRunnerModeCodexAgent:
		return runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_CODEX_AGENT
	default:
		return runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_UNSPECIFIED
	}
}

func agentRunAllowedSecretRefs(refs []agentservice.AgentRunExecutionRef) []*runtimev1.AgentRunAllowedSecretRef {
	return mapAgentRunExecutionRefs(refs, agentRunAllowedSecretRef)
}

func agentRunReportingTargetRefs(refs []agentservice.AgentRunExecutionRef) []*runtimev1.AgentRunReportingTargetRef {
	return mapAgentRunExecutionRefs(refs, agentRunReportingTargetRef)
}

func agentRunAllowedSecretRef(ref agentservice.AgentRunExecutionRef) *runtimev1.AgentRunAllowedSecretRef {
	return &runtimev1.AgentRunAllowedSecretRef{Purpose: ref.Kind, SecretRef: ref.Ref}
}

func agentRunReportingTargetRef(ref agentservice.AgentRunExecutionRef) *runtimev1.AgentRunReportingTargetRef {
	return &runtimev1.AgentRunReportingTargetRef{Kind: ref.Kind, Ref: ref.Ref}
}

func codexSessionExecutionSpec(spec *agentservice.CodexSessionExecutionSpec) *runtimev1.CodexSessionExecutionSpec {
	if spec == nil {
		return nil
	}
	result := &runtimev1.CodexSessionExecutionSpec{
		InstructionObjectRef:    strings.TrimSpace(spec.InstructionObjectRef),
		InstructionObjectDigest: strings.TrimSpace(spec.InstructionObjectDigest),
		ResultSchemaRef:         strings.TrimSpace(spec.ResultSchemaRef),
		ResultSchemaDigest:      strings.TrimSpace(spec.ResultSchemaDigest),
		HookEndpointRef:         strings.TrimSpace(spec.HookEndpointRef),
		CallbackRefs:            agentRunExecutionRefs(spec.CallbackRefs),
		TimeoutSeconds:          spec.TimeoutSeconds,
		RunnerProfileRef:        strings.TrimSpace(spec.RunnerProfileRef),
		RunnerMode:              agentRunRunnerMode(spec.RunnerMode),
		OutputRefs:              agentRunExecutionRefs(spec.OutputRefs),
		ResultRefs:              agentRunExecutionRefs(spec.ResultRefs),
		AllowedSecretRefs:       agentRunAllowedSecretRefs(spec.AllowedSecretRefs),
	}
	if snapshotRef := strings.TrimSpace(spec.SessionSnapshotRef); snapshotRef != "" {
		result.SessionSnapshotRef = &snapshotRef
	}
	if snapshotRef := strings.TrimSpace(spec.WorkspaceSnapshotRef); snapshotRef != "" {
		result.WorkspaceSnapshotRef = &snapshotRef
	}
	return result
}

func agentRunExecutionRefs(refs []agentservice.AgentRunExecutionRef) []*runtimev1.AgentRunExecutionRef {
	return mapAgentRunExecutionRefs(refs, agentRunExecutionRef)
}

func agentRunExecutionRef(ref agentservice.AgentRunExecutionRef) *runtimev1.AgentRunExecutionRef {
	return &runtimev1.AgentRunExecutionRef{Kind: ref.Kind, Ref: ref.Ref}
}

func mapAgentRunExecutionRefs[T any](refs []agentservice.AgentRunExecutionRef, mapRef func(agentservice.AgentRunExecutionRef) T) []T {
	result := make([]T, 0, len(refs))
	for _, ref := range refs {
		result = append(result, mapRef(agentservice.AgentRunExecutionRef{
			Kind: strings.TrimSpace(ref.Kind),
			Ref:  strings.TrimSpace(ref.Ref),
		}))
	}
	return result
}

func getAgentRunJobRequest(input agentservice.RuntimeJobReadInput) *runtimev1.GetJobRequest {
	return &runtimev1.GetJobRequest{
		JobId: strings.TrimSpace(input.JobRef),
		Meta:  queryMeta(input.Meta),
	}
}

func workspacePolicy(policy agentservice.RuntimeWorkspacePolicy) *runtimev1.WorkspacePolicyInput {
	return &runtimev1.WorkspacePolicyInput{
		ProjectId:               policy.ProjectID.String(),
		PolicyDigest:            strings.TrimSpace(policy.PolicyDigest),
		PolicyVersion:           policy.PolicyVersion,
		Sources:                 workspaceSources(policy.Sources),
		ActivePolicyOverrideIds: trimmedStrings(policy.ActivePolicyOverrideIDs),
	}
}

func workspaceSources(sources []agentservice.RuntimeWorkspaceSource) []*runtimev1.WorkspaceSource {
	result := make([]*runtimev1.WorkspaceSource, 0, len(sources))
	for _, source := range sources {
		result = append(result, &runtimev1.WorkspaceSource{
			SourceId:      strings.TrimSpace(source.SourceID),
			Kind:          workspaceSourceKind(source.Kind),
			RepositoryId:  optionalUUIDString(source.RepositoryID),
			Provider:      optionalString(source.Provider),
			ProviderOwner: optionalString(source.ProviderOwner),
			ProviderName:  optionalString(source.ProviderName),
			SourceRef:     optionalString(source.SourceRef),
			CommitSha:     optionalString(source.CommitSHA),
			LocalPath:     strings.TrimSpace(source.LocalPath),
			AccessMode:    workspaceSourceAccessMode(source.AccessMode),
			Digest:        optionalString(source.Digest),
			MetadataJson:  defaultJSON(source.MetadataJSON),
		})
	}
	return result
}

func placementConstraints(constraints agentservice.RuntimePlacementConstraints) *runtimev1.PlacementConstraints {
	return &runtimev1.PlacementConstraints{
		ProjectId:             constraints.ProjectID.String(),
		RepositoryIds:         uuidStrings(constraints.RepositoryIDs),
		ServiceKeys:           trimmedStrings(constraints.ServiceKeys),
		RuntimeProfile:        strings.TrimSpace(constraints.RuntimeProfile),
		PreferredFleetScopeId: optionalUUIDString(constraints.PreferredFleetScopeID),
		RequiredCapabilities:  trimmedStrings(constraints.RequiredCapabilities),
		MetadataJson:          defaultJSON(constraints.MetadataJSON),
	}
}

func commandMeta(meta value.CommandMeta) *runtimev1.CommandMeta {
	commandID := meta.CommandID.String()
	return &runtimev1.CommandMeta{
		CommandId: &commandID,
		Actor: &runtimev1.Actor{
			Type: strings.TrimSpace(meta.Actor.Type),
			Id:   strings.TrimSpace(meta.Actor.ID),
		},
		RequestContext: &runtimev1.RequestContext{Source: callerID},
	}
}

func queryMeta(meta value.QueryMeta) *runtimev1.QueryMeta {
	return &runtimev1.QueryMeta{
		Actor:          runtimeQueryActor(meta.Actor),
		RequestContext: runtimeQueryContext(),
	}
}

func runtimeQueryActor(actor value.Actor) *runtimev1.Actor {
	return &runtimev1.Actor{
		Type: strings.TrimSpace(actor.Type),
		Id:   strings.TrimSpace(actor.ID),
	}
}

func runtimeQueryContext() *runtimev1.RequestContext {
	return &runtimev1.RequestContext{Source: callerID}
}

func prepareRuntimeResult(input agentservice.RuntimePreparationInput, response *runtimev1.PrepareRuntimeResponse) (agentservice.RuntimePreparationResult, error) {
	if response == nil {
		return agentservice.RuntimePreparationResult{}, agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager returned an empty response")
	}
	slot := response.GetSlot()
	materialization := response.GetWorkspaceMaterialization()
	runtimeContext := response.GetRuntimeContext()
	slotRef := strings.TrimSpace(runtimeContext.GetSlotId())
	if slotRef == "" {
		slotRef = strings.TrimSpace(slot.GetSlotId())
	}
	workspaceRef := strings.TrimSpace(materialization.GetWorkspaceMaterializationId())
	if slotRef == "" || workspaceRef == "" {
		return agentservice.RuntimePreparationResult{}, agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager returned incomplete runtime refs")
	}
	fingerprint := firstNonEmpty(
		runtimeContext.GetMaterializationFingerprint(),
		materialization.GetFingerprint(),
		materialization.GetPolicyDigest(),
		slot.GetFingerprint(),
		input.WorkspacePolicy.PolicyDigest,
	)
	contextRef, contextDigest := agentRunContextRefs(materialization)
	return agentservice.RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     runtimeSlotStatus(slot.GetStatus()),
		WorkspaceRef:                   workspaceRef,
		WorkspaceMaterializationStatus: runtimeWorkspaceMaterializationStatus(materialization.GetStatus()),
		ContextRef:                     contextRef,
		ContextDigest:                  contextDigest,
		MaterializationFingerprint:     fingerprint,
		DiagnosticSummary:              runtimeDiagnosticSummary(slot, materialization),
	}, nil
}

func agentRunContextRefs(materialization *runtimev1.WorkspaceMaterialization) (string, string) {
	if materialization == nil {
		return "", ""
	}
	materializationID := strings.TrimSpace(materialization.GetWorkspaceMaterializationId())
	if materializationID == "" {
		return "", ""
	}
	contextRef := "runtime://workspace-materializations/" + materializationID + "/context/agent-run.json"
	for _, source := range materialization.GetSources() {
		if source.GetKind() == runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT {
			return contextRef, strings.TrimSpace(source.GetDigest())
		}
	}
	return contextRef, ""
}

func runtimeJobResult(input agentservice.RuntimeJobInput, response *runtimev1.JobResponse) (agentservice.RuntimeJobResult, error) {
	if response == nil || response.GetJob() == nil {
		return agentservice.RuntimeJobResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned an empty job response")
	}
	job := response.GetJob()
	jobRef := strings.TrimSpace(job.GetJobId())
	if jobRef == "" || job.GetJobType() != runtimev1.JobType_JOB_TYPE_AGENT_RUN || strings.TrimSpace(job.GetAgentRunId()) != input.AgentRunID.String() {
		return agentservice.RuntimeJobResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned incomplete agent run job refs")
	}
	return agentservice.RuntimeJobResult{
		JobRef:            jobRef,
		Status:            runtimeJobStatus(job.GetStatus()),
		DiagnosticSummary: runtimeJobDiagnosticSummary(job),
	}, nil
}

func selfDeployBuildJobResult(input agentservice.SelfDeployBuildJobInput, response *runtimev1.JobResponse) (agentservice.RuntimeJobResult, error) {
	if response == nil || response.GetJob() == nil {
		return agentservice.RuntimeJobResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned an empty build job response")
	}
	job := response.GetJob()
	jobRef := strings.TrimSpace(job.GetJobId())
	if jobRef == "" ||
		job.GetJobType() != runtimev1.JobType_JOB_TYPE_BUILD ||
		strings.TrimSpace(job.GetProjectId()) != input.ProjectID.String() ||
		strings.TrimSpace(job.GetRepositoryId()) != input.RepositoryID.String() {
		return agentservice.RuntimeJobResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned incomplete build job refs")
	}
	return agentservice.RuntimeJobResult{
		JobRef:            jobRef,
		Status:            runtimeJobStatus(job.GetStatus()),
		DiagnosticSummary: runtimeJobDiagnosticSummary(job),
	}, nil
}

func runtimeJobReadResult(input agentservice.RuntimeJobReadInput, response *runtimev1.JobResponse) (agentservice.RuntimeJobReadResult, error) {
	if response == nil || response.GetJob() == nil {
		return agentservice.RuntimeJobReadResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned an empty job response")
	}
	job := response.GetJob()
	jobRef := strings.TrimSpace(job.GetJobId())
	agentRunID, err := uuid.Parse(strings.TrimSpace(job.GetAgentRunId()))
	if err != nil || agentRunID == uuid.Nil {
		return agentservice.RuntimeJobReadResult{}, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned incomplete agent run job refs")
	}
	if jobRef == "" || jobRef != strings.TrimSpace(input.JobRef) || job.GetJobType() != runtimev1.JobType_JOB_TYPE_AGENT_RUN || agentRunID != input.AgentRunID {
		return agentservice.RuntimeJobReadResult{}, agentservice.NewRuntimeJobError(true, "conflict", "runtime-manager returned mismatched agent run job refs")
	}
	createdAt, err := optionalRuntimeTime(job.GetCreatedAt())
	if err != nil {
		return agentservice.RuntimeJobReadResult{}, err
	}
	startedAt, err := optionalRuntimeTime(job.GetStartedAt())
	if err != nil {
		return agentservice.RuntimeJobReadResult{}, err
	}
	finishedAt, err := optionalRuntimeTime(job.GetFinishedAt())
	if err != nil {
		return agentservice.RuntimeJobReadResult{}, err
	}
	return agentservice.RuntimeJobReadResult{
		JobRef:           jobRef,
		AgentRunID:       agentRunID,
		CommandRef:       strings.TrimSpace(job.GetCommandId()),
		Status:           runtimeJobStatusDomain(job.GetStatus()),
		Version:          job.GetVersion(),
		CreatedAt:        createdAt,
		StartedAt:        startedAt,
		FinishedAt:       finishedAt,
		SafeErrorCode:    strings.TrimSpace(job.GetLastErrorCode()),
		SafeErrorSummary: safeRuntimeText(job.GetLastErrorMessage()),
		SafeSummary:      safeRuntimeText(runtimeJobDiagnosticSummary(job)),
	}, nil
}

func runtimeDiagnosticSummary(slot *runtimev1.Slot, materialization *runtimev1.WorkspaceMaterialization) string {
	parts := []string{}
	if slot != nil && slot.GetStatus() != runtimev1.SlotStatus_SLOT_STATUS_UNSPECIFIED {
		parts = append(parts, "slot_status="+slot.GetStatus().String())
	}
	if materialization != nil && materialization.GetStatus() != runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_UNSPECIFIED {
		parts = append(parts, "workspace_status="+materialization.GetStatus().String())
	}
	return strings.Join(parts, ";")
}

func runtimeJobDiagnosticSummary(job *runtimev1.Job) string {
	if job == nil {
		return ""
	}
	parts := []string{}
	if job.GetStatus() != runtimev1.JobStatus_JOB_STATUS_UNSPECIFIED {
		parts = append(parts, "job_status="+runtimeJobStatus(job.GetStatus()))
	}
	if next := strings.TrimSpace(job.GetNextAction()); next != "" {
		parts = append(parts, "next_action="+next)
	}
	return strings.Join(parts, ";")
}

var runtimeJobStatusNames = map[runtimev1.JobStatus]string{
	runtimev1.JobStatus_JOB_STATUS_PENDING:   "pending",
	runtimev1.JobStatus_JOB_STATUS_CLAIMED:   "claimed",
	runtimev1.JobStatus_JOB_STATUS_RUNNING:   "running",
	runtimev1.JobStatus_JOB_STATUS_SUCCEEDED: "succeeded",
	runtimev1.JobStatus_JOB_STATUS_FAILED:    "failed",
	runtimev1.JobStatus_JOB_STATUS_CANCELLED: "cancelled",
	runtimev1.JobStatus_JOB_STATUS_TIMED_OUT: "timed_out",
}

var runtimeSlotStatusNames = map[runtimev1.SlotStatus]string{
	runtimev1.SlotStatus_SLOT_STATUS_PREWARMED:       "prewarmed",
	runtimev1.SlotStatus_SLOT_STATUS_RESERVED:        "reserved",
	runtimev1.SlotStatus_SLOT_STATUS_MATERIALIZING:   "materializing",
	runtimev1.SlotStatus_SLOT_STATUS_READY:           agentservice.RuntimeSlotStatusReady,
	runtimev1.SlotStatus_SLOT_STATUS_IN_USE:          "in_use",
	runtimev1.SlotStatus_SLOT_STATUS_RELEASING:       "releasing",
	runtimev1.SlotStatus_SLOT_STATUS_FAILED:          agentservice.RuntimeSlotStatusFailed,
	runtimev1.SlotStatus_SLOT_STATUS_CLEANUP_PENDING: "cleanup_pending",
	runtimev1.SlotStatus_SLOT_STATUS_CLEANED:         "cleaned",
}

var runtimeWorkspaceMaterializationStatusNames = map[runtimev1.WorkspaceMaterializationStatus]string{
	runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_PENDING:   "pending",
	runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_RUNNING:   "running",
	runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_COMPLETED: agentservice.RuntimeWorkspaceMaterializationStatusCompleted,
	runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_FAILED:    agentservice.RuntimeWorkspaceMaterializationStatusFailed,
	runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_CANCELLED: agentservice.RuntimeWorkspaceMaterializationStatusCancelled,
}

func runtimeJobStatus(status runtimev1.JobStatus) string {
	return runtimeJobStatusNames[status]
}

func runtimeSlotStatus(status runtimev1.SlotStatus) string {
	return runtimeSlotStatusNames[status]
}

func runtimeWorkspaceMaterializationStatus(status runtimev1.WorkspaceMaterializationStatus) string {
	return runtimeWorkspaceMaterializationStatusNames[status]
}

func runtimeJobStatusDomain(status runtimev1.JobStatus) agentservice.RuntimeJobStatus {
	return agentservice.RuntimeJobStatus(runtimeJobStatus(status))
}

func optionalRuntimeTime(text string) (*time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil, agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned invalid agent run job timestamps")
	}
	utc := parsed.UTC()
	return &utc, nil
}

func safeRuntimeText(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= 512 {
		return trimmed
	}
	return trimmed[:512]
}

func runtimeMode(mode string) runtimev1.RuntimeMode {
	switch strings.TrimSpace(mode) {
	case "code_only":
		return runtimev1.RuntimeMode_RUNTIME_MODE_CODE_ONLY
	case agentservice.RuntimeModeFullEnv:
		return runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	case "read_only_production":
		return runtimev1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION
	default:
		return runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	}
}

func workspaceSourceKind(kind string) runtimev1.WorkspaceSourceKind {
	switch strings.TrimSpace(kind) {
	case agentservice.WorkspaceSourceKindCode:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_CODE
	case agentservice.WorkspaceSourceKindDocumentation:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_DOCUMENTATION
	case agentservice.WorkspaceSourceKindGuidancePackage:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GUIDANCE_PACKAGE
	case agentservice.WorkspaceSourceKindGeneratedContext:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT
	default:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_UNSPECIFIED
	}
}

func workspaceSourceAccessMode(mode string) runtimev1.WorkspaceSourceAccessMode {
	switch strings.TrimSpace(mode) {
	case agentservice.WorkspaceSourceAccessWrite:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_WRITE
	default:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_READ
	}
}

func mapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agentservice.NewRuntimePreparationError(true, "deadline_exceeded", "runtime-manager did not finish workspace preparation request in time")
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return agentservice.NewRuntimePreparationError(false, "invalid_argument", "runtime-manager rejected the workspace preparation request")
	case codes.NotFound:
		return agentservice.NewRuntimePreparationError(false, "not_found", "runtime-manager could not find required preparation state")
	case codes.PermissionDenied, codes.Unauthenticated:
		return agentservice.NewRuntimePreparationError(false, "permission_denied", "runtime-manager rejected the preparation caller")
	case codes.FailedPrecondition:
		return agentservice.NewRuntimePreparationError(false, "failed_precondition", "runtime-manager rejected workspace preparation preconditions")
	case codes.Aborted:
		return agentservice.NewRuntimePreparationError(true, "conflict", "runtime-manager reported a retryable preparation conflict")
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager is temporarily unavailable")
	default:
		return agentservice.NewRuntimePreparationError(true, "runtime_prepare_failed", "runtime-manager workspace preparation failed")
	}
}

func mapRuntimeJobError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agentservice.NewRuntimeJobError(true, "deadline_exceeded", "runtime-manager did not finish agent run job request in time")
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return agentservice.NewRuntimeJobError(false, "invalid_argument", "runtime-manager rejected the agent run job request")
	case codes.NotFound:
		return agentservice.NewRuntimeJobError(false, "not_found", "runtime-manager could not find required agent run job state")
	case codes.PermissionDenied, codes.Unauthenticated:
		return agentservice.NewRuntimeJobError(false, "permission_denied", "runtime-manager rejected the agent run job caller")
	case codes.FailedPrecondition:
		return agentservice.NewRuntimeJobError(false, "failed_precondition", "runtime-manager rejected agent run job preconditions")
	case codes.Aborted, codes.AlreadyExists:
		return agentservice.NewRuntimeJobError(true, "conflict", "runtime-manager reported a retryable agent run job conflict")
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return agentservice.NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager is temporarily unavailable")
	default:
		return agentservice.NewRuntimeJobError(true, "runtime_job_failed", "runtime-manager agent run job creation failed")
	}
}

func optionalString(text string) *string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := strings.TrimSpace(id.String())
	if value == "" {
		return nil
	}
	return &value
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			result = append(result, id.String())
		}
	}
	return result
}

func trimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func defaultJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
