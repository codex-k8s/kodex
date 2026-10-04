package callback

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const maximumAssistantCurrentConfigurationBytes = 1 << 20

func castAssistantOwnCurrentConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant current configuration response is invalid")
	current := response.GetCurrentConfiguration()
	if request.GetAssistantRef() != input.AgentRef || len(response.GetEntries()) != 0 || response.GetNextOffset() != 0 || current == nil ||
		!validAssistantCurrentReadMessage(current.ProtoReflect(), 0) || current.GetAgentVersion() < 1 ||
		current.GetConfiguration() == nil || current.GetPublishedOverlay() == nil || current.GetEnvironmentBinding() == nil ||
		current.GetImage() == nil || current.GetPolicy() == nil || len(current.GetTemplateVariables()) == 0 ||
		current.GetConfiguration().GetAgentRef() != input.AgentRef || current.GetEnvironmentBinding().GetAgentRef() != input.AgentRef ||
		current.GetEnvironmentRef() != current.GetEnvironmentBinding().GetEnvironmentRef() ||
		current.GetEnvironmentVersionRef() != current.GetEnvironmentBinding().GetVersionRef() ||
		!validAssistantResourceRef(current.GetInstructionTemplateRef()) || !validAssistantCatalogDigest(current.GetInstructionTemplateDigest()) ||
		!validAssistantResourceRef(current.GetEnvironmentRef()) || !validAssistantResourceRef(current.GetEnvironmentVersionRef()) ||
		current.GetEnvironmentVersion() < 1 || current.GetEnvironmentRevision() < 1 || !validAssistantCatalogDigest(current.GetEnvironmentDigest()) ||
		!validAssistantCurrentImage(input, current.GetImage()) ||
		strings.TrimSpace(current.GetPublishedInstructions()) == "" || current.GetPolicy().GetNetwork() == nil || !current.GetPolicy().GetNetwork().GetDenyByDefault() ||
		current.GetPolicy().GetKubernetesAccess() == nil || current.GetPolicy().GetKubernetesAccess().GetKind() != controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE {
		return nil, invalid
	}
	configuration, binding, overlay := current.GetConfiguration(), current.GetEnvironmentBinding(), current.GetPublishedOverlay()
	if !validAssistantResourceRef(configuration.GetRef()) || configuration.GetVersion() < 1 || !validAssistantCatalogDigest(configuration.GetDigest()) ||
		!validAssistantCatalogProvider(configuration.GetProvider()) || !assistantCatalogModelPattern.MatchString(configuration.GetModel()) ||
		configuration.GetRuntimeProfileRef() == "" || configuration.GetProviderPolicy() == nil ||
		!validAssistantResourceRef(binding.GetRef()) || binding.GetVersion() < 1 || !validAssistantCatalogDigest(binding.GetDigest()) ||
		!validAssistantResourceRef(overlay.GetRef()) || overlay.GetVersion() < 1 || overlay.GetState() != "PUBLISHED" || !validAssistantCatalogDigest(overlay.GetDigest()) {
		return nil, invalid
	}
	if input.IsSystemAssistant() {
		if current.GetSystemCoreRevision() == "" || current.GetSystemCoreInstructions() == "" || current.GetOwnerInstructionsRevision() < 1 {
			return nil, invalid
		}
	} else if current.GetSystemCoreRevision() != "" || current.GetSystemCoreInstructions() != "" || current.GetOwnerInstructions() != "" || current.GetOwnerInstructionsRevision() != 0 {
		return nil, invalid
	}
	seen := map[string]bool{}
	for _, variable := range current.GetTemplateVariables() {
		if variable.GetName() == "" || seen[variable.GetName()] || variable.GetReason() == 0 || variable.GetAvailable() != (variable.GetReason() == controlplanev1.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE) {
			return nil, invalid
		}
		seen[variable.GetName()] = true
	}
	seen = map[string]bool{}
	for _, binding := range current.GetSecretBindings() {
		if binding.GetName() == "" || seen[binding.GetName()] || !validAssistantResourceRef(binding.GetSecretRef()) || binding.GetRevision() < 1 {
			return nil, invalid
		}
		seen[binding.GetName()] = true
	}
	values := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(current.GetValues()))
	for _, value := range current.GetValues() {
		if value.GetName() == "" || seen[value.GetName()] {
			return nil, invalid
		}
		seen[value.GetName()] = true
		values = append(values, runtimecontract.RuntimeEnvironmentValue{Name: value.GetName(), Value: value.GetValue()})
	}
	if runtimecontract.ValidateRuntimeEnvironment(values, nil) != nil {
		return nil, invalid
	}
	// Сериализуется только новый typed public DTO: private descriptors отсутствуют
	// в его схеме. Не используется исходный RuntimeEnvironmentVersion или input.
	raw, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(current)
	if err != nil || len(raw) > maximumAssistantCurrentConfigurationBytes {
		return nil, invalid
	}
	var projection map[string]any
	if json.Unmarshal(raw, &projection) != nil {
		return nil, invalid
	}
	return map[string]any{"kind": "CURRENT_CONFIGURATION", "assistant_ref": input.AgentRef,
		"scope_kind": response.GetScopeKind(), "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(),
		"assistant_profile_ref": response.GetAssistantProfileRef(), "entries": []any{}, "next_offset": 0,
		"current_configuration": projection, "execution_snapshot": assistantCurrentExecutionSnapshot(input)}, nil
}

