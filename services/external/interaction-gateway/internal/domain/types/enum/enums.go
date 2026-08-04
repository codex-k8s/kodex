// Package enum содержит закрытые множества interaction-gateway.
package enum

type InboundKind string

const (
	InboundPost           InboundKind = "POST"
	InboundSlash          InboundKind = "SLASH"
	InboundAction         InboundKind = "ACTION"
	InboundDialog         InboundKind = "DIALOG"
	InboundReaction       InboundKind = "REACTION"
	InboundChannelDelete  InboundKind = "CHANNEL_DELETE"
	InboundChannelRestore InboundKind = "CHANNEL_RESTORE"
	InboundThreadDelete   InboundKind = "THREAD_DELETE"
	InboundThreadRestore  InboundKind = "THREAD_RESTORE"
)

func (kind InboundKind) Valid() bool {
	switch kind {
	case InboundPost, InboundSlash, InboundAction, InboundDialog, InboundReaction,
		InboundChannelDelete, InboundChannelRestore, InboundThreadDelete, InboundThreadRestore:
		return true
	default:
		return false
	}
}

type InboundState string

const (
	InboundPending        InboundState = "PENDING"
	InboundProcessing     InboundState = "PROCESSING"
	InboundWaitingScan    InboundState = "WAITING_SCAN"
	InboundCompleted      InboundState = "COMPLETED"
	InboundIgnored        InboundState = "IGNORED"
	InboundFailed         InboundState = "FAILED"
	InboundWaitingCleanup InboundState = "WAITING_CLEANUP"
)

type DeliveryKind string

const (
	DeliveryRun           DeliveryKind = "RUN"
	DeliveryStatus        DeliveryKind = "STATUS"
	DeliveryIncident      DeliveryKind = "INCIDENT"
	DeliveryOwnerDecision DeliveryKind = "OWNER_DECISION"
	DeliveryArtifact      DeliveryKind = "ARTIFACT"
)

func (kind DeliveryKind) Valid() bool {
	switch kind {
	case DeliveryRun, DeliveryStatus, DeliveryIncident, DeliveryOwnerDecision, DeliveryArtifact:
		return true
	default:
		return false
	}
}

type DeliveryState string

const (
	DeliveryPending          DeliveryState = "PENDING"
	DeliveryDelivering       DeliveryState = "DELIVERING"
	DeliveryProviderAccepted DeliveryState = "PROVIDER_ACCEPTED"
	DeliveryDelivered        DeliveryState = "DELIVERED"
	DeliveryDeadLetter       DeliveryState = "DEAD_LETTER"
)

func (state DeliveryState) Terminal() bool {
	return state == DeliveryDelivered || state == DeliveryDeadLetter
}

type OwnerDecision string

const (
	DecisionApprove          OwnerDecision = "APPROVE"
	DecisionReject           OwnerDecision = "REJECT"
	DecisionChangesRequested OwnerDecision = "CHANGES_REQUESTED"
	DecisionCancel           OwnerDecision = "CANCEL"
	DecisionStop             OwnerDecision = "STOP"
	DecisionRetry            OwnerDecision = "RETRY"
)

func (decision OwnerDecision) Valid() bool {
	switch decision {
	case DecisionApprove, DecisionReject, DecisionChangesRequested, DecisionCancel,
		DecisionStop, DecisionRetry:
		return true
	default:
		return false
	}
}
