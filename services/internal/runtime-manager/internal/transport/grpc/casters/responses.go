package casters

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// PrepareRuntimeResponse maps a domain prepare result to a gRPC response.
func PrepareRuntimeResponse(result runtimeservice.PrepareRuntimeResult) *runtimev1.PrepareRuntimeResponse {
	return &runtimev1.PrepareRuntimeResponse{
		Slot:                     SlotToProto(result.Slot),
		WorkspaceMaterialization: WorkspaceMaterializationToProto(result.WorkspaceMaterialization),
		RuntimeContext:           RuntimeContextToProto(result.RuntimeContext),
	}
}

// SlotResponse maps a domain slot to a gRPC response.
func SlotResponse(slot entity.Slot) *runtimev1.SlotResponse {
	return &runtimev1.SlotResponse{Slot: SlotToProto(slot)}
}

// WorkspaceMaterializationResponse maps a domain materialization to a gRPC response.
func WorkspaceMaterializationResponse(materialization entity.WorkspaceMaterialization) *runtimev1.WorkspaceMaterializationResponse {
	return &runtimev1.WorkspaceMaterializationResponse{WorkspaceMaterialization: WorkspaceMaterializationToProto(materialization)}
}

// JobResponse maps a domain job to a gRPC response.
func JobResponse(job entity.Job) *runtimev1.JobResponse {
	return &runtimev1.JobResponse{Job: JobToProto(job)}
}

// ClaimRunnableJobResponse maps a claimed job and one-time lease token.
func ClaimRunnableJobResponse(result runtimeservice.ClaimRunnableJobResult) *runtimev1.ClaimRunnableJobResponse {
	return &runtimev1.ClaimRunnableJobResponse{Job: JobToProto(result.Job), LeaseToken: result.LeaseToken}
}

// RuntimeArtifactRefResponse maps one external artifact reference.
func RuntimeArtifactRefResponse(ref entity.RuntimeArtifactRef) *runtimev1.RuntimeArtifactRefResponse {
	return &runtimev1.RuntimeArtifactRefResponse{RuntimeArtifactRef: RuntimeArtifactRefToProto(ref)}
}

// ListSlotsResponse maps a domain slot page to a gRPC response.
func ListSlotsResponse(result runtimeservice.ListSlotsResult) *runtimev1.ListSlotsResponse {
	return &runtimev1.ListSlotsResponse{Slots: slotsToProto(result.Slots), Page: pageResponseToProto(result.Page)}
}

// ListWorkspaceMaterializationsResponse maps a domain materialization page to a gRPC response.
func ListWorkspaceMaterializationsResponse(result runtimeservice.ListWorkspaceMaterializationsResult) *runtimev1.ListWorkspaceMaterializationsResponse {
	return &runtimev1.ListWorkspaceMaterializationsResponse{
		WorkspaceMaterializations: workspaceMaterializationsToProto(result.WorkspaceMaterializations),
		Page:                      pageResponseToProto(result.Page),
	}
}

// ListJobsResponse maps a domain job page to a gRPC response.
func ListJobsResponse(result runtimeservice.ListJobsResult) *runtimev1.ListJobsResponse {
	return &runtimev1.ListJobsResponse{Jobs: jobsToProto(result.Jobs), Page: pageResponseToProto(result.Page)}
}

// ListRuntimeArtifactRefsResponse maps an artifact reference page to a gRPC response.
func ListRuntimeArtifactRefsResponse(result runtimeservice.ListRuntimeArtifactRefsResult) *runtimev1.ListRuntimeArtifactRefsResponse {
	return &runtimev1.ListRuntimeArtifactRefsResponse{
		RuntimeArtifactRefs: runtimeArtifactRefsToProto(result.RuntimeArtifactRefs),
		Page:                pageResponseToProto(result.Page),
	}
}