// Только безопасные pins immutable owner input, без fence, credential, input/task
// или resolved template values; не является перечитыванием текущих настроек.
func assistantCurrentExecutionSnapshot(input runtimecontract.RunnerInput) map[string]any {
	return map[string]any{"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": input.Attempt,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_version": input.RuntimeRevisionVersion, "runtime_revision_digest": input.RuntimeRevisionDigest,
		"runtime_config_ref": input.RuntimeConfigRef, "runtime_config_version": input.RuntimeConfigVersion, "runtime_config_digest": input.RuntimeConfigDigest,
		"environment_ref": input.RuntimeEnvironmentRef, "environment_version": input.RuntimeEnvironmentVersion, "environment_digest": input.RuntimeEnvironmentDigest,
		"image_reference": input.ImageReference, "image_manifest_digest": input.ImageManifestDigest,
		"instruction_ref": input.InstructionRef, "instruction_digest": input.InstructionDigest, "prompt_template_ref": input.PromptTemplateRef,
		"prompt_template_digest": input.PromptTemplateDigest, "model": input.Model, "reasoning_effort": input.EffectiveReasoningEffort}
}

func validAssistantCurrentImage(input runtimecontract.RunnerInput, image *controlplanev1.RuntimeEnvironmentImage) bool {
	if image.GetReference() == "" {
		// Canonical ORGANIZATION bootstrap может ещё не иметь опубликованного
		// image binding. Не подставляем image из immutable хода в current.
		return input.IsSystemAssistant() && image.GetDigest() == "" && image.GetArtifactRef() == "" && image.GetRecipeRef() == "" && image.GetRecipeGeneration() == 0
	}
	return assistantCatalogPinnedImagePattern.MatchString(image.GetReference()) && strings.HasPrefix(image.GetDigest(), "sha256:") &&
		validAssistantCatalogDigest(strings.TrimPrefix(image.GetDigest(), "sha256:")) && strings.HasSuffix(image.GetReference(), "@"+image.GetDigest())
}

// Recursion проверяет unknown metadata, enum, точность чисел и общий bounded
// typed DTO, но не приписывает прочитанным настройкам исполнительные полномочия.
func validAssistantCurrentReadMessage(message protoreflect.Message, depth int) bool {
	if !message.IsValid() || depth > 10 || len(message.GetUnknown()) != 0 {
		return false
	}
	valid := true
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			valid = false
			return false
		}
		validate := func(value protoreflect.Value) bool {
			switch field.Kind() {
			case protoreflect.MessageKind:
				return validAssistantCurrentReadMessage(value.Message(), depth+1)
			case protoreflect.StringKind:
				return utf8.ValidString(value.String()) && len(value.String()) <= 128<<10 && !strings.ContainsRune(value.String(), 0)
			case protoreflect.EnumKind:
				return field.Enum().Values().ByNumber(value.Enum()) != nil
			case protoreflect.Int64Kind, protoreflect.Int32Kind:
				return value.Int() >= 0 && value.Int() <= 9007199254740991
			case protoreflect.BoolKind:
				return true
			default:
				return false
			}
		}
		if field.IsList() {
			list := value.List()
			valid = list.Len() <= 128
			for index := 0; valid && index < list.Len(); index++ {
				valid = validate(list.Get(index))
			}
		} else {
			valid = validate(value)
		}
		return valid
	})
	return valid
}
