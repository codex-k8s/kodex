// Package enum содержит закрытые множества interaction-gateway.
package enum

type InboundKind string

const (
	InboundPost     InboundKind = "POST"
	InboundSlash    InboundKind = "SLASH"
	InboundAction   InboundKind = "ACTION"
	InboundDialog   InboundKind = "DIALOG"
	InboundReaction InboundKind = "REACTION"
)

func (kind InboundKind) Valid() bool {
	switch kind {
	case InboundPost, InboundSlash, InboundAction, InboundDialog, InboundReaction:
		return true
	default:
		return false
	}
}

type InboundState string

const (
	InboundPending     InboundState = "PENDING"
	InboundProcessing  InboundState = "PROCESSING"
	InboundWaitingScan InboundState = "WAITING_SCAN"
	InboundCompleted   InboundState = "COMPLETED"
	InboundIgnored     InboundState = "IGNORED"
	InboundFailed      InboundState = "FAILED"
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
)

func (decision OwnerDecision) Valid() bool {
	switch decision {
	case DecisionApprove, DecisionReject, DecisionChangesRequested, DecisionCancel:
		return true
	default:
		return false
	}
}
