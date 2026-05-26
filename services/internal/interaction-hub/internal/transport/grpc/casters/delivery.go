package casters

import (
	"strings"
	"time"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func PlanDeliveryInput(input *interactionsv1.PlanDeliveryRequest) (interactionservice.PlanDeliveryInput, error) {
	if input == nil {
		return interactionservice.PlanDeliveryInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.PlanDeliveryInput{}, err
	}
	routeID, err := OptionalUUID(input.GetRouteId())
	if err != nil {
		return interactionservice.PlanDeliveryInput{}, err
	}
	target, err := DeliveryTarget(input.GetTarget())
	if err != nil {
		return interactionservice.PlanDeliveryInput{}, err
	}
	return interactionservice.PlanDeliveryInput{
		Meta:          meta,
		Target:        target,
		RouteID:       routeID,
		CorrelationID: strings.TrimSpace(input.GetCorrelationId()),
	}, nil
}

func RecordDeliveryResultInput(input *interactionsv1.RecordDeliveryResultRequest) (interactionservice.RecordDeliveryResultInput, error) {
	if input == nil {
		return interactionservice.RecordDeliveryResultInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.RecordDeliveryResultInput{}, err
	}
	result, err := ChannelDeliveryResult(input.GetResult())
	if err != nil {
		return interactionservice.RecordDeliveryResultInput{}, err
	}
	return interactionservice.RecordDeliveryResultInput{Meta: meta, Result: result}, nil
}

func GetDeliveryStatusInput(input *interactionsv1.GetDeliveryStatusRequest) (interactionservice.GetDeliveryStatusInput, error) {
	if input == nil {
		return interactionservice.GetDeliveryStatusInput{}, errs.ErrInvalidArgument
	}
	target, err := DeliveryTarget(input.GetTarget())
	if err != nil && strings.TrimSpace(input.GetDeliveryId()) == "" {
		return interactionservice.GetDeliveryStatusInput{}, err
	}
	return interactionservice.GetDeliveryStatusInput{
		Meta:       QueryMeta(input.GetMeta()),
		Target:     target,
		DeliveryID: strings.TrimSpace(input.GetDeliveryId()),
	}, nil
}

func DeliveryAttemptResponse(attempt entity.DeliveryAttempt) *interactionsv1.DeliveryAttemptResponse {
	return &interactionsv1.DeliveryAttemptResponse{DeliveryAttempt: DeliveryAttempt(attempt)}
}

func DeliveryStatusResponse(result interactionservice.DeliveryStatusResult) *interactionsv1.DeliveryStatusResponse {
	response := &interactionsv1.DeliveryStatusResponse{DeliveryAttempts: make([]*interactionsv1.DeliveryAttempt, 0, len(result.DeliveryAttempts))}
	if result.Request != nil {
		response.Request = InteractionRequest(*result.Request)
	}
	if result.Notification != nil {
		response.Notification = Notification(*result.Notification)
	}
	for _, attempt := range result.DeliveryAttempts {
		response.DeliveryAttempts = append(response.DeliveryAttempts, DeliveryAttempt(attempt))
	}
	return response
}

func DeliveryAttempt(attempt entity.DeliveryAttempt) *interactionsv1.DeliveryAttempt {
	return &interactionsv1.DeliveryAttempt{
		Id:                attempt.ID.String(),
		Target:            DeliveryTargetProto(attempt.Target),
		RouteId:           attempt.RouteID.String(),
		DeliveryId:        attempt.DeliveryID,
		DeliveryKind:      DeliveryKindProto(attempt.DeliveryKind),
		Status:            DeliveryAttemptStatusProto(attempt.Status),
		ChannelMessageRef: OptionalString(attempt.ChannelMessageRef),
		AttemptNumber:     attempt.AttemptNumber,
		NextRetryAt:       OptionalTimeProto(attempt.NextRetryAt),
		ErrorCode:         OptionalString(attempt.ErrorCode),
		ErrorClass:        DeliveryErrorClassProto(attempt.ErrorClass),
		PayloadDigest:     attempt.PayloadDigest,
		CreatedAt:         TimeProto(attempt.CreatedAt),
		UpdatedAt:         TimeProto(attempt.UpdatedAt),
		SentAt:            OptionalTimeProto(attempt.SentAt),
	}
}

func DeliveryTarget(input *interactionsv1.DeliveryTarget) (value.DeliveryTarget, error) {
	if input == nil {
		return value.DeliveryTarget{}, errs.ErrInvalidArgument
	}
	switch target := input.GetTarget().(type) {
	case *interactionsv1.DeliveryTarget_RequestId:
		id, err := ParseUUID(target.RequestId)
		if err != nil {
			return value.DeliveryTarget{}, err
		}
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindRequest, ID: id}, nil
	case *interactionsv1.DeliveryTarget_NotificationId:
		id, err := ParseUUID(target.NotificationId)
		if err != nil {
			return value.DeliveryTarget{}, err
		}
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindNotification, ID: id}, nil
	default:
		return value.DeliveryTarget{}, errs.ErrInvalidArgument
	}
}

