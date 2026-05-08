package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
)

const (
	operationReserveSlot     = "domain.Service.ReserveSlot"
	operationExtendSlotLease = "domain.Service.ExtendSlotLease"
	operationReleaseSlot     = "domain.Service.ReleaseSlot"
	operationMarkSlotFailed  = "domain.Service.MarkSlotFailed"
	operationGetSlot         = "domain.Service.GetSlot"
	operationListSlots       = "domain.Service.ListSlots"
	operationPrepareRuntime  = "domain.Service.PrepareRuntime"
	operationStartWorkspace  = "domain.Service.StartWorkspaceMaterialization"
	operationReportWorkspace = "domain.Service.ReportWorkspaceMaterializationProgress"
	operationGetWorkspace    = "domain.Service.GetWorkspaceMaterialization"
	operationListWorkspaces  = "domain.Service.ListWorkspaceMaterializations"
	operationCreateJob       = "domain.Service.CreateJob"
	operationClaimJob        = "domain.Service.ClaimRunnableJob"
	operationReportJobStep   = "domain.Service.ReportJobStepProgress"
	operationCompleteJob     = "domain.Service.CompleteJob"
	operationFailJob         = "domain.Service.FailJob"
	operationCancelJob       = "domain.Service.CancelJob"
	operationGetJob          = "domain.Service.GetJob"
	operationListJobs        = "domain.Service.ListJobs"
	operationRecordArtifact  = "domain.Service.RecordRuntimeArtifactRef"
	operationListArtifacts   = "domain.Service.ListRuntimeArtifactRefs"
	aggregateTypeSlot        = runtimeevents.AggregateSlot
	aggregateTypeWorkspace   = runtimeevents.AggregateWorkspaceMaterialization
	aggregateTypeJob         = runtimeevents.AggregateJob
	eventSlotReserved        = runtimeevents.EventSlotReserved
	eventSlotLeaseExtended   = runtimeevents.EventSlotLeaseExtended
	eventSlotReleased        = runtimeevents.EventSlotReleased
	eventSlotFailed          = runtimeevents.EventSlotFailed
	eventWorkspaceStarted    = runtimeevents.EventWorkspaceMaterializationStarted
	eventWorkspaceCompleted  = runtimeevents.EventWorkspaceMaterializationCompleted
	eventWorkspaceFailed     = runtimeevents.EventWorkspaceMaterializationFailed
	eventJobCreated          = runtimeevents.EventJobCreated
	eventJobStarted          = runtimeevents.EventJobStarted
	eventJobStepUpdated      = runtimeevents.EventJobStepUpdated
	eventJobCompleted        = runtimeevents.EventJobCompleted
	eventJobFailed           = runtimeevents.EventJobFailed
	eventJobCancelled        = runtimeevents.EventJobCancelled
	actionSlotReserve        = accesscatalog.ActionRuntimeSlotReserve
	actionSlotExtendLease    = accesscatalog.ActionRuntimeSlotExtendLease
	actionSlotRelease        = accesscatalog.ActionRuntimeSlotRelease
	actionSlotFail           = accesscatalog.ActionRuntimeSlotFail
	actionSlotRead           = accesscatalog.ActionRuntimeSlotRead
	actionSlotList           = accesscatalog.ActionRuntimeSlotList
	actionWorkspaceStart     = accesscatalog.ActionRuntimeWorkspaceMaterializationStart
	actionWorkspaceReport    = accesscatalog.ActionRuntimeWorkspaceMaterializationReport
	actionWorkspaceRead      = accesscatalog.ActionRuntimeWorkspaceMaterializationRead
	actionWorkspaceList      = accesscatalog.ActionRuntimeWorkspaceMaterializationList
	actionJobCreate          = accesscatalog.ActionRuntimeJobCreate
	actionJobClaim           = accesscatalog.ActionRuntimeJobClaim
	actionJobStepReport      = accesscatalog.ActionRuntimeJobStepReport
	actionJobComplete        = accesscatalog.ActionRuntimeJobComplete
	actionJobFail            = accesscatalog.ActionRuntimeJobFail
	actionJobCancel          = accesscatalog.ActionRuntimeJobCancel
	actionJobRead            = accesscatalog.ActionRuntimeJobRead
	actionJobList            = accesscatalog.ActionRuntimeJobList
	actionArtifactRecord     = accesscatalog.ActionRuntimeArtifactRefRecord
	actionArtifactList       = accesscatalog.ActionRuntimeArtifactRefList
)
