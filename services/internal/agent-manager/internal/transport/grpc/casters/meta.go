package casters

import (
	"strings"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

// IDQueryInput carries a required aggregate id and safe query metadata.
type IDQueryInput struct {
	ID   uuid.UUID
	Meta value.QueryMeta
}

func CommandMetaFromProto(meta *agentsv1.CommandMeta) (value.CommandMeta, error) {
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
	}, nil
}

func QueryMetaFromProto(meta *agentsv1.QueryMeta) (value.QueryMeta, error) {
	if meta == nil {
		return value.QueryMeta{}, errs.ErrInvalidArgument
	}
	return value.QueryMeta{Actor: ActorFromProto(meta.GetActor())}, nil
}

func ActorFromProto(actor *agentsv1.Actor) value.Actor {
	result := value.Actor{}
	if actor != nil {
		result.Type = strings.TrimSpace(actor.GetType())
		result.ID = strings.TrimSpace(actor.GetId())
	}
	return result
}

func LocalizedTextFromProto(text []*agentsv1.LocalizedText) []value.LocalizedText {
	result := make([]value.LocalizedText, 0, len(text))
	for _, item := range text {
		if item == nil {
			continue
		}
		result = append(result, value.LocalizedText{
			Locale: strings.TrimSpace(item.GetLocale()),
			Text:   strings.TrimSpace(item.GetText()),
		})
	}
	return result
}

func ScopeFromProto(scope *agentsv1.ScopeRef) (value.ScopeRef, error) {
	if scope == nil {
		return value.ScopeRef{}, errs.ErrInvalidArgument
	}
	scopeType, err := ScopeTypeFromProto(scope.GetType())
	if err != nil {
		return value.ScopeRef{}, err
	}
	return value.ScopeRef{Type: string(scopeType), Ref: strings.TrimSpace(scope.GetRef())}, nil
}

func ObjectRefFromProto(object *agentsv1.ObjectRef) value.ObjectRef {
	if object == nil {
		return value.ObjectRef{}
	}
	var size *int64
	if object.ObjectSizeBytes != nil {
		copied := object.GetObjectSizeBytes()
		size = &copied
	}
	return value.ObjectRef{
		ObjectURI:       strings.TrimSpace(object.GetObjectUri()),
		ObjectDigest:    strings.TrimSpace(object.GetObjectDigest()),
		ObjectSizeBytes: size,
	}
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
	parsed, parseErr := uuid.Parse(trimmed)
	switch {
	case parseErr != nil:
		return uuid.Nil, false, errs.ErrInvalidArgument
	case parsed == uuid.Nil:
		return uuid.Nil, false, errs.ErrInvalidArgument
	default:
		return parsed, true, nil
	}
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

func optionalPresentString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func pageRequestFromProto(page *agentsv1.PageRequest) value.PageRequest {
	if page == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{
		PageSize:  page.GetPageSize(),
		PageToken: strings.TrimSpace(page.GetPageToken()),
	}
}

func pageResponseToProto(page value.PageResult) *agentsv1.PageResponse {
	return &agentsv1.PageResponse{NextPageToken: optionalStringPtr(page.NextPageToken)}
}
