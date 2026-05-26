package casters

import (
	"strings"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func RequestNotificationInput(input *interactionsv1.RequestNotificationRequest) (interactionservice.RequestNotificationInput, error) {
	if input == nil {
		return interactionservice.RequestNotificationInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.RequestNotificationInput{}, err
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.RequestNotificationInput{}, err
	}
	requestID, err := OptionalUUID(input.GetRequestId())
	if err != nil {
		return interactionservice.RequestNotificationInput{}, err
	}
	subscriptionID, err := OptionalUUID(input.GetSubscriptionId())
	if err != nil {
		return interactionservice.RequestNotificationInput{}, err
	}
	expiresAt, err := OptionalTime(input.GetExpiresAt())
	if err != nil {
		return interactionservice.RequestNotificationInput{}, err
	}
	return interactionservice.RequestNotificationInput{
		Meta:                  meta,
		Scope:                 scope,
		NotificationKind:      NotificationKind(input.GetNotificationKind()),
		RequestID:             requestID,
		SubscriptionID:        subscriptionID,
		RecipientRefs:         ActorRefs(input.GetRecipientRefs()),
		MessageTemplateRef:    strings.TrimSpace(input.GetMessageTemplateRef()),
		MessageTitle:          strings.TrimSpace(input.GetMessageTitle()),
		MessageSummary:        strings.TrimSpace(input.GetMessageSummary()),
		BodyPreview:           strings.TrimSpace(input.GetBodyPreview()),
		Priority:              NotificationPriority(input.GetPriority()),
		ExpiresAt:             expiresAt,
		SourceOwner:           SourceOwnerRef(input.GetSourceOwner()),
		Ingress:               IngressRef(input.GetIngress()),
		ContextRefs:           ExternalRefs(input.GetContextRefs()),
		ChannelHintRefs:       ExternalRefs(input.GetChannelHintRefs()),
		NotificationPolicyRef: strings.TrimSpace(input.GetNotificationPolicyRef()),
	}, nil
}

func UpsertSubscriptionInput(input *interactionsv1.UpsertSubscriptionRequest) (interactionservice.UpsertSubscriptionInput, error) {
	if input == nil {
		return interactionservice.UpsertSubscriptionInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.UpsertSubscriptionInput{}, err
	}
	subscriptionID, err := OptionalUUID(input.GetSubscriptionId())
	if err != nil {
		return interactionservice.UpsertSubscriptionInput{}, err
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.UpsertSubscriptionInput{}, err
	}
	return interactionservice.UpsertSubscriptionInput{
		Meta:                    meta,
		SubscriptionID:          subscriptionID,
		Scope:                   scope,
		SubscriberRef:           ActorRef(input.GetSubscriberRef()),
		EventFilterJSON:         input.GetEventFilterJson(),
		DeliveryPreferencesJSON: input.GetDeliveryPreferencesJson(),
		Status:                  SubscriptionStatus(input.GetStatus()),
		SourceOwner:             SourceOwnerRef(input.GetSourceOwner()),
		ChannelHintRefs:         ExternalRefs(input.GetChannelHintRefs()),
		SubscriptionPolicyRef:   strings.TrimSpace(input.GetSubscriptionPolicyRef()),
	}, nil
}

func DisableSubscriptionInput(input *interactionsv1.DisableSubscriptionRequest) (interactionservice.DisableSubscriptionInput, error) {
	if input == nil {
		return interactionservice.DisableSubscriptionInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.DisableSubscriptionInput{}, err
	}
	subscriptionID, err := ParseUUID(input.GetSubscriptionId())
	if err != nil {
		return interactionservice.DisableSubscriptionInput{}, err
	}
	return interactionservice.DisableSubscriptionInput{Meta: meta, SubscriptionID: subscriptionID}, nil
}

func ListSubscriptionsInput(input *interactionsv1.ListSubscriptionsRequest) (interactionservice.ListSubscriptionsInput, error) {
	if input == nil {
		return interactionservice.ListSubscriptionsInput{}, errs.ErrInvalidArgument
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.ListSubscriptionsInput{}, err
	}
	result := interactionservice.ListSubscriptionsInput{
		Meta:          QueryMeta(input.GetMeta()),
		Scope:         scope,
		SubscriberRef: strings.TrimSpace(input.GetSubscriberRef()),
		Page:          PageRequest(input.GetPage()),
	}
	if input.Status != nil {
		result.Status = SubscriptionStatus(input.GetStatus())
	}
	return result, nil
}

func NotificationResponse(notification entity.Notification) *interactionsv1.NotificationResponse {
	return &interactionsv1.NotificationResponse{Notification: Notification(notification)}
}

func SubscriptionResponse(subscription entity.Subscription) *interactionsv1.SubscriptionResponse {
	return &interactionsv1.SubscriptionResponse{Subscription: Subscription(subscription)}
}

func ListSubscriptionsResponse(subscriptions []entity.Subscription, page value.PageResult) *interactionsv1.ListSubscriptionsResponse {
	items := make([]*interactionsv1.Subscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		items = append(items, Subscription(subscription))
	}
	return &interactionsv1.ListSubscriptionsResponse{Subscriptions: items, Page: PageResponse(page)}
}

func Notification(notification entity.Notification) *interactionsv1.Notification {
	return &interactionsv1.Notification{
		Id:                    notification.ID.String(),
		Scope:                 &interactionsv1.ScopeRef{Type: ScopeTypeProto(notification.Scope.Type), Ref: notification.Scope.Ref},
		NotificationKind:      NotificationKindProto(notification.NotificationKind),
		RequestId:             OptionalUUIDProto(notification.RequestID),
		SubscriptionId:        OptionalUUIDProto(notification.SubscriptionID),
		RecipientRefs:         ActorRefsProto(notification.RecipientRefs),
		MessageTemplateRef:    notification.MessageTemplateRef,
		MessageTitle:          OptionalString(notification.MessageTitle),
		MessageSummary:        notification.MessageSummary,
		BodyPreview:           OptionalString(notification.BodyPreview),
		Priority:              NotificationPriorityProto(notification.Priority),
		Status:                NotificationStatusProto(notification.Status),
		CreatedAt:             TimeProto(notification.CreatedAt),
		UpdatedAt:             TimeProto(notification.UpdatedAt),
		ExpiresAt:             OptionalTimeProto(notification.ExpiresAt),
		SourceOwner:           SourceOwnerRefProto(notification.SourceOwner),
		Ingress:               IngressRefProto(notification.Ingress),
		ContextRefs:           ExternalRefsProto(notification.ContextRefs),
		ChannelHintRefs:       ExternalRefsProto(notification.ChannelHintRefs),
		NotificationPolicyRef: OptionalString(notification.NotificationPolicyRef),
	}
}

func Subscription(subscription entity.Subscription) *interactionsv1.Subscription {
	return &interactionsv1.Subscription{
		Id:                      subscription.ID.String(),
		Scope:                   &interactionsv1.ScopeRef{Type: ScopeTypeProto(subscription.Scope.Type), Ref: subscription.Scope.Ref},
		SubscriberRef:           ActorRefProto(subscription.SubscriberRef),
		EventFilterJson:         subscription.EventFilterJSON,
		DeliveryPreferencesJson: subscription.DeliveryPreferencesJSON,
		Status:                  SubscriptionStatusProto(subscription.Status),
		Version:                 subscription.Version,
		CreatedAt:               TimeProto(subscription.CreatedAt),
		UpdatedAt:               TimeProto(subscription.UpdatedAt),
		SourceOwner:             SourceOwnerRefProto(subscription.SourceOwner),
		ChannelHintRefs:         ExternalRefsProto(subscription.ChannelHintRefs),
		SubscriptionPolicyRef:   OptionalString(subscription.SubscriptionPolicyRef),
	}
}

func ActorRef(input *interactionsv1.ActorRef) value.ActorRef {
	if input == nil {
		return value.ActorRef{}
	}
	return value.ActorRef{Kind: strings.TrimSpace(input.GetRefKind()), Ref: strings.TrimSpace(input.GetRef())}
}

func ActorRefProto(input value.ActorRef) *interactionsv1.ActorRef {
	if input.Kind == "" && input.Ref == "" {
		return nil
	}
	return &interactionsv1.ActorRef{RefKind: input.Kind, Ref: input.Ref}
}

func NotificationKind(input interactionsv1.NotificationKind) enum.NotificationKind {
	return domainEnumValue[enum.NotificationKind](input, "NOTIFICATION_KIND_")
}

func NotificationKindProto(input enum.NotificationKind) interactionsv1.NotificationKind {
	return protoEnumValue(input, interactionsv1.NotificationKind_value, "NOTIFICATION_KIND_", interactionsv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED)
}

func NotificationPriority(input interactionsv1.NotificationPriority) enum.NotificationPriority {
	return domainEnumValue[enum.NotificationPriority](input, "NOTIFICATION_PRIORITY_")
}

func NotificationPriorityProto(input enum.NotificationPriority) interactionsv1.NotificationPriority {
	return protoEnumValue(input, interactionsv1.NotificationPriority_value, "NOTIFICATION_PRIORITY_", interactionsv1.NotificationPriority_NOTIFICATION_PRIORITY_UNSPECIFIED)
}

func NotificationStatus(input interactionsv1.NotificationStatus) enum.NotificationStatus {
	return domainEnumValue[enum.NotificationStatus](input, "NOTIFICATION_STATUS_")
}

func NotificationStatusProto(input enum.NotificationStatus) interactionsv1.NotificationStatus {
	return protoEnumValue(input, interactionsv1.NotificationStatus_value, "NOTIFICATION_STATUS_", interactionsv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED)
}

func SubscriptionStatus(input interactionsv1.SubscriptionStatus) enum.SubscriptionStatus {
	return domainEnumValue[enum.SubscriptionStatus](input, "SUBSCRIPTION_STATUS_")
}

func SubscriptionStatusProto(input enum.SubscriptionStatus) interactionsv1.SubscriptionStatus {
	return protoEnumValue(input, interactionsv1.SubscriptionStatus_value, "SUBSCRIPTION_STATUS_", interactionsv1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED)
}
