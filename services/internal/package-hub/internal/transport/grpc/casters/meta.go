package casters

import (
	"strings"

	"github.com/google/uuid"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func CommandMetaFromProto(meta *packagesv1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUIDValue(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	result := value.CommandMeta{CommandID: commandID, ExpectedVersion: meta.ExpectedVersion}
	result.IdempotencyKey = strings.TrimSpace(meta.GetIdempotencyKey())
	result.Actor = ActorFromProto(meta.GetActor())
	result.Reason = strings.TrimSpace(meta.GetReason())
	result.RequestID = strings.TrimSpace(meta.GetRequestId())
	result.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return result, nil
}

func QueryMetaFromProto(meta *packagesv1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	query := value.QueryMeta{Actor: ActorFromProto(meta.GetActor())}
	query.RequestID = strings.TrimSpace(meta.GetRequestId())
	query.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return query, nil
}

func ActorFromProto(actor *packagesv1.Actor) value.Actor {
	result := value.Actor{}
	if actor == nil {
		return result
	}
	result.Type = strings.TrimSpace(actor.GetType())
	result.ID = strings.TrimSpace(actor.GetId())
	return result
}

func RequestContextFromProto(context *packagesv1.RequestContext) value.RequestContext {
	result := value.RequestContext{}
	if context == nil {
		return result
	}
	result.Source = strings.TrimSpace(context.GetSource())
	result.TraceID = strings.TrimSpace(context.GetTraceId())
	result.SessionID = strings.TrimSpace(context.GetSessionId())
	result.ClientIPHash = strings.TrimSpace(context.GetClientIpHash())
	return result
}

func requiredUUID(text string) (uuid.UUID, error) {
	id, ok, err := parseUUID(text)
	if err != nil || !ok || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func parseUUID(text string) (uuid.UUID, bool, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return uuid.Nil, false, nil
	}
	id, err := uuid.Parse(trimmed)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false, errs.ErrInvalidArgument
	}
	return id, true, nil
}

func optionalUUIDPtr(text string) (*uuid.UUID, error) {
	id, ok, err := parseUUID(text)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func optionalUUIDValue(text string) (uuid.UUID, error) {
	id, ok, err := parseUUID(text)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, nil
	}
	return id, nil
}

func optionalStringPtr(text string) *string {
	value := strings.TrimSpace(text)
	if value == "" {
		return nil
	}
	return &value
}

func pageRequestFromProto(page *packagesv1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: page.GetPageSize(), PageToken: strings.TrimSpace(page.GetPageToken())}
}

func pageResponseToProto(page value.PageResult) *packagesv1.PageResponse {
	return &packagesv1.PageResponse{NextPageToken: optionalStringPtr(page.NextPageToken)}
}