// CleanupPolicyResponse maps one cleanup policy.
func CleanupPolicyResponse(policy entity.CleanupPolicy) *runtimev1.CleanupPolicyResponse {
	response := new(runtimev1.CleanupPolicyResponse)
	response.CleanupPolicy = CleanupPolicyToProto(policy)
	return response
}

// RunCleanupBatchResponse maps cleanup counters.
func RunCleanupBatchResponse(result runtimeservice.RunCleanupBatchResult) *runtimev1.RunCleanupBatchResponse {
	response := new(runtimev1.RunCleanupBatchResponse)
	response.ClaimedCount = int32(result.ClaimedCount)
	response.CleanedCount = int32(result.CleanedCount)
	response.FailedCount = int32(result.FailedCount)
	response.AffectedSlotIds = uuidStrings(result.AffectedSlotIDs)
	return response
}

// PrewarmPoolResponse maps one prewarm pool.
func PrewarmPoolResponse(pool entity.PrewarmPool) *runtimev1.PrewarmPoolResponse {
	response := new(runtimev1.PrewarmPoolResponse)
	response.PrewarmPool = PrewarmPoolToProto(pool)
	return response
}

// SlotToProto maps one domain slot.
func SlotToProto(slot entity.Slot) *runtimev1.Slot {
	return &runtimev1.Slot{
		SlotId:           slot.ID.String(),
		SlotKey:          slot.SlotKey,
		Status:           SlotStatusToProto(slot.Status),
		RuntimeMode:      RuntimeModeToProto(slot.RuntimeMode),
		IsPrewarmed:      slot.IsPrewarmed,
		FleetScopeId:     uuidStringPtr(slot.FleetScopeID),
		ClusterId:        uuidStringPtr(slot.ClusterID),
		NamespaceName:    slot.NamespaceName,
		AgentRunId:       uuidStringPtr(slot.AgentRunID),
		ProjectId:        uuidStringPtr(slot.ProjectID),
		RepositoryIds:    uuidStrings(slot.RepositoryIDs),
		RuntimeProfile:   slot.RuntimeProfile,
		Fingerprint:      slot.Fingerprint,
		LeaseOwner:       slot.LeaseOwner,
		LeaseUntil:       timeStringPtr(slot.LeaseUntil),
		LastErrorCode:    slot.LastErrorCode,
		LastErrorMessage: slot.LastErrorMessage,
		CreatedAt:        slot.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        slot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Version:          slot.Version,
	}
}

// WorkspaceMaterializationToProto maps one domain materialization attempt.
func WorkspaceMaterializationToProto(materialization entity.WorkspaceMaterialization) *runtimev1.WorkspaceMaterialization {
	return &runtimev1.WorkspaceMaterialization{
		WorkspaceMaterializationId: materialization.ID.String(),
		SlotId:                     materialization.SlotID.String(),
		Status:                     WorkspaceMaterializationStatusToProto(materialization.Status),
		PolicyDigest:               materialization.PolicyDigest,
		Sources:                    WorkspaceSourcesToProto(materialization.Sources),
		Fingerprint:                materialization.Fingerprint,
		StartedAt:                  timeStringPtr(materialization.StartedAt),
		FinishedAt:                 timeStringPtr(materialization.FinishedAt),
		LastErrorCode:              materialization.LastErrorCode,
		LastErrorMessage:           materialization.LastErrorMessage,
		Version:                    materialization.Version,
	}
}

// RuntimeContextToProto maps a prepared runtime context.
func RuntimeContextToProto(context runtimeservice.RuntimeContext) *runtimev1.RuntimeContext {
	return &runtimev1.RuntimeContext{
		SlotId:                     context.SlotID.String(),
		AgentRunId:                 uuidStringPtr(context.AgentRunID),
		FleetScopeId:               uuidStringPtr(context.FleetScopeID),
		ClusterId:                  uuidStringPtr(context.ClusterID),
		NamespaceName:              context.NamespaceName,
		RuntimeProfile:             context.RuntimeProfile,
		WorkspaceRoot:              context.WorkspaceRoot,
		MaterializationFingerprint: context.MaterializationFingerprint,
	}
}

