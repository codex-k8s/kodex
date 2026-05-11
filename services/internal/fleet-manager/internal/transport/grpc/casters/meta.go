package casters

import (
	"strings"

	"github.com/google/uuid"

	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// CommandMetaFromProto maps command metadata to the domain model.
func CommandMetaFromProto(meta *fleetv1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	commandID, err := optionalUUIDValue(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	result := value.CommandMeta{CommandID: commandID}
	result.IdempotencyKey = strings.TrimSpace(meta.GetIdempotencyKey())
	result.ExpectedVersion = meta.ExpectedVersion
	result.Actor = ActorFromProto(meta.GetActor())
	result.Reason = strings.TrimSpace(meta.GetReason())
	result.RequestID = strings.TrimSpace(meta.GetRequestId())
	result.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return result, nil
}

// QueryMetaFromProto maps read metadata to the domain model.
func QueryMetaFromProto(meta *fleetv1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	result := value.QueryMeta{}
	result.Actor = ActorFromProto(meta.GetActor())
	result.RequestID = strings.TrimSpace(meta.GetRequestId())
	result.RequestContext = RequestContextFromProto(meta.GetRequestContext())
	return result, nil
}

// ActorFromProto maps actor identity.
func ActorFromProto(actor *fleetv1.Actor) value.Actor {
	if actor == nil {
		return value.Actor{}
	}
	result := value.Actor{}
	result.Type = strings.TrimSpace(actor.GetType())
	result.ID = strings.TrimSpace(actor.GetId())
	return result
}

// RequestContextFromProto maps safe request metadata.
func RequestContextFromProto(context *fleetv1.RequestContext) value.RequestContext {
	if context == nil {
		return value.RequestContext{}
	}
	result := value.RequestContext{}
	result.Source = strings.TrimSpace(context.GetSource())
	result.TraceID = strings.TrimSpace(context.GetTraceId())
	result.SessionID = strings.TrimSpace(context.GetSessionId())
	result.ClientIPHash = strings.TrimSpace(context.GetClientIpHash())
	return result
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

func idWithCommandMeta(idText string, meta *fleetv1.CommandMeta) (uuid.UUID, value.CommandMeta, error) {
	return idWithMeta(idText, func() (value.CommandMeta, error) {
		return CommandMetaFromProto(meta)
	})
}

func idWithQueryMeta(idText string, meta *fleetv1.QueryMeta) (uuid.UUID, value.QueryMeta, error) {
	return idWithMeta(idText, func() (value.QueryMeta, error) {
		return QueryMetaFromProto(meta)
	})
}

func idWithMeta[T any](idText string, build func() (T, error)) (uuid.UUID, T, error) {
	id, err := requiredUUID(idText)
	if err != nil {
		var empty T
		return uuid.Nil, empty, err
	}
	meta, err := build()
	return id, meta, err
}

func pageRequestFromProto(page *fleetv1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: page.GetPageSize(), PageToken: strings.TrimSpace(page.GetPageToken())}
}

func pageResponseToProto(page value.PageResult) *fleetv1.PageResponse {
	if strings.TrimSpace(page.NextPageToken) == "" {
		return &fleetv1.PageResponse{}
	}
	return &fleetv1.PageResponse{NextPageToken: &page.NextPageToken}
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}
