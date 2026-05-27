package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func CommandMeta(input *interactionsv1.CommandMeta) (value.CommandMeta, error) {
	if input == nil {
		return value.CommandMeta{}, errs.ErrInvalidArgument
	}
	var commandID uuid.UUID
	if strings.TrimSpace(input.GetCommandId()) != "" {
		id, err := uuid.Parse(input.GetCommandId())
		if err != nil {
			return value.CommandMeta{}, errs.ErrInvalidArgument
		}
		commandID = id
	}
	var expectedVersion *int64
	if input.ExpectedVersion != nil {
		value := input.GetExpectedVersion()
		expectedVersion = &value
	}
	return value.CommandMeta{
		CommandID:       commandID,
		IdempotencyKey:  strings.TrimSpace(input.GetIdempotencyKey()),
		ExpectedVersion: expectedVersion,
		Actor:           Actor(input.GetActor()),
		Reason:          strings.TrimSpace(input.GetReason()),
		RequestID:       strings.TrimSpace(input.GetRequestId()),
	}, nil
}

func QueryMeta(input *interactionsv1.QueryMeta) value.QueryMeta {
	if input == nil {
		return value.QueryMeta{}
	}
	return value.QueryMeta{
		Actor:     Actor(input.GetActor()),
		RequestID: strings.TrimSpace(input.GetRequestId()),
	}
}

func Actor(input *interactionsv1.Actor) value.Actor {
	if input == nil {
		return value.Actor{}
	}
	return actorFromTransport(input.GetType(), input.GetId())
}

func actorFromTransport(actorType string, actorID string) value.Actor {
	return value.Actor{
		Type: strings.TrimSpace(actorType),
		ID:   strings.TrimSpace(actorID),
	}
}

func ScopeRef(input *interactionsv1.ScopeRef) (value.ScopeRef, error) {
	if input == nil {
		return value.ScopeRef{}, errs.ErrInvalidArgument
	}
	scope := value.ScopeRef{Type: ScopeType(input.GetType()), Ref: strings.TrimSpace(input.GetRef())}
	if !scope.Type.Valid() || scope.Ref == "" {
		return value.ScopeRef{}, errs.ErrInvalidArgument
	}
	return scope, nil
}

func ScopeType(input interactionsv1.InteractionScopeType) enum.ScopeType {
	switch input {
	case interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM:
		return enum.ScopeTypePlatform
	case interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION:
		return enum.ScopeTypeOrganization
	case interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT:
		return enum.ScopeTypeProject
	case interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY:
		return enum.ScopeTypeRepository
	case interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE:
		return enum.ScopeTypeService
	default:
		return ""
	}
}

func ScopeTypeProto(input enum.ScopeType) interactionsv1.InteractionScopeType {
	switch input {
	case enum.ScopeTypePlatform:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM
	case enum.ScopeTypeOrganization:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION
	case enum.ScopeTypeProject:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT
	case enum.ScopeTypeRepository:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY
	case enum.ScopeTypeService:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE
	default:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_UNSPECIFIED
	}
}

func ObjectRef(input *interactionsv1.ObjectRef) value.ObjectRef {
	if input == nil {
		return value.ObjectRef{}
	}
	size := optionalObjectSize(input)
	return value.ObjectRef{
		URI:       strings.TrimSpace(input.GetObjectUri()),
		Digest:    strings.TrimSpace(input.GetObjectDigest()),
		SizeBytes: size,
	}
}

func optionalObjectSize(input *interactionsv1.ObjectRef) *int64 {
	if input.ObjectSizeBytes == nil {
		return nil
	}
	value := input.GetObjectSizeBytes()
	return &value
}

func ObjectRefProto(input value.ObjectRef) *interactionsv1.ObjectRef {
	if input.URI == "" && input.Digest == "" && input.SizeBytes == nil {
		return nil
	}
	return objectRefMessage(input.URI, input.Digest, input.SizeBytes)
}

func objectRefMessage(uri string, digest string, size *int64) *interactionsv1.ObjectRef {
	output := &interactionsv1.ObjectRef{ObjectUri: uri}
	output.ObjectDigest = digest
	output.ObjectSizeBytes = size
	return output
}

func PageRequest(input *interactionsv1.PageRequest) value.PageRequest {
	if input == nil {
		return value.PageRequest{}
	}
	return value.PageRequest{PageSize: input.GetPageSize(), PageToken: input.GetPageToken()}
}

func PageResponse(input value.PageResult) *interactionsv1.PageResponse {
	if input.NextPageToken == "" {
		return &interactionsv1.PageResponse{}
	}
	token := input.NextPageToken
	return &interactionsv1.PageResponse{NextPageToken: &token}
}

func ParseUUID(input string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(input))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func TimeProto(input time.Time) string {
	if input.IsZero() {
		return ""
	}
	return input.UTC().Format(time.RFC3339Nano)
}

func OptionalTimeProto(input *time.Time) *string {
	if input == nil || input.IsZero() {
		return nil
	}
	value := TimeProto(*input)
	return &value
}

func OptionalUUIDProto(input *uuid.UUID) *string {
	if input == nil || *input == uuid.Nil {
		return nil
	}
	value := input.String()
	return &value
}

func OptionalString(input string) *string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	value := input
	return &value
}
