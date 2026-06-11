// Package grpc exposes runtime-manager through the generated gRPC contract.
package grpc

import (
	"context"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	grpccasters "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ runtimev1.RuntimeManagerServiceServer = (*Server)(nil)

type runtimeService interface {
	PrepareRuntime(context.Context, runtimeservice.PrepareRuntimeInput) (runtimeservice.PrepareRuntimeResult, error)
	ReserveSlot(context.Context, runtimeservice.ReserveSlotInput) (entity.Slot, error)
	ExtendSlotLease(context.Context, runtimeservice.ExtendSlotLeaseInput) (entity.Slot, error)
	ReleaseSlot(context.Context, runtimeservice.ReleaseSlotInput) (entity.Slot, error)
	MarkSlotFailed(context.Context, runtimeservice.MarkSlotFailedInput) (entity.Slot, error)
	GetSlot(context.Context, runtimeservice.GetSlotInput) (entity.Slot, error)
	ListSlots(context.Context, runtimeservice.ListSlotsInput) (runtimeservice.ListSlotsResult, error)
	StartWorkspaceMaterialization(context.Context, runtimeservice.StartWorkspaceMaterializationInput) (entity.WorkspaceMaterialization, error)
	ReportWorkspaceMaterializationProgress(context.Context, runtimeservice.ReportWorkspaceMaterializationProgressInput) (entity.WorkspaceMaterialization, error)
	GetWorkspaceMaterialization(context.Context, runtimeservice.GetWorkspaceMaterializationInput) (entity.WorkspaceMaterialization, error)
	ListWorkspaceMaterializations(context.Context, runtimeservice.ListWorkspaceMaterializationsInput) (runtimeservice.ListWorkspaceMaterializationsResult, error)
	PrepareBuildContext(context.Context, runtimeservice.PrepareBuildContextInput) (entity.BuildContext, error)
	ReportBuildContextProgress(context.Context, runtimeservice.ReportBuildContextProgressInput) (entity.BuildContext, error)
	GetBuildContext(context.Context, runtimeservice.GetBuildContextInput) (entity.BuildContext, error)
	CreateJob(context.Context, runtimeservice.CreateJobInput) (entity.Job, error)
	ClaimRunnableJob(context.Context, runtimeservice.ClaimRunnableJobInput) (runtimeservice.ClaimRunnableJobResult, error)
	ReportJobStepProgress(context.Context, runtimeservice.ReportJobStepProgressInput) (entity.Job, error)
	CompleteJob(context.Context, runtimeservice.CompleteJobInput) (entity.Job, error)
	FailJob(context.Context, runtimeservice.FailJobInput) (entity.Job, error)
	CancelJob(context.Context, runtimeservice.CancelJobInput) (entity.Job, error)
	GetJob(context.Context, runtimeservice.GetJobInput) (entity.Job, error)
	ListJobs(context.Context, runtimeservice.ListJobsInput) (runtimeservice.ListJobsResult, error)
	RecordRuntimeArtifactRef(context.Context, runtimeservice.RecordRuntimeArtifactRefInput) (entity.RuntimeArtifactRef, error)
	ListRuntimeArtifactRefs(context.Context, runtimeservice.ListRuntimeArtifactRefsInput) (runtimeservice.ListRuntimeArtifactRefsResult, error)
	CreateOrUpdateCleanupPolicy(context.Context, runtimeservice.CreateOrUpdateCleanupPolicyInput) (entity.CleanupPolicy, error)
	RunCleanupBatch(context.Context, runtimeservice.RunCleanupBatchInput) (runtimeservice.RunCleanupBatchResult, error)
	CreateOrUpdatePrewarmPool(context.Context, runtimeservice.CreateOrUpdatePrewarmPoolInput) (entity.PrewarmPool, error)
	ReconcilePrewarmPool(context.Context, runtimeservice.ReconcilePrewarmPoolInput) (entity.PrewarmPool, error)
}

// Server exposes the generated runtime-manager service.
type Server struct {
	runtimev1.UnimplementedRuntimeManagerServiceServer
	service runtimeService
}

// NewServer creates a runtime-manager gRPC transport.
func NewServer(service runtimeService) *Server {
	if service == nil {
		panic("runtime-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterRuntimeManagerService registers runtime-manager gRPC handlers.
func RegisterRuntimeManagerService(registrar grpcruntime.ServiceRegistrar, service runtimeService) {
	runtimev1.RegisterRuntimeManagerServiceServer(registrar, NewServer(service))
}

// PrepareRuntime resolves placement, reserves a slot and starts workspace materialization.
func (s *Server) PrepareRuntime(ctx context.Context, request *runtimev1.PrepareRuntimeRequest) (*runtimev1.PrepareRuntimeResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.PrepareRuntimeInput, s.service.PrepareRuntime, grpccasters.PrepareRuntimeResponse)
}

// ReserveSlot reserves or creates a runtime slot.
func (s *Server) ReserveSlot(ctx context.Context, request *runtimev1.ReserveSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReserveSlotInput, s.service.ReserveSlot, grpccasters.SlotResponse)
}

// ExtendSlotLease extends active slot lease.
func (s *Server) ExtendSlotLease(ctx context.Context, request *runtimev1.ExtendSlotLeaseRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ExtendSlotLeaseInput, s.service.ExtendSlotLease, grpccasters.SlotResponse)
}

// ReleaseSlot releases a slot after work finishes.
func (s *Server) ReleaseSlot(ctx context.Context, request *runtimev1.ReleaseSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReleaseSlotInput, s.service.ReleaseSlot, grpccasters.SlotResponse)
}

