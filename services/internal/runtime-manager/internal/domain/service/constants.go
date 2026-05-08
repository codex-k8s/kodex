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
	aggregateTypeSlot        = runtimeevents.AggregateSlot
	aggregateTypeWorkspace   = runtimeevents.AggregateWorkspaceMaterialization
	eventSlotReserved        = runtimeevents.EventSlotReserved
	eventSlotLeaseExtended   = runtimeevents.EventSlotLeaseExtended
	eventSlotReleased        = runtimeevents.EventSlotReleased
	eventSlotFailed          = runtimeevents.EventSlotFailed
	eventWorkspaceStarted    = runtimeevents.EventWorkspaceMaterializationStarted
	eventWorkspaceCompleted  = runtimeevents.EventWorkspaceMaterializationCompleted
	eventWorkspaceFailed     = runtimeevents.EventWorkspaceMaterializationFailed
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
)
