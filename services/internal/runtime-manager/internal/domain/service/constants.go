package service

import runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"

const (
	operationReserveSlot     = "domain.Service.ReserveSlot"
	operationExtendSlotLease = "domain.Service.ExtendSlotLease"
	operationReleaseSlot     = "domain.Service.ReleaseSlot"
	operationMarkSlotFailed  = "domain.Service.MarkSlotFailed"
	operationGetSlot         = "domain.Service.GetSlot"
	operationListSlots       = "domain.Service.ListSlots"
	aggregateTypeSlot        = runtimeevents.AggregateSlot
	eventSlotReserved        = runtimeevents.EventSlotReserved
	eventSlotLeaseExtended   = runtimeevents.EventSlotLeaseExtended
	eventSlotReleased        = runtimeevents.EventSlotReleased
	eventSlotFailed          = runtimeevents.EventSlotFailed
)
