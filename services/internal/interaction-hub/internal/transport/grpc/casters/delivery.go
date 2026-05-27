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
	return commandPayloadInput(input, (*interactionsv1.RecordDeliveryResultRequest).GetMeta, (*interactionsv1.RecordDeliveryResultRequest).GetResult, ChannelDeliveryResult, deliveryResultInput)
}

func RecordChannelCallbackInput(input *interactionsv1.RecordChannelCallbackRequest) (interactionservice.RecordChannelCallbackInput, error) {
	return commandPayloadInput(input, (*interactionsv1.RecordChannelCallbackRequest).GetMeta, (*interactionsv1.RecordChannelCallbackRequest).GetCallback, ChannelCallbackEnvelope, channelCallbackInput)
}

func deliveryResultInput(meta value.CommandMeta, result value.ChannelDeliveryResult) interactionservice.RecordDeliveryResultInput {
	return interactionservice.RecordDeliveryResultInput{Meta: meta, Result: result}
}

func channelCallbackInput(meta value.CommandMeta, callback value.ChannelCallbackEnvelope) interactionservice.RecordChannelCallbackInput {
	return interactionservice.RecordChannelCallbackInput{Meta: meta, Callback: callback}
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
	if result.LatestCallback != nil {
		response.LatestCallback = ChannelCallback(*result.LatestCallback)
	}
	return response
}

func ChannelCallbackResponse(result interactionservice.ChannelCallbackResult) *interactionsv1.ChannelCallbackResponse {
	response := &interactionsv1.ChannelCallbackResponse{Callback: ChannelCallback(result.Callback)}
	if result.Response != nil {
		response.Response = InteractionResponse(*result.Response)
	}
	return response
}

func DeliveryAttempt(attempt entity.DeliveryAttempt) *interactionsv1.DeliveryAttempt {
	return &interactionsv1.DeliveryAttempt{
		Id:                     attempt.ID.String(),
		Target:                 DeliveryTargetProto(attempt.Target),
		RouteId:                attempt.RouteID.String(),
		DeliveryId:             attempt.DeliveryID,
		DeliveryKind:           DeliveryKindProto(attempt.DeliveryKind),
		Status:                 DeliveryAttemptStatusProto(attempt.Status),
		ChannelMessageRef:      OptionalString(attempt.ChannelMessageRef),
		AttemptNumber:          attempt.AttemptNumber,
		NextRetryAt:            OptionalTimeProto(attempt.NextRetryAt),
		ErrorCode:              OptionalString(attempt.ErrorCode),
		ErrorClass:             DeliveryErrorClassProto(attempt.ErrorClass),
		PayloadDigest:          attempt.PayloadDigest,
		CreatedAt:              TimeProto(attempt.CreatedAt),
		UpdatedAt:              TimeProto(attempt.UpdatedAt),
		SentAt:                 OptionalTimeProto(attempt.SentAt),
		ChannelCapabilityRef:   OptionalString(attempt.ChannelCapabilityRef),
		PackageInstallationRef: OptionalString(attempt.PackageInstallationRef),
		PackageVersionRef:      OptionalString(attempt.PackageVersionRef),
		DeliveryCommandRef:     OptionalString(attempt.DeliveryCommandRef),
		CallbackRef:            OptionalString(attempt.CallbackRef),
		CallbackRouteRef:       OptionalString(attempt.CallbackRouteRef),
		RuntimeRef:             OptionalString(attempt.RuntimeRef),
		RuntimeJobRef:          OptionalString(attempt.RuntimeJobRef),
		RoutingPolicyRef:       OptionalString(attempt.RoutingPolicyRef),
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
		ContractVersion:    strings.TrimSpace(input.GetContractVersion()),
		DeliveryID:         strings.TrimSpace(input.GetDeliveryId()),
		ResultStatus:       ChannelDeliveryResultStatus(input.GetResultStatus()),
		ChannelMessageRef:  strings.TrimSpace(input.GetChannelMessageRef()),
		ErrorCode:          strings.TrimSpace(input.GetErrorCode()),
		ErrorClass:         DeliveryErrorClass(input.GetErrorClass()),
		RetryAfter:         retryAfter,
		OccurredAt:         occurredAt,
		DeliveryCommandRef: strings.TrimSpace(input.GetDeliveryCommandRef()),
		RuntimeRef:         strings.TrimSpace(input.GetRuntimeRef()),
		RuntimeJobRef:      strings.TrimSpace(input.GetRuntimeJobRef()),
	}, nil
}

