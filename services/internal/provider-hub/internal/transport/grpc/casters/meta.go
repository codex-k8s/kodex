package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// CommandMetaFromProto maps command metadata to the domain model.
func CommandMetaFromProto(meta *providersv1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUIDValue(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	return value.CommandMeta{
		CommandID:       commandID,
		IdempotencyKey:  strings.TrimSpace(meta.GetIdempotencyKey()),
		ExpectedVersion: meta.ExpectedVersion,
		Actor:           ActorFromProto(meta.GetActor()),
		Reason:          strings.TrimSpace(meta.GetReason()),
		RequestID:       strings.TrimSpace(meta.GetRequestId()),
		RequestContext:  RequestContextFromProto(meta.GetRequestContext()),
	}, nil
}

// QueryMetaFromProto maps read metadata to the domain model.
func QueryMetaFromProto(meta *providersv1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	requestContext := RequestContextFromProto(meta.GetRequestContext())
	return value.QueryMeta{
		Actor:          ActorFromProto(meta.GetActor()),
		RequestID:      cleanText(meta.GetRequestId()),
		RequestContext: requestContext,
	}, nil
}

// ActorFromProto maps actor identity.
func ActorFromProto(actor *providersv1.Actor) value.Actor {
	if actor == nil {
		return value.Actor{}
	}
	actorType, actorID := grpcserver.ActorParts(actor)
	return value.Actor{Type: actorType, ID: actorID}
}

// RequestContextFromProto maps safe request metadata.
func RequestContextFromProto(context *providersv1.RequestContext) value.RequestContext {
	if context == nil {
		return value.RequestContext{}
	}
	source, traceID, sessionID, clientIPHash := grpcserver.RequestContextParts(context)
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

func requiredTime(text string) (time.Time, error) {
	timestamp := cleanText(text)
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	switch {
	case err != nil:
		return time.Time{}, errs.ErrInvalidArgument
	case parsed.IsZero():
		return time.Time{}, errs.ErrInvalidArgument
	default:
		return parsed.UTC(), nil
	}
}

func optionalTimePtr(text string) (*time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := requiredTime(trimmed)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func pageRequestFromProto(page *providersv1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: page.GetPageSize(), PageToken: strings.TrimSpace(page.GetPageToken())}
}

func pageResponseToProto(page value.PageResult) *providersv1.PageResponse {
	return &providersv1.PageResponse{NextPageToken: optionalStringPtr(page.NextPageToken)}
}

func optionalStringPtr(text string) *string {
	value := cleanText(text)
	if value == "" {
		return nil
	}
	return &value
}

func timePtrString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	return value
}

func optionalPositiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func optionalWorkItemKindPtr(kind providersv1.WorkItemKind) *providersv1.WorkItemKind {
	if kind == providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED {
		return nil
	}
	return &kind
}

func cleanText(text string) string {
	return strings.TrimSpace(text)
}
