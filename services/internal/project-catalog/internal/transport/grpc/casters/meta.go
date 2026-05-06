package casters

import (
	"strings"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// CommandMetaFromProto maps command metadata to the domain model.
func CommandMetaFromProto(meta *projectsv1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUIDValue(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	expectedVersion := meta.ExpectedVersion
	return value.CommandMeta{
		CommandID:       commandID,
		IdempotencyKey:  strings.TrimSpace(meta.GetIdempotencyKey()),
		ExpectedVersion: expectedVersion,
		Actor:           ActorFromProto(meta.GetActor()),
		Reason:          strings.TrimSpace(meta.GetReason()),
		RequestID:       strings.TrimSpace(meta.GetRequestId()),
		RequestContext:  RequestContextFromProto(meta.GetRequestContext()),
	}, nil
}

// QueryMetaFromProto maps read metadata to the domain model.
func QueryMetaFromProto(meta *projectsv1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	return value.QueryMeta{
		Actor:          ActorFromProto(meta.GetActor()),
		RequestID:      strings.TrimSpace(meta.GetRequestId()),
		RequestContext: RequestContextFromProto(meta.GetRequestContext()),
	}, nil
}

// ActorFromProto maps actor identity.
func ActorFromProto(actor *projectsv1.Actor) value.Actor {
	if actor == nil {
		return value.Actor{}
	}
	return value.Actor{Type: strings.TrimSpace(actor.GetType()), ID: strings.TrimSpace(actor.GetId())}
}

// RequestContextFromProto maps safe request metadata.
func RequestContextFromProto(context *projectsv1.RequestContext) value.RequestContext {
	if context == nil {
		return value.RequestContext{}
	}
	return value.RequestContext{
		Source:       strings.TrimSpace(context.GetSource()),
		TraceID:      strings.TrimSpace(context.GetTraceId()),
		SessionID:    strings.TrimSpace(context.GetSessionId()),
		ClientIPHash: strings.TrimSpace(context.GetClientIpHash()),
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

func stringPtr(value string) *string {
	return &value
}

func optionalStringPtr(text string) *string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	value := strings.TrimSpace(text)
	return &value
}

func pageRequestFromProto(page *projectsv1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: page.GetPageSize(), PageToken: strings.TrimSpace(page.GetPageToken())}
}

func pageResponseToProto(page value.PageResult) *projectsv1.PageResponse {
	return &projectsv1.PageResponse{NextPageToken: optionalStringPtr(page.NextPageToken)}
}
