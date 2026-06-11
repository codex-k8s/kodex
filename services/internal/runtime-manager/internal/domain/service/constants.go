package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
)

const (
	operationReserveSlot      = "domain.Service.ReserveSlot"
	operationExtendSlotLease  = "domain.Service.ExtendSlotLease"
	operationReleaseSlot      = "domain.Service.ReleaseSlot"
	operationMarkSlotFailed   = "domain.Service.MarkSlotFailed"
	operationGetSlot          = "domain.Service.GetSlot"
	operationListSlots        = "domain.Service.ListSlots"
	operationPrepareRuntime   = "domain.Service.PrepareRuntime"
	operationStartWorkspace   = "domain.Service.StartWorkspaceMaterialization"
	operationReportWorkspace  = "domain.Service.ReportWorkspaceMaterializationProgress"
	operationGetWorkspace     = "domain.Service.GetWorkspaceMaterialization"
	operationListWorkspaces   = "domain.Service.ListWorkspaceMaterializations"
	operationPrepareBuildCtx  = "domain.Service.PrepareBuildContext"
	operationReportBuildCtx   = "domain.Service.ReportBuildContextProgress"
	operationGetBuildCtx      = "domain.Service.GetBuildContext"
	operationCreateJob        = "domain.Service.CreateJob"
	operationClaimJob         = "domain.Service.ClaimRunnableJob"
	operationReportJobStep    = "domain.Service.ReportJobStepProgress"
	operationCompleteJob      = "domain.Service.CompleteJob"
	operationFailJob          = "domain.Service.FailJob"
	operationCancelJob        = "domain.Service.CancelJob"
	operationGetJob           = "domain.Service.GetJob"
	operationListJobs         = "domain.Service.ListJobs"
	operationRecordArtifact   = "domain.Service.RecordRuntimeArtifactRef"
	operationListArtifacts    = "domain.Service.ListRuntimeArtifactRefs"
	operationUpsertCleanup    = "domain.Service.CreateOrUpdateCleanupPolicy"
	operationRunCleanup       = "domain.Service.RunCleanupBatch"
	operationUpsertPrewarm    = "domain.Service.CreateOrUpdatePrewarmPool"
	operationReconcilePool    = "domain.Service.ReconcilePrewarmPool"
	aggregateTypeSlot         = runtimeevents.AggregateSlot
	aggregateTypeWorkspace    = runtimeevents.AggregateWorkspaceMaterialization
	aggregateTypeBuildContext = "runtime_build_context"
	aggregateTypeJob          = runtimeevents.AggregateJob
	aggregateTypeCleanup      = runtimeevents.AggregateCleanupPolicy
	aggregateTypePrewarmPool  = runtimeevents.AggregatePrewarmPool
	eventSlotReserved         = runtimeevents.EventSlotReserved
	eventSlotLeaseExtended    = runtimeevents.EventSlotLeaseExtended
	eventSlotReleased         = runtimeevents.EventSlotReleased
	eventSlotFailed           = runtimeevents.EventSlotFailed
	eventSlotCleanupRequested = runtimeevents.EventSlotCleanupRequested
	eventSlotCleaned          = runtimeevents.EventSlotCleaned
	eventCleanupFailed        = runtimeevents.EventCleanupFailed
	eventWorkspaceStarted     = runtimeevents.EventWorkspaceMaterializationStarted
	eventWorkspaceCompleted   = runtimeevents.EventWorkspaceMaterializationCompleted
	eventWorkspaceFailed      = runtimeevents.EventWorkspaceMaterializationFailed
	eventJobCreated           = runtimeevents.EventJobCreated
	eventJobStarted           = runtimeevents.EventJobStarted
	eventJobStepUpdated       = runtimeevents.EventJobStepUpdated
	eventJobCompleted         = runtimeevents.EventJobCompleted
	eventJobFailed            = runtimeevents.EventJobFailed
	eventJobCancelled         = runtimeevents.EventJobCancelled
	eventPrewarmChanged       = runtimeevents.EventPrewarmCapacityChanged
	actionSlotReserve         = accesscatalog.ActionRuntimeSlotReserve
	actionSlotExtendLease     = accesscatalog.ActionRuntimeSlotExtendLease
	actionSlotRelease         = accesscatalog.ActionRuntimeSlotRelease
	actionSlotFail            = accesscatalog.ActionRuntimeSlotFail
	actionSlotRead            = accesscatalog.ActionRuntimeSlotRead
	actionSlotList            = accesscatalog.ActionRuntimeSlotList
	actionWorkspaceStart      = accesscatalog.ActionRuntimeWorkspaceMaterializationStart
	actionWorkspaceReport     = accesscatalog.ActionRuntimeWorkspaceMaterializationReport
	actionWorkspaceRead       = accesscatalog.ActionRuntimeWorkspaceMaterializationRead
	actionWorkspaceList       = accesscatalog.ActionRuntimeWorkspaceMaterializationList
	actionBuildContextPrepare = accesscatalog.ActionRuntimeBuildContextPrepare
	actionBuildContextReport  = accesscatalog.ActionRuntimeBuildContextReport
	actionBuildContextRead    = accesscatalog.ActionRuntimeBuildContextRead
	actionJobCreate           = accesscatalog.ActionRuntimeJobCreate
	actionJobClaim            = accesscatalog.ActionRuntimeJobClaim
	actionJobStepReport       = accesscatalog.ActionRuntimeJobStepReport
	actionJobComplete         = accesscatalog.ActionRuntimeJobComplete
	actionJobFail             = accesscatalog.ActionRuntimeJobFail
	actionJobCancel           = accesscatalog.ActionRuntimeJobCancel
	actionJobRead             = accesscatalog.ActionRuntimeJobRead
	actionJobList             = accesscatalog.ActionRuntimeJobList
	actionArtifactRecord      = accesscatalog.ActionRuntimeArtifactRefRecord
	actionArtifactList        = accesscatalog.ActionRuntimeArtifactRefList
	actionCleanupUpsert       = accesscatalog.ActionRuntimeCleanupPolicyUpsert
	actionCleanupRun          = accesscatalog.ActionRuntimeCleanupRun
	actionPrewarmUpsert       = accesscatalog.ActionRuntimePrewarmPoolUpsert
	actionPrewarmReconcile    = accesscatalog.ActionRuntimePrewarmPoolReconcile
)