// JobToProto maps one platform job.
func JobToProto(job entity.Job) *runtimev1.Job {
	return &runtimev1.Job{
		JobId:                 job.ID.String(),
		CommandId:             job.CommandID,
		JobType:               JobTypeToProto(job.JobType),
		Status:                JobStatusToProto(job.Status),
		Priority:              JobPriorityToProto(job.Priority),
		JobInputJson:          string(job.JobInputJSON),
		LeaseOwner:            job.LeaseOwner,
		LeaseUntil:            timeStringPtr(job.LeaseUntil),
		ClaimAttempt:          job.ClaimAttempt,
		SlotId:                uuidStringPtr(job.SlotID),
		AgentRunId:            uuidStringPtr(job.AgentRunID),
		ProjectId:             uuidStringPtr(job.ProjectID),
		RepositoryId:          uuidStringPtr(job.RepositoryID),
		ReleaseLineId:         uuidStringPtr(job.ReleaseLineID),
		PackageInstallationId: uuidStringPtr(job.PackageInstallationID),
		FleetScopeId:          uuidStringPtr(job.FleetScopeID),
		ClusterId:             uuidStringPtr(job.ClusterID),
		RequestedBy:           uuidStringPtr(job.RequestedBy),
		CreatedAt:             job.CreatedAt.UTC().Format(time.RFC3339Nano),
		StartedAt:             timeStringPtr(job.StartedAt),
		FinishedAt:            timeStringPtr(job.FinishedAt),
		NextAction:            job.NextAction,
		LastErrorCode:         job.LastErrorCode,
		LastErrorMessage:      job.LastErrorMessage,
		ShortLogTail:          job.ShortLogTail,
		FullLogRef:            job.FullLogRef,
		Version:               job.Version,
		Steps:                 jobStepsToProto(job.Steps),
	}
}

// JobStepToProto maps one job step.
func JobStepToProto(step entity.JobStep) *runtimev1.JobStep {
	return &runtimev1.JobStep{
		JobStepId:    step.ID.String(),
		JobId:        step.JobID.String(),
		StepKey:      step.StepKey,
		Status:       JobStepStatusToProto(step.Status),
		StartedAt:    timeStringPtr(step.StartedAt),
		FinishedAt:   timeStringPtr(step.FinishedAt),
		ShortLogTail: step.ShortLogTail,
		ExternalRef:  step.ExternalRef,
		ErrorCode:    step.ErrorCode,
		ErrorMessage: step.ErrorMessage,
		Version:      step.Version,
	}
}

