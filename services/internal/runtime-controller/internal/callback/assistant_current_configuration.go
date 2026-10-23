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

// maximumAssistantCurrentConfigurationBytes bounds a complete current/full source
// snapshot to one MiB before page projection; it does not raise per-page or wire budgets.
// maximumAssistantCurrentConfigurationBytes ограничивает полный current/full source
// snapshot одним MiB до page projection, не увеличивая per-page/wire budgets.
const maximumAssistantCurrentConfigurationBytes = 1 << 20

// castAssistantOwnCurrentConfiguration validates the caller's own typed current
// configuration, environment/image/inventory bindings, safe variables and scope-specific
// instruction fields, then emits the public DTO separately from immutable turn pins.
// The enclosing catalog path owns response authority; this function neither substitutes
// historical execution settings nor exposes credential descriptors or resolved task data.
// castAssistantOwnCurrentConfiguration проверяет собственные typed current configuration,
// environment/image/inventory bindings,safe variables и scope-specific instructions,
// выдавая public DTO отдельно от immutable turn pins. Response authority проверяет
// внешний catalog path; historical settings не подставляются,credential descriptors
// и resolved task data не выдаются.
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
		!validAssistantImageToolInventory(current.GetImageToolInventory(), current.GetImage()) ||
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
	execution, err := assistantCurrentExecutionSnapshot(input)
	if err != nil {
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
	search, err := assistantHostedSearchDiscovery(overlay.GetContent())
	if err != nil {
		return nil, invalid
	}
	projection["hosted_native_search"] = search
	return map[string]any{"kind": "CURRENT_CONFIGURATION", "assistant_ref": input.AgentRef,
		"scope_kind": response.GetScopeKind(), "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(),
		"assistant_profile_ref": response.GetAssistantProfileRef(), "entries": []any{}, "next_offset": 0,
		"current_configuration": projection, "execution_snapshot": execution}, nil
}

// assistantCurrentExecutionSnapshot validates workspace policy and projects safe
// immutable turn/revision/image/instruction/model pins with fixed read-only roots and
// search configuration. It omits fences, credentials, task/input and resolved values;
// provider-process status remains UNKNOWN, not an inferred live process observation.
// assistantCurrentExecutionSnapshot проверяет workspace policy и проецирует safe immutable
// turn/revision/image/instruction/model pins с fixed read-only roots и search configuration.
// Fences,credentials,task/input,resolved values исключены; provider-process status
// остаётся UNKNOWN, а не выводится как live process observation.
func assistantCurrentExecutionSnapshot(input runtimecontract.RunnerInput) (map[string]any, error) {
	if err := input.WorkspacePolicy.Validate(); err != nil {
		return nil, err
	}
	// Полный набор rules содержит private auth path. После canonical validation
	// публикуются только безопасные capacity и фиксированные logical roots.
	workspace := map[string]any{"revision": input.WorkspacePolicy.Revision, "root": input.WorkspacePolicy.Root,
		"maximum_writable_bytes": input.WorkspacePolicy.MaximumWritableBytes, "maximum_file_count": input.WorkspacePolicy.MaximumFileCount,
		"readonly_logical_roots": []string{"input", "knowledge", "context"}}
	search, err := assistantHostedSearchDiscovery(input.ConfigOverlay)
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": input.Attempt,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_version": input.RuntimeRevisionVersion, "runtime_revision_digest": input.RuntimeRevisionDigest,
		"runtime_config_ref": input.RuntimeConfigRef, "runtime_config_version": input.RuntimeConfigVersion, "runtime_config_digest": input.RuntimeConfigDigest,
		"environment_ref": input.RuntimeEnvironmentRef, "environment_version": input.RuntimeEnvironmentVersion, "environment_digest": input.RuntimeEnvironmentDigest,
		"image_reference": input.ImageReference, "image_manifest_digest": input.ImageManifestDigest,
		"provider_process": map[string]any{"status": "UNKNOWN", "provider": "CODEX"},
		"instruction_ref":  input.InstructionRef, "instruction_digest": input.InstructionDigest, "prompt_template_ref": input.PromptTemplateRef,
		"prompt_template_digest": input.PromptTemplateDigest, "model": input.Model, "reasoning_effort": input.EffectiveReasoningEffort,
		"workspace_policy": workspace, "hosted_native_search": search}, nil
}

// assistantHostedSearchDiscovery parses the overlay and reports the requested mode
// or the pinned SDK's cached default with provenance. It explicitly marks actual call
// verification false and does not claim effective provider behavior or shell access.
// assistantHostedSearchDiscovery разбирает overlay и сообщает requested mode либо cached
// default закреплённого SDK с provenance. Actual call verification явно false;
// effective provider behavior и shell access не утверждаются.
func assistantHostedSearchDiscovery(raw string) (map[string]any, error) {
	overlay, err := runtimecontract.ParseConfigOverlay(raw)
	if err != nil {
		return nil, errors.New("hosted search configuration is invalid")
	}
	mode, source := overlay.WebSearchMode, "PUBLISHED_CONFIG_OVERLAY"
	if mode == "" {
		mode, source = "cached", "SDK_DEFAULT_CACHED"
	}
	return map[string]any{"configuration_source": source, "requested_mode": mode, "owner_editable": true,
		"sandbox_domain_allowlist_applies": false, "actual_call_verified": false}, nil
}

