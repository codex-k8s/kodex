package enum

// Operation identifies one stable interaction-hub use case.
type Operation string

const (
	OperationCreateConversationThread  Operation = "interaction.CreateConversationThread"
	OperationRecordConversationMessage Operation = "interaction.RecordConversationMessage"
	OperationGetConversationThread     Operation = "interaction.GetConversationThread"
	OperationListConversationMessages  Operation = "interaction.ListConversationMessages"
	OperationRequestFeedback           Operation = "interaction.RequestFeedback"
	OperationRequestApproval           Operation = "interaction.RequestApproval"
	OperationRequestHumanGate          Operation = "interaction.RequestHumanGate"
	OperationRecordInteractionResponse Operation = "interaction.RecordInteractionResponse"
	OperationCancelInteractionRequest  Operation = "interaction.CancelInteractionRequest"
	OperationExpireInteractionRequests Operation = "interaction.ExpireInteractionRequests"
	OperationGetInteractionRequest     Operation = "interaction.GetInteractionRequest"
	OperationListInteractionRequests   Operation = "interaction.ListInteractionRequests"
	OperationListOwnerInboxItems       Operation = "interaction.ListOwnerInboxItems"
	OperationRequestNotification       Operation = "interaction.RequestNotification"
	OperationUpsertSubscription        Operation = "interaction.UpsertSubscription"
	OperationDisableSubscription       Operation = "interaction.DisableSubscription"
	OperationListSubscriptions         Operation = "interaction.ListSubscriptions"
	OperationPlanDelivery              Operation = "interaction.PlanDelivery"
	OperationRecordDeliveryResult      Operation = "interaction.RecordDeliveryResult"
	OperationRecordChannelCallback     Operation = "interaction.RecordChannelCallback"
	OperationGetDeliveryStatus         Operation = "interaction.GetDeliveryStatus"
)

// Valid reports whether an operation belongs to the stable interaction-hub contract.
func (o Operation) Valid() bool {
	switch o {
	case OperationCreateConversationThread,
		OperationRecordConversationMessage,
		OperationGetConversationThread,
		OperationListConversationMessages,
		OperationRequestFeedback,
		OperationRequestApproval,
		OperationRequestHumanGate,
		OperationRecordInteractionResponse,
		OperationCancelInteractionRequest,
		OperationExpireInteractionRequests,
		OperationGetInteractionRequest,
		OperationListInteractionRequests,
		OperationListOwnerInboxItems,
		OperationRequestNotification,
		OperationUpsertSubscription,
		OperationDisableSubscription,
		OperationListSubscriptions,
		OperationPlanDelivery,
		OperationRecordDeliveryResult,
		OperationRecordChannelCallback,
		OperationGetDeliveryStatus:
		return true
	default:
		return false
	}
}