// MarkSlotFailed marks a slot as failed with a classified error.
func (s *Server) MarkSlotFailed(ctx context.Context, request *runtimev1.MarkSlotFailedRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.MarkSlotFailedInput, s.service.MarkSlotFailed, grpccasters.SlotResponse)
}

// GetSlot returns authoritative slot state.
func (s *Server) GetSlot(ctx context.Context, request *runtimev1.GetSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetSlotInput, s.service.GetSlot, grpccasters.SlotResponse)
}

// ListSlots returns slots by project, status, runtime profile or fleet scope.
func (s *Server) ListSlots(ctx context.Context, request *runtimev1.ListSlotsRequest) (*runtimev1.ListSlotsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListSlotsInput, s.service.ListSlots, grpccasters.ListSlotsResponse)
}

// StartWorkspaceMaterialization starts source preparation inside a slot.
func (s *Server) StartWorkspaceMaterialization(ctx context.Context, request *runtimev1.StartWorkspaceMaterializationRequest) (*runtimev1.WorkspaceMaterializationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.StartWorkspaceMaterializationInput, s.service.StartWorkspaceMaterialization, grpccasters.WorkspaceMaterializationResponse)
}

// ReportWorkspaceMaterializationProgress updates materialization status, fingerprint and classified error.
func (s *Server) ReportWorkspaceMaterializationProgress(ctx context.Context, request *runtimev1.ReportWorkspaceMaterializationProgressRequest) (*runtimev1.WorkspaceMaterializationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReportWorkspaceMaterializationProgressInput, s.service.ReportWorkspaceMaterializationProgress, grpccasters.WorkspaceMaterializationResponse)
}

// GetWorkspaceMaterialization returns one materialization attempt.
func (s *Server) GetWorkspaceMaterialization(ctx context.Context, request *runtimev1.GetWorkspaceMaterializationRequest) (*runtimev1.WorkspaceMaterializationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetWorkspaceMaterializationInput, s.service.GetWorkspaceMaterialization, grpccasters.WorkspaceMaterializationResponse)
}

// ListWorkspaceMaterializations returns materialization attempts by slot or agent run.
func (s *Server) ListWorkspaceMaterializations(ctx context.Context, request *runtimev1.ListWorkspaceMaterializationsRequest) (*runtimev1.ListWorkspaceMaterializationsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListWorkspaceMaterializationsInput, s.service.ListWorkspaceMaterializations, grpccasters.ListWorkspaceMaterializationsResponse)
}

// PrepareBuildContext records a runtime-owned build context request.
func (s *Server) PrepareBuildContext(ctx context.Context, request *runtimev1.PrepareBuildContextRequest) (*runtimev1.BuildContextResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.PrepareBuildContextInput, s.service.PrepareBuildContext, grpccasters.BuildContextResponse)
}

// ReportBuildContextProgress updates build context materialization status.
func (s *Server) ReportBuildContextProgress(ctx context.Context, request *runtimev1.ReportBuildContextProgressRequest) (*runtimev1.BuildContextResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReportBuildContextProgressInput, s.service.ReportBuildContextProgress, grpccasters.BuildContextResponse)
}

// GetBuildContext returns one build context materialization attempt.
func (s *Server) GetBuildContext(ctx context.Context, request *runtimev1.GetBuildContextRequest) (*runtimev1.BuildContextResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetBuildContextInput, s.service.GetBuildContext, grpccasters.BuildContextResponse)
}

