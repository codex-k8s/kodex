package casters

import (
	"strings"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// CommandMetaFromProto maps command metadata to the domain model.
func CommandMetaFromProto(meta *runtimev1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUIDValue(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	result := value.CommandMeta{CommandID: commandID}
	result.IdempotencyKey = cleanText(meta.GetIdempotencyKey())
	result.ExpectedVersion = meta.ExpectedVersion
	result.Actor = ActorFromProto(meta.GetActor())
	result.Reason = cleanText(meta.GetReason())
	result.RequestID = cleanText(meta.GetRequestId())
	result.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return result, nil
}

// QueryMetaFromProto maps read metadata to the domain model.
func QueryMetaFromProto(meta *runtimev1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	result := value.QueryMeta{}
	result.Actor = ActorFromProto(meta.GetActor())
	result.RequestID = cleanText(meta.GetRequestId())
	result.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return result, nil
}

// ActorFromProto maps actor identity.
func ActorFromProto(actor *runtimev1.Actor) value.Actor {
	if actor == nil {
		return value.Actor{}
	}
	actorType, actorID := grpcserver.ActorParts(actor)
	return value.Actor{Type: actorType, ID: actorID}
}

// RequestContextFromProto maps safe request metadata.
func RequestContextFromProto(context *runtimev1.RequestContext) value.RequestContext {
	if context == nil {
		return value.RequestContext{}
	}
	return requestContextFromParts(grpcserver.RequestContextParts(context))
}

func requestContextFromParts(source string, traceID string, sessionID string, clientIPHash string) value.RequestContext {
	return value.RequestContext{
		Source:       source,
		TraceID:      traceID,
		SessionID:    sessionID,
		ClientIPHash: clientIPHash,
	}
}

func requiredUUID(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(text))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func optionalUUIDPtr(text string) (*uuid.UUID, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	id, err := requiredUUID(trimmed)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func optionalUUIDValue(text string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return uuid.Nil, nil
	}
	return requiredUUID(trimmed)
}

func optionalStringPtr(text string) *string {
	value := cleanText(text)
	if value == "" {
		return nil
	}
	return &value
}

func pageRequestFromProto(page *runtimev1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: page.GetPageSize(), PageToken: cleanText(page.GetPageToken())}
}

func pageResponseToProto(page value.PageResult) *runtimev1.PageResponse {
	return &runtimev1.PageResponse{NextPageToken: optionalStringPtr(page.NextPageToken)}
}

func cleanText(text string) string {
	return strings.TrimSpace(text)
}