// validAssistantCurrentImage allows a wholly unset image only for SYSTEM bootstrap;
// otherwise it requires a pinned reference whose suffix matches a canonical digest.
// It never fills an absent current binding from historical turn image pins.
// validAssistantCurrentImage допускает полностью unset image лишь для SYSTEM bootstrap;
// иначе требует pinned reference с suffix точного canonical digest.
// Отсутствующий current binding не заполняется historical turn image pins.
func validAssistantCurrentImage(input runtimecontract.RunnerInput, image *controlplanev1.RuntimeEnvironmentImage) bool {
	if image.GetReference() == "" {
		// Canonical ORGANIZATION bootstrap может ещё не иметь опубликованного
		// image binding. Не подставляем image из immutable хода в current.
		return input.IsSystemAssistant() && image.GetDigest() == "" && image.GetArtifactRef() == "" && image.GetRecipeRef() == "" && image.GetRecipeGeneration() == 0
	}
	return assistantCatalogPinnedImagePattern.MatchString(image.GetReference()) && strings.HasPrefix(image.GetDigest(), "sha256:") &&
		validAssistantCatalogDigest(strings.TrimPrefix(image.GetDigest(), "sha256:")) && strings.HasSuffix(image.GetReference(), "@"+image.GetDigest())
}

// validAssistantImageToolInventory accepts an explicitly empty UNAVAILABLE observation
// or validates VERIFIED inventory structure, image binding, platform uniqueness and
// closed tool manifests. It does not rehash external image bytes, verify admission
// signatures or turn observed binaries into execution capabilities.
// validAssistantImageToolInventory принимает явно пустое UNAVAILABLE observation либо
// проверяет VERIFIED inventory structure,image binding,platform uniqueness и closed
// tool manifests. External image bytes не хешируются повторно,admission signatures
// не проверяются и observed binaries не превращаются в execution capabilities.
func validAssistantImageToolInventory(value *controlplanev1.ImageToolInventory, image *controlplanev1.RuntimeEnvironmentImage) bool {
	if value == nil {
		return false
	}
	if value.GetStatus() == "UNAVAILABLE" {
		return value.GetSha256() == "" && value.GetImageDigest() == "" && value.GetProvenanceSha256() == "" && len(value.GetPlatforms()) == 0
	}
	if value.GetStatus() != "VERIFIED" || image.GetArtifactRef() == "" || value.GetImageDigest() != image.GetDigest() ||
		!validAssistantCatalogDigest(value.GetSha256()) || !validAssistantCatalogDigest(value.GetProvenanceSha256()) || len(value.GetPlatforms()) < 1 || len(value.GetPlatforms()) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, platform := range value.GetPlatforms() {
		if platform == nil || seen[platform.GetPlatform()] || !strings.HasPrefix(platform.GetPlatformDigest(), "sha256:") ||
			!validAssistantCatalogDigest(strings.TrimPrefix(platform.GetPlatformDigest(), "sha256:")) || !validAssistantCatalogDigest(platform.GetManifestSha256()) {
			return false
		}
		seen[platform.GetPlatform()] = true
		// Shared validator проверяет закрытый registry/type/bounds, но не назначает authority.
		manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, Platform: platform.GetPlatform(),
			SpecSHA256: value.GetSha256(), ImmutableBuildSHA256: value.GetSha256(), RuntimeContractSHA256: value.GetSha256()}
		for _, tool := range platform.GetTools() {
			if tool == nil {
				return false
			}
			manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: tool.GetName(), Status: tool.GetStatus(), Path: tool.GetPath(), Version: tool.GetVersion(), SHA256: tool.GetSha256(), Required: tool.GetRequired()})
		}
		if manifest.Validate() != nil {
			return false
		}
	}
	return true
}

// validAssistantCurrentReadMessage recursively rejects unknown fields, maps, depth
// over ten, unsupported kinds, invalid UTF-8/NUL, large strings/lists and integers
// outside the nonnegative JavaScript-exact range. It inspects populated typed fields
// only; domain-specific required fields and authority are checked by the caller.
// validAssistantCurrentReadMessage рекурсивно отвергает unknown fields,maps,depth
// более десяти,unsupported kinds,invalid UTF-8/NUL,large strings/lists и integers
// вне nonnegative JavaScript-exact range. Читаются лишь populated typed fields;
// domain-required fields и authority проверяет caller.
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
