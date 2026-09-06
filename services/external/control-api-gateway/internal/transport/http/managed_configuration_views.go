package httptransport

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

var errManagedConfigurationShape = errors.New("managed configuration response is invalid")

type managedConfigurationResponse interface {
	GetConfiguration() *controlplanev1.ManagedConfigurationSet
	GetRevision() *controlplanev1.ManagedConfigurationRevision
}

func requireManagedDraftMutation(w http.ResponseWriter, key, etag string, body generated.ManagedConfigurationDraftInput, allowPromptScope ...bool) (*controlplanev1.MutationContext, bool) {
	if body.PromptScope != nil && (len(allowPromptScope) != 1 || !allowPromptScope[0]) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 160 || len(body.Content) == 0 || len(body.Content) > 256<<10 ||
		body.ConfigurationRef != nil && (*body.ConfigurationRef == "" || etag == "") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	switch body.ContentFormat {
	case "TEXT", "JSON", "YAML", "TOML":
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

func managedConsumerInput(w http.ResponseWriter, input generated.ManagedConfigurationRebindInput) ([]*controlplanev1.ManagedConfigurationConsumer, bool) {
	if !validManagedDigest(input.ImpactDigest) || len(input.Consumers) == 0 || len(input.Consumers) > 128 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	result := make([]*controlplanev1.ManagedConfigurationConsumer, 0, len(input.Consumers))
	seen := make(map[string]struct{}, len(input.Consumers))
	for _, value := range input.Consumers {
		item := &controlplanev1.ManagedConfigurationConsumer{Kind: string(value.Kind), Ref: value.Ref, RevisionRef: value.RevisionRef, Version: value.Version}
		key := item.Kind + "\x00" + item.Ref
		_, duplicate := seen[key]
		if _, err := managedConsumerView(item); err != nil || duplicate {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result, true
}

func writeManagedResult(w http.ResponseWriter, statusCode int, value managedConfigurationResponse) {
	configuration, err := managedConfigurationView(value.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	revision, err := managedRevisionForConfiguration(value.GetRevision(), value.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", configuration.Version))
	writeJSON(w, statusCode, generated.ManagedConfigurationResult{Configuration: configuration, Revision: revision})
}

// Содержимое конфигурации не проходит общую нормализацию строк и enum.
func managedConfigurationView(value *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfiguration, error) {
	result, err := managedConfigurationMetadataView(value)
	if err != nil {
		return generated.ManagedConfiguration{}, err
	}
	if value.GetCurrentRevision() != nil {
		revision, err := managedRevisionForConfiguration(value.GetCurrentRevision(), value)
		if err != nil {
			return generated.ManagedConfiguration{}, err
		}
		result.CurrentRevision = &revision
	}
	return result, nil
}

func managedConfigurationMetadataView(value *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfiguration, error) {
	if value == nil || value.GetRef() == "" || !validManagedVersion(value.GetVersion()) || value.GetUpdatedAt() == nil || value.GetUpdatedAt().CheckValid() != nil {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	switch value.GetKind() {
	case controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_EMAIL_MAILBOX:
	default:
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	if value.GetManagedBy() != controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI && value.GetManagedBy() != controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_GIT {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	if (value.GetKind() == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE) != (value.SourceEditable != nil) {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	result := generated.ManagedConfiguration{
		SourceEditable: value.SourceEditable,
		Ref:            value.GetRef(), Version: value.GetVersion(), Name: value.GetName(), ProjectRef: optionalManagedString(value.GetProjectRef()),
		Kind:      generated.ManagedConfigurationKind(strings.TrimPrefix(value.GetKind().String(), "MANAGED_CONFIGURATION_KIND_")),
		ManagedBy: generated.ManagedConfigurationManagedBy(strings.TrimPrefix(value.GetManagedBy().String(), "MANAGED_CONFIGURATION_OWNER_")),
		Source:    value.GetSource(), SourceRevision: value.GetSourceRevision(), UpdatedAt: value.GetUpdatedAt().AsTime(),
	}
	var err error
	result.GitSource, err = managedGitSourceView(value)
	if err != nil {
		return generated.ManagedConfiguration{}, err
	}
	return result, nil
}

func managedRevisionForConfiguration(value *controlplanev1.ManagedConfigurationRevision, configuration *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfigurationRevision, error) {
	if value == nil || configuration == nil || (configuration.GetKind() == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE) != (value.SourceAvailable != nil) {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	return managedRevisionView(value)
}

func managedRevisionView(value *controlplanev1.ManagedConfigurationRevision) (generated.ManagedConfigurationRevision, error) {
	if value == nil || value.GetRef() == "" || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) || value.GetCreatedAt() == nil || value.GetCreatedAt().CheckValid() != nil {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	if value.SourceAvailable != nil && !value.GetSourceAvailable() && (value.GetContent() != "" || len(value.GetValidationDiagnostics()) != 0) {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	switch value.GetState() {
	case controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_VALID,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_INVALID,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_SUPERSEDED,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED:
	default:
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	switch value.GetContentFormat() {
	case "TEXT", "JSON", "YAML", "TOML":
	default:
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	result := generated.ManagedConfigurationRevision{
		SourceAvailable: value.SourceAvailable,
		Ref:             value.GetRef(), Revision: value.GetRevision(), Digest: value.GetDigest(), Content: value.GetContent(),
		ContentFormat: generated.ManagedConfigurationRevisionContentFormat(value.GetContentFormat()),
		State:         generated.ManagedConfigurationRevisionState(strings.TrimPrefix(value.GetState().String(), "MANAGED_CONFIGURATION_STATE_")),
		CreatedAt:     value.GetCreatedAt().AsTime(), ParentRevisionRef: optionalManagedString(value.GetParentRevisionRef()),
		ValidationDiagnostics: append([]string{}, value.GetValidationDiagnostics()...),
	}
	if value.GetValidatedAt() != nil {
		if value.GetValidatedAt().CheckValid() != nil {
			return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
		}
		validated := value.GetValidatedAt().AsTime()
		result.ValidatedAt = &validated
	}
	if value.GetPublishedAt() != nil {
		if value.GetPublishedAt().CheckValid() != nil {
			return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
		}
		published := value.GetPublishedAt().AsTime()
		result.PublishedAt = &published
	}
	scope, ok := promptScopeView(value.GetPromptScope())
	if !ok {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	result.PromptScope = scope
	return result, nil
}

func managedConsumerView(value *controlplanev1.ManagedConfigurationConsumer) (generated.ManagedConfigurationConsumer, error) {
	if value == nil || value.GetRef() == "" || len(value.GetRef()) > 160 || value.GetRevisionRef() == "" || !validManagedVersion(value.GetVersion()) {
		return generated.ManagedConfigurationConsumer{}, errManagedConfigurationShape
	}
	switch value.GetKind() {
	case "AGENT", "AGENT_CONTINUATION", "WORKFLOW", "SCHEDULE", "RUNTIME_ENVIRONMENT", "INTEGRATION_CONNECTION", "STT_SERVICE":
	default:
		return generated.ManagedConfigurationConsumer{}, errManagedConfigurationShape
	}
	return generated.ManagedConfigurationConsumer{Kind: generated.ManagedConfigurationConsumerKind(value.GetKind()), Ref: value.GetRef(), RevisionRef: value.GetRevisionRef(), Version: value.GetVersion()}, nil
}

func validManagedVersion(value int64) bool { return value > 0 && value <= maximumSafeJSONInteger }

func validManagedDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func optionalManagedString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
