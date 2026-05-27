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
	return collectProtoValues(text, localizedTextValue)
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
	return value.ObjectRef{
		ObjectURI:       strings.TrimSpace(object.GetObjectUri()),
		ObjectDigest:    strings.TrimSpace(object.GetObjectDigest()),
		ObjectSizeBytes: objectSizeBytesPtr(object),
	}
}

func RuntimeContextFromProto(context *agentsv1.RuntimeContextRef) value.RuntimeContextRef {
	if context == nil {
		return value.RuntimeContextRef{}
	}
	refs := trimmedProtoStringGetters(
		context.GetSlotRef,
		context.GetJobRef,
		context.GetWorkspaceRef,
		context.GetContextRef,
	)
	return value.RuntimeContextRef{SlotRef: refs[0], JobRef: refs[1], WorkspaceRef: refs[2], ContextRef: refs[3]}
}

func ProviderTargetFromProto(target *agentsv1.ProviderTargetRef) value.ProviderTargetRef {
	if target == nil {
		return value.ProviderTargetRef{}
	}
	return providerTargetValue(target)
}

func providerTargetValue(target *agentsv1.ProviderTargetRef) value.ProviderTargetRef {
	refs := trimmedProtoStrings(target.GetWorkItemRef(), target.GetPullRequestRef(), target.GetCommentRef(), target.GetReviewSignalRef())
	return value.ProviderTargetRef{
		WorkItemRef:     refs[0],
		PullRequestRef:  refs[1],
		CommentRef:      refs[2],
		ReviewSignalRef: refs[3],
	}
}

func GuidanceSelectionHintsFromProto(hints []*agentsv1.GuidanceSelectionHint) []value.GuidanceSelectionHint {
	return collectProtoValues(hints, guidanceSelectionHintValue)
}

func localizedTextValue(item *agentsv1.LocalizedText) (value.LocalizedText, bool) {
	if item == nil {
		return value.LocalizedText{}, false
	}
	text := value.LocalizedText{}
	text.Locale = strings.TrimSpace(item.GetLocale())
	text.Text = strings.TrimSpace(item.GetText())
	return text, true
}

func guidanceSelectionHintValue(hint *agentsv1.GuidanceSelectionHint) (value.GuidanceSelectionHint, bool) {
	if hint == nil {
		return value.GuidanceSelectionHint{}, false
	}
	return value.GuidanceSelectionHint{
		PackageInstallationRef: strings.TrimSpace(hint.GetPackageInstallationRef()),
		PackageSlug:            strings.TrimSpace(hint.GetPackageSlug()),
	}, true
}

func collectProtoValues[Proto any, Domain any](items []Proto, cast func(Proto) (Domain, bool)) []Domain {
	result := make([]Domain, 0, len(items))
	for _, item := range items {
		value, ok := cast(item)
		if ok {
			result = append(result, value)
		}
	}
	return result
}

func objectSizeBytesPtr(object *agentsv1.ObjectRef) *int64 {
	if object.ObjectSizeBytes == nil {
		return nil
	}
	copied := object.GetObjectSizeBytes()
	return &copied
}

func trimmedProtoStrings(values ...string) []string {
	result := make([]string, len(values))
	for idx, value := range values {
		result[idx] = strings.TrimSpace(value)
	}
	return result
}

func trimmedProtoStringGetters(getters ...func() string) []string {
	result := make([]string, len(getters))
	for idx, getter := range getters {
		result[idx] = strings.TrimSpace(getter())
	}
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