func DeliveryTargetProto(input value.DeliveryTarget) *interactionsv1.DeliveryTarget {
	switch input.Kind {
	case value.DeliveryTargetKindRequest:
		return &interactionsv1.DeliveryTarget{Target: &interactionsv1.DeliveryTarget_RequestId{RequestId: input.ID.String()}}
	case value.DeliveryTargetKindNotification:
		return &interactionsv1.DeliveryTarget{Target: &interactionsv1.DeliveryTarget_NotificationId{NotificationId: input.ID.String()}}
	default:
		return nil
	}
}

func ChannelDeliveryResult(input *interactionsv1.ChannelDeliveryResult) (value.ChannelDeliveryResult, error) {
	if input == nil {
		return value.ChannelDeliveryResult{}, errs.ErrInvalidArgument
	}
	occurredAt, err := parseRequiredTime(input.GetOccurredAt())
	if err != nil {
		return value.ChannelDeliveryResult{}, err
	}
	retryAfter, err := retryAfterTime(input.GetRetryAfter(), occurredAt)
	if err != nil {
		return value.ChannelDeliveryResult{}, err
	}
	return value.ChannelDeliveryResult{
		ContractVersion:   strings.TrimSpace(input.GetContractVersion()),
		DeliveryID:        strings.TrimSpace(input.GetDeliveryId()),
		ResultStatus:      ChannelDeliveryResultStatus(input.GetResultStatus()),
		ChannelMessageRef: strings.TrimSpace(input.GetChannelMessageRef()),
		ErrorCode:         strings.TrimSpace(input.GetErrorCode()),
		ErrorClass:        DeliveryErrorClass(input.GetErrorClass()),
		RetryAfter:        retryAfter,
		OccurredAt:        occurredAt,
	}, nil
}

func parseRequiredTime(input string) (time.Time, error) {
	if strings.TrimSpace(input) == "" {
		return time.Time{}, errs.ErrInvalidArgument
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input))
	if err != nil {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return parsed.UTC(), nil
}

func retryAfterTime(input string, occurredAt time.Time) (*time.Time, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input)); err == nil {
		value := parsed.UTC()
		return &value, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(input))
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	value := occurredAt.Add(duration).UTC()
	return &value, nil
}

func DeliveryKind(input interactionsv1.DeliveryKind) enum.DeliveryKind {
	return domainEnumValue[enum.DeliveryKind](input, "DELIVERY_KIND_")
}

func DeliveryKindProto(input enum.DeliveryKind) interactionsv1.DeliveryKind {
	return protoEnumValue(input, interactionsv1.DeliveryKind_value, "DELIVERY_KIND_", interactionsv1.DeliveryKind_DELIVERY_KIND_UNSPECIFIED)
}

func DeliveryAttemptStatusProto(input enum.DeliveryAttemptStatus) interactionsv1.DeliveryAttemptStatus {
	return protoEnumValue(input, interactionsv1.DeliveryAttemptStatus_value, "DELIVERY_ATTEMPT_STATUS_", interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_UNSPECIFIED)
}

func DeliveryErrorClass(input interactionsv1.DeliveryErrorClass) enum.DeliveryErrorClass {
	return domainEnumValue[enum.DeliveryErrorClass](input, "DELIVERY_ERROR_CLASS_")
}

func DeliveryErrorClassProto(input enum.DeliveryErrorClass) interactionsv1.DeliveryErrorClass {
	return protoEnumValue(input, interactionsv1.DeliveryErrorClass_value, "DELIVERY_ERROR_CLASS_", interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_UNSPECIFIED)
}

func ChannelDeliveryResultStatus(input interactionsv1.ChannelDeliveryResultStatus) enum.ChannelDeliveryResultStatus {
	return domainEnumValue[enum.ChannelDeliveryResultStatus](input, "CHANNEL_DELIVERY_RESULT_STATUS_")
}