func ChannelCallbackEnvelope(input *interactionsv1.ChannelCallbackEnvelope) (value.ChannelCallbackEnvelope, error) {
	if input == nil {
		return value.ChannelCallbackEnvelope{}, errs.ErrInvalidArgument
	}
	receivedAt, err := parseRequiredTime(input.GetReceivedAt())
	if err != nil {
		return value.ChannelCallbackEnvelope{}, err
	}
	return value.ChannelCallbackEnvelope{
		ContractVersion: strings.TrimSpace(input.GetContractVersion()),
		CallbackID:      strings.TrimSpace(input.GetCallbackId()),
		DeliveryID:      strings.TrimSpace(input.GetDeliveryId()),
		RequestRef:      strings.TrimSpace(input.GetRequestRef()),
		ActorRef:        strings.TrimSpace(input.GetActorRef()),
		Action:          strings.TrimSpace(input.GetAction()),
		AnswerSummary:   strings.TrimSpace(input.GetAnswerSummary()),
		AnswerObject:    ObjectRef(input.GetAnswerObject()),
		SignatureStatus: CallbackSignatureStatus(input.GetSignatureStatus()),
		GatewayRef:      strings.TrimSpace(input.GetGatewayRef()),
		ReceivedAt:      receivedAt,
		CorrelationID:   strings.TrimSpace(input.GetCorrelationId()),
	}, nil
}

func ChannelCallback(callback entity.ChannelCallback) *interactionsv1.ChannelCallback {
	return &interactionsv1.ChannelCallback{
		Id:                callback.ID.String(),
		CallbackId:        callback.CallbackID,
		DeliveryId:        OptionalString(callback.DeliveryID),
		DeliveryAttemptId: OptionalUUIDProto(callback.DeliveryAttemptID),
		RequestId:         OptionalUUIDProto(callback.RequestID),
		SourceRouteId:     OptionalUUIDProto(callback.SourceRouteID),
		ActorRef:          OptionalString(callback.ActorRef),
		Action:            OptionalString(callback.Action),
		CallbackSummary:   OptionalString(callback.CallbackSummary),
		CallbackObject:    ObjectRefProto(callback.CallbackObject),
		SignatureStatus:   CallbackSignatureStatusProto(callback.SignatureStatus),
		ProcessingStatus:  CallbackProcessingStatusProto(callback.ProcessingStatus),
		ErrorCode:         OptionalString(callback.ErrorCode),
		ReceivedAt:        TimeProto(callback.ReceivedAt),
		CreatedAt:         TimeProto(callback.CreatedAt),
		CallbackRouteRef:  OptionalString(callback.CallbackRouteRef),
		GatewayRef:        OptionalString(callback.GatewayRef),
		CorrelationId:     OptionalString(callback.CorrelationID),
	}
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

func commandPayloadInput[Request any, Payload any, DomainPayload any, Output any](
	input *Request,
	metaInput func(*Request) *interactionsv1.CommandMeta,
	payloadInput func(*Request) *Payload,
	decodePayload func(*Payload) (DomainPayload, error),
	build func(value.CommandMeta, DomainPayload) Output,
) (Output, error) {
	decode := func(request *Request) (DomainPayload, error) {
		return decodePayload(payloadInput(request))
	}
	return decodeCommandEnvelope(input, metaInput, decode, build)
}

func decodeCommandEnvelope[Request any, DomainPayload any, Output any](
	input *Request,
	metaInput func(*Request) *interactionsv1.CommandMeta,
	decodePayload func(*Request) (DomainPayload, error),
	build func(value.CommandMeta, DomainPayload) Output,
) (Output, error) {
	var zero Output
	if input == nil {
		return zero, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(metaInput(input))
	if err != nil {
		return zero, err
	}
	payload, err := decodePayload(input)
	if err != nil {
		return zero, err
	}
	return build(meta, payload), nil
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

func CallbackSignatureStatus(input interactionsv1.CallbackSignatureStatus) enum.CallbackSignatureStatus {
	return domainEnumValue[enum.CallbackSignatureStatus](input, "CALLBACK_SIGNATURE_STATUS_")
}

func CallbackSignatureStatusProto(input enum.CallbackSignatureStatus) interactionsv1.CallbackSignatureStatus {
	return protoEnumValue(input, interactionsv1.CallbackSignatureStatus_value, "CALLBACK_SIGNATURE_STATUS_", interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_UNSPECIFIED)
}

func CallbackProcessingStatusProto(input enum.CallbackProcessingStatus) interactionsv1.CallbackProcessingStatus {
	return protoEnumValue(input, interactionsv1.CallbackProcessingStatus_value, "CALLBACK_PROCESSING_STATUS_", interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_UNSPECIFIED)
}