// CreateJob creates a platform technical job.
func (s *Server) CreateJob(ctx context.Context, request *runtimev1.CreateJobRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateJobInput, s.service.CreateJob, grpccasters.JobResponse)
}

// ClaimRunnableJob claims a runnable job and returns a one-time lease token.
func (s *Server) ClaimRunnableJob(ctx context.Context, request *runtimev1.ClaimRunnableJobRequest) (*runtimev1.ClaimRunnableJobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ClaimRunnableJobInput, s.service.ClaimRunnableJob, grpccasters.ClaimRunnableJobResponse)
}

// ReportJobStepProgress updates job step progress and bounded diagnostics.
func (s *Server) ReportJobStepProgress(ctx context.Context, request *runtimev1.ReportJobStepProgressRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReportJobStepProgressInput, s.service.ReportJobStepProgress, grpccasters.JobResponse)
}

// CompleteJob completes a job successfully.
func (s *Server) CompleteJob(ctx context.Context, request *runtimev1.CompleteJobRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CompleteJobInput, s.service.CompleteJob, grpccasters.JobResponse)
}

// FailJob completes a job with a classified failure.
func (s *Server) FailJob(ctx context.Context, request *runtimev1.FailJobRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.FailJobInput, s.service.FailJob, grpccasters.JobResponse)
}

// CancelJob cancels a non-terminal job.
func (s *Server) CancelJob(ctx context.Context, request *runtimev1.CancelJobRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CancelJobInput, s.service.CancelJob, grpccasters.JobResponse)
}

// GetJob returns authoritative job state.
func (s *Server) GetJob(ctx context.Context, request *runtimev1.GetJobRequest) (*runtimev1.JobResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetJobInput, s.service.GetJob, grpccasters.JobResponse)
}

// ListJobs returns platform jobs by filters.
func (s *Server) ListJobs(ctx context.Context, request *runtimev1.ListJobsRequest) (*runtimev1.ListJobsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListJobsInput, s.service.ListJobs, grpccasters.ListJobsResponse)
}

// RecordRuntimeArtifactRef stores a reference to an external runtime artifact.
func (s *Server) RecordRuntimeArtifactRef(ctx context.Context, request *runtimev1.RecordRuntimeArtifactRefRequest) (*runtimev1.RuntimeArtifactRefResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RecordRuntimeArtifactRefInput, s.service.RecordRuntimeArtifactRef, grpccasters.RuntimeArtifactRefResponse)
}

// ListRuntimeArtifactRefs returns external artifact references by job or slot.
func (s *Server) ListRuntimeArtifactRefs(ctx context.Context, request *runtimev1.ListRuntimeArtifactRefsRequest) (*runtimev1.ListRuntimeArtifactRefsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListRuntimeArtifactRefsInput, s.service.ListRuntimeArtifactRefs, grpccasters.ListRuntimeArtifactRefsResponse)
}

// CreateOrUpdateCleanupPolicy creates or updates runtime retention policy.
func (s *Server) CreateOrUpdateCleanupPolicy(ctx context.Context, request *runtimev1.CreateOrUpdateCleanupPolicyRequest) (*runtimev1.CleanupPolicyResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateOrUpdateCleanupPolicyInput, s.service.CreateOrUpdateCleanupPolicy, grpccasters.CleanupPolicyResponse)
}

// RunCleanupBatch claims and executes one cleanup batch.
func (s *Server) RunCleanupBatch(ctx context.Context, request *runtimev1.RunCleanupBatchRequest) (*runtimev1.RunCleanupBatchResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RunCleanupBatchInput, s.service.RunCleanupBatch, grpccasters.RunCleanupBatchResponse)
}

// CreateOrUpdatePrewarmPool creates or updates desired prewarmed slot capacity.
func (s *Server) CreateOrUpdatePrewarmPool(ctx context.Context, request *runtimev1.CreateOrUpdatePrewarmPoolRequest) (*runtimev1.PrewarmPoolResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateOrUpdatePrewarmPoolInput, s.service.CreateOrUpdatePrewarmPool, grpccasters.PrewarmPoolResponse)
}

// ReconcilePrewarmPool reconciles actual prewarmed slots with target pool size.
func (s *Server) ReconcilePrewarmPool(ctx context.Context, request *runtimev1.ReconcilePrewarmPoolRequest) (*runtimev1.PrewarmPoolResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReconcilePrewarmPoolInput, s.service.ReconcilePrewarmPool, grpccasters.PrewarmPoolResponse)
}