// RuntimeArtifactRefToProto maps one external runtime artifact reference.
func RuntimeArtifactRefToProto(ref entity.RuntimeArtifactRef) *runtimev1.RuntimeArtifactRef {
	return &runtimev1.RuntimeArtifactRef{
		RuntimeArtifactRefId: ref.ID.String(),
		JobId:                uuidStringPtr(ref.JobID),
		SlotId:               uuidStringPtr(ref.SlotID),
		ArtifactType:         RuntimeArtifactTypeToProto(ref.ArtifactType),
		ExternalRef:          ref.ExternalRef,
		Digest:               ref.Digest,
		MetadataJson:         string(ref.MetadataJSON),
		CreatedAt:            ref.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// CleanupPolicyToProto maps one cleanup policy.
func CleanupPolicyToProto(policy entity.CleanupPolicy) *runtimev1.CleanupPolicy {
	return &runtimev1.CleanupPolicy{
		CleanupPolicyId:  policy.ID.String(),
		ScopeType:        RuntimeScopeTypeToProto(policy.ScopeType),
		ScopeId:          optionalStringPtr(policy.ScopeID),
		TtlSeconds:       policy.TTLSeconds,
		FailedTtlSeconds: policy.FailedTTLSeconds,
		KeepShortLogTail: policy.KeepShortLogTail,
		Status:           CleanupPolicyStatusToProto(policy.Status),
		CreatedAt:        policy.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        policy.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Version:          policy.Version,
	}
}

// PrewarmPoolToProto maps one prewarm pool.
func PrewarmPoolToProto(pool entity.PrewarmPool) *runtimev1.PrewarmPool {
	return &runtimev1.PrewarmPool{
		PrewarmPoolId:      pool.ID.String(),
		ScopeType:          PrewarmPoolScopeTypeToProto(pool.ScopeType),
		ScopeId:            optionalStringPtr(pool.ScopeID),
		RuntimeProfile:     pool.RuntimeProfile,
		FleetScopeId:       uuidStringPtr(pool.FleetScopeID),
		TargetSize:         pool.TargetSize,
		Status:             PrewarmPoolStatusToProto(pool.Status),
		LastCapacityStatus: CapacityStatusToProto(pool.LastCapacityStatus),
		CreatedAt:          pool.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:          pool.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Version:            pool.Version,
	}
}

// WorkspaceSourcesToProto maps normalized workspace sources.
func WorkspaceSourcesToProto(sources []value.WorkspaceSource) []*runtimev1.WorkspaceSource {
	if len(sources) == 0 {
		return nil
	}
	result := make([]*runtimev1.WorkspaceSource, len(sources))
	for index := range sources {
		result[index] = WorkspaceSourceToProto(sources[index])
	}
	return result
}

// WorkspaceSourceToProto maps one normalized workspace source.
func WorkspaceSourceToProto(source value.WorkspaceSource) *runtimev1.WorkspaceSource {
	return &runtimev1.WorkspaceSource{
		SourceId:      source.SourceID,
		Kind:          WorkspaceSourceKindToProto(source.Kind),
		RepositoryId:  uuidStringPtr(source.RepositoryID),
		Provider:      optionalStringPtr(source.Provider),
		ProviderOwner: optionalStringPtr(source.ProviderOwner),
		ProviderName:  optionalStringPtr(source.ProviderName),
		SourceRef:     optionalStringPtr(source.SourceRef),
		CommitSha:     optionalStringPtr(source.CommitSHA),
		LocalPath:     source.LocalPath,
		AccessMode:    WorkspaceSourceAccessModeToProto(source.AccessMode),
		Digest:        optionalStringPtr(source.Digest),
		MetadataJson:  workspaceSourceMetadata(source.Metadata),
	}
}

func uuidStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func workspaceSourceMetadata(metadata json.RawMessage) string {
	if len(metadata) == 0 {
		return "{}"
	}
	return string(metadata)
}

func slotsToProto(slots []entity.Slot) []*runtimev1.Slot {
	result := []*runtimev1.Slot{}
	for index := range slots {
		result = append(result, SlotToProto(slots[index]))
	}
	return result
}

func workspaceMaterializationsToProto(materializations []entity.WorkspaceMaterialization) []*runtimev1.WorkspaceMaterialization {
	result := make([]*runtimev1.WorkspaceMaterialization, 0, len(materializations))
	for _, materialization := range materializations {
		result = append(result, WorkspaceMaterializationToProto(materialization))
	}
	return result
}

func jobsToProto(jobs []entity.Job) []*runtimev1.Job {
	result := make([]*runtimev1.Job, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, JobToProto(job))
	}
	return result
}

func jobStepsToProto(steps []entity.JobStep) []*runtimev1.JobStep {
	result := make([]*runtimev1.JobStep, 0, len(steps))
	for _, step := range steps {
		result = append(result, JobStepToProto(step))
	}
	return result
}

func runtimeArtifactRefsToProto(refs []entity.RuntimeArtifactRef) []*runtimev1.RuntimeArtifactRef {
	result := make([]*runtimev1.RuntimeArtifactRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, RuntimeArtifactRefToProto(ref))
	}
	return result
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}

func timeStringPtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := value.UTC().Format(time.RFC3339Nano)
	return &text
}
