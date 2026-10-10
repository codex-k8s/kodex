package callback

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/encoding/protojson"
)

func (server *Server) configurationCatalog(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	result, err := configurationCatalog(input, arguments)
	if err != nil {
		return nil, err
	}
	if raw, requested := arguments["assistant_configuration_catalog"]; requested {
		return server.assistantConfigurationCatalog(ctx, input, arguments, raw, result.(map[string]any))
	}
	_, requested := arguments["definition_query"]
	if !requested {
		if _, offsetOnly := arguments["definition_offset"]; offsetOnly {
			return nil, invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
		}
		return result, nil
	}
	if input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, errors.New("integration definition catalog is not available")
	}
	query, ok := arguments["definition_query"].(string)
	query = strings.TrimSpace(query)
	if !ok || utf8.RuneCountInString(query) > 80 {
		return nil, invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
	}
	offset := 0
	if raw, supplied := arguments["definition_offset"]; supplied {
		switch value := raw.(type) {
		case int:
			offset = value
		case float64:
			if value != float64(int(value)) {
				return nil, invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
			}
			offset = int(value)
		default:
			return nil, invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
		}
	}
	if offset < 0 || offset > 10000 {
		return nil, invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		IntegrationDefinitionCatalog: true, DefinitionQuery: query, DefinitionOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetAssistantTaskSession() != nil || response.GetAssistantConfigurationCatalog() != nil || len(response.GetResults()) != 0 || response.GetTruncated() || len(response.GetDefinitions()) > maximumAssistantIntegrationDefinitions ||
		response.GetNextDefinitionOffset() < 0 || response.GetNextDefinitionOffset() > 10000 ||
		(response.GetNextDefinitionOffset() != 0 && response.GetNextDefinitionOffset() <= int32(offset)) {
		return nil, errors.New("integration definition catalog response is invalid")
	}
	definitions := make([]map[string]any, 0, len(response.GetDefinitions()))
	for _, definition := range response.GetDefinitions() {
		if definition == nil || definition.GetKey() == "" || len(definition.GetConfigurationFields()) > 100 || len(definition.GetCapabilityKeys()) > 100 {
			return nil, errors.New("integration definition catalog response is invalid")
		}
		fields := make([]map[string]any, 0, len(definition.GetConfigurationFields()))
		for _, field := range definition.GetConfigurationFields() {
			if field == nil || field.GetKey() == "" {
				return nil, errors.New("integration definition catalog response is invalid")
			}
			entry := map[string]any{"key": field.GetKey(), "label": truncateRunes(field.GetLabel(), 120),
				"help": truncateRunes(field.GetHelp(), 300), "value_type": field.GetValueType(),
				"required": field.GetRequired(), "format": field.GetFormat(),
				"allowed_values": field.GetAllowedValues(), "maximum_length": field.GetMaximumLength()}
			if field.GetHasMinimum() {
				entry["minimum"] = field.GetMinimum()
			}
			if field.GetHasMaximum() {
				entry["maximum"] = field.GetMaximum()
			}
			fields = append(fields, entry)
		}
		definitions = append(definitions, map[string]any{
			"key": definition.GetKey(), "name": truncateRunes(definition.GetName(), 160),
			"description": truncateRunes(definition.GetDescription(), 500), "category": definition.GetCategory(),
			"adapter": definition.GetAdapter(), "origin": definition.GetOrigin(),
			"credential_secret_key": definition.GetCredentialSecretKey(),
			"capability_keys":       definition.GetCapabilityKeys(), "configuration_fields": fields,
		})
	}
	catalog := result.(map[string]any)
	catalog["integration_definitions"] = definitions
	if response.GetNextDefinitionOffset() > 0 {
		catalog["definition_next_offset"] = response.GetNextDefinitionOffset()
	}
	return catalog, nil
}

const maximumAssistantIntegrationDefinitions = 10

const maximumAssistantConfigurationEntries = 10
const maximumAssistantConfigurationPageModelBytes = 64 << 10

// Имя только для модельной проекции: refs, scope и owner snapshot не меняются.
func assistantCatalogResourceName(input runtimecontract.RunnerInput, ref, name string) string {
	if input.IsSystemAssistant() && ref != "" && ref == input.AgentRef && name == "i18n:SYSTEM_ASSISTANT_NAME" {
		return "Системный помощник"
	}
	return name
}

var assistantConfigurationCatalogKinds = []string{"ASSISTANTS", "RUNTIME_PROFILES", "PROVIDER_ACCOUNTS", "MODELS", "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS", "ROLE_ENVIRONMENTS", "CURRENT_CONFIGURATION"}

func assistantConfigurationCatalogInputSchema(input runtimecontract.RunnerInput) map[string]any {
	assistantRef := opaqueRefSchema()
	if input.AssistantScope == runtimecontract.AssistantScopeProject {
		assistantRef = enumSchema(input.AgentRef)
	}
	kinds := append([]string{}, assistantConfigurationCatalogKinds...)
	if input.AssistantScope == runtimecontract.AssistantScopeProject {
		kinds = append(kinds, "PROJECT_INTEGRATION_GRANTS")
	}
	if assistantRecipientIntegrationCatalogAvailable(input) {
		kinds = append(kinds, "RECIPIENT_INTEGRATION_GRANTS")
	}
	if assistantWorkflowConfigurationAvailable(input) {
		kinds = append(kinds, "WORKFLOW_CONFIGURATION")
	}
	if assistantAgentConfigurationAvailable(input) {
		kinds = append(kinds, "AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION")
	}
	properties := map[string]any{
		"kind": enumSchema(kinds...), "assistant_ref": assistantRef,
		"query": stringSchema(0, 80), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
		"account_ref": opaqueRefSchema(), "runtime_profile_ref": assistantRuntimeProfileKeySchema(),
	}
	if assistantRecipientIntegrationCatalogAvailable(input) || assistantWorkflowConfigurationAvailable(input) || assistantAgentConfigurationAvailable(input) {
		properties["entity_kind"], properties["entity_ref"] = enumSchema("AGENT", "WORKFLOW"), opaqueRefSchema()
	}
	if assistantWorkflowConfigurationAvailable(input) || assistantAgentConfigurationAvailable(input) {
		properties["configuration_offset_bytes"] = map[string]any{"type": "integer", "minimum": 0, "maximum": maximumAssistantCurrentConfigurationBytes, "default": 0}
		properties["maximum_bytes"] = map[string]any{"type": "integer", "minimum": 4, "maximum": maximumAssistantConfigurationPageBytes, "default": maximumAssistantConfigurationPageBytes}
		properties["configuration_sha256"] = map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	}
	return objectSchema([]string{"kind", "assistant_ref"}, properties)
}

func parseAssistantConfigurationCatalog(input runtimecontract.RunnerInput, arguments map[string]any, raw any) (*controlplanev1.AssistantConfigurationCatalogRequest, error) {
	invalid := errors.New("assistant configuration catalog request is invalid")
	invalidInput := invalidAssistantCatalogInput(assistantCatalogSelectorInvalid)
	if !input.IsAssistant() || input.OrganizationRef == "" || input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, invalid
	}
	for _, field := range []string{"agent_query", "agent_offset", "definition_query", "definition_offset"} {
		if _, mixed := arguments[field]; mixed {
			return nil, invalidAssistantCatalogInput(assistantCatalogShapeInvalid)
		}
	}
	if rawOperations, supplied := arguments["operation_types"]; supplied {
		operations, ok := rawOperations.([]any)
		if !ok || len(operations) != 0 {
			return nil, invalidAssistantCatalogInput(assistantCatalogShapeInvalid)
		}
	}
	selector, ok := raw.(map[string]any)
	if !ok || !onlyKeys(selector, "kind", "assistant_ref", "query", "offset", "account_ref", "runtime_profile_ref", "entity_kind", "entity_ref", "configuration_offset_bytes", "maximum_bytes", "configuration_sha256") {
		return nil, invalidAssistantCatalogInput(assistantCatalogShapeInvalid)
	}
	kind, ok := selector["kind"].(string)
	if !ok || !assistantConfigurationCatalogKindKnown(kind) {
		return nil, invalidInput
	}
	// Недоступный server-owned контекст не превращается в исправляемый ввод.
	if kind == "PROJECT_INTEGRATION_GRANTS" && input.AssistantScope != runtimecontract.AssistantScopeProject ||
		kind == "RECIPIENT_INTEGRATION_GRANTS" && !assistantRecipientIntegrationCatalogAvailable(input) ||
		kind == "WORKFLOW_CONFIGURATION" && !assistantWorkflowConfigurationAvailable(input) ||
		(kind == "AGENT_CONFIGURATION" || kind == "AGENT_RUNTIME_CONFIGURATION") && !assistantAgentConfigurationAvailable(input) {
		return nil, invalid
	}
	if _, err := parseAssistantConfigurationPage(selector, kind); err != nil {
		return nil, invalidAssistantCatalogInput(assistantCatalogPageInvalid)
	}
	assistantRef, ok := selector["assistant_ref"].(string)
	if !ok || !validAssistantResourceRef(assistantRef) {
		return nil, invalidInput
	}
	if input.AssistantScope == runtimecontract.AssistantScopeProject && assistantRef != input.AgentRef {
		return nil, invalid
	}
	request := &controlplanev1.AssistantConfigurationCatalogRequest{Kind: controlplanev1.AssistantConfigurationCatalogKind(controlplanev1.AssistantConfigurationCatalogKind_value["ASSISTANT_CONFIGURATION_CATALOG_KIND_"+kind]), AssistantRef: assistantRef}
	entityKind, hasKind := selector["entity_kind"]
	entityRef, hasRef := selector["entity_ref"]
	if hasKind != hasRef {
		return nil, invalidInput
	}
	if hasKind {
		selectedKind, kindOK := entityKind.(string)
		selectedRef, refOK := entityRef.(string)
		if !kindOK || !refOK || selectedKind != "AGENT" && selectedKind != "WORKFLOW" || !validAssistantResourceRef(selectedRef) ||
			kind != "RECIPIENT_INTEGRATION_GRANTS" && kind != "WORKFLOW_CONFIGURATION" && kind != "AGENT_CONFIGURATION" && kind != "AGENT_RUNTIME_CONFIGURATION" {
			return nil, invalidInput
		}
		request.EntityKind, request.EntityRef = selectedKind, selectedRef
	}
	if query, supplied := selector["query"]; supplied {
		value, ok := query.(string)
		if !ok || utf8.RuneCountInString(value) > 80 {
			return nil, invalidInput
		}
		request.Query = strings.TrimSpace(value)
	}
	if rawOffset, supplied := selector["offset"]; supplied {
		switch offset := rawOffset.(type) {
		case int:
			if offset < 0 || offset > 10000 {
				return nil, invalidInput
			}
			request.Offset = int32(offset)
		case float64:
			if offset < 0 || offset > 10000 || offset != float64(int32(offset)) {
				return nil, invalidInput
			}
			request.Offset = int32(offset)
		default:
			return nil, invalidInput
		}
	}
	for _, field := range []string{"account_ref", "runtime_profile_ref"} {
		if rawRef, supplied := selector[field]; supplied {
			ref, ok := rawRef.(string)
			if !ok || !safeInvocationRef(ref) || field == "account_ref" && (kind != "MODELS" || !validAssistantResourceRef(ref)) ||
				field == "runtime_profile_ref" && kind != "MODELS" && kind != "PROVIDER_ACCOUNTS" {
				return nil, invalidInput
			}
			if field == "account_ref" {
				request.AccountRef = ref
			} else {
				request.RuntimeProfileRef = ref
			}
		}
	}
	if kind == "PROJECT_INTEGRATION_GRANTS" && (request.AccountRef != "" || request.RuntimeProfileRef != "") {
		return nil, invalidInput
	}
	if kind == "RECIPIENT_INTEGRATION_GRANTS" && assistantRef != input.AgentRef {
		return nil, invalid
	}
	if kind == "RECIPIENT_INTEGRATION_GRANTS" && hasKind &&
		(input.AssistantContext.EntityKind != "WORKFLOW" && (request.EntityKind != input.AssistantContext.EntityKind || request.EntityRef != input.AssistantContext.EntityRef) ||
			input.AssistantContext.EntityKind == "WORKFLOW" && request.EntityKind == "WORKFLOW" && request.EntityRef != input.AssistantContext.EntityRef ||
			input.AssistantContext.EntityKind == "WORKFLOW" && request.EntityKind == "AGENT" && !assistantWorkflowConfigurationAvailable(input)) {
		return nil, invalid
	}
	if kind == "WORKFLOW_CONFIGURATION" {
		if assistantRef != input.AgentRef || hasRef && request.EntityRef != input.AssistantContext.EntityRef {
			return nil, invalid
		}
		if request.EntityKind != "WORKFLOW" || request.Query != "" || request.Offset != 0 {
			return nil, invalidInput
		}
	}
	if kind == "AGENT_CONFIGURATION" || kind == "AGENT_RUNTIME_CONFIGURATION" {
		if assistantRef != input.AgentRef || hasRef && request.EntityRef != input.AssistantContext.EntityRef {
			return nil, invalid
		}
		if request.EntityKind != "AGENT" || request.Query != "" || request.Offset != 0 {
			return nil, invalidInput
		}
	}
	if kind == "MODELS" && request.AccountRef == "" {
		return nil, invalidInput
	}
	if kind == "CURRENT_CONFIGURATION" && (assistantRef != input.AgentRef ||
		input.RuntimeRevisionRef == "" || input.RuntimeRevisionVersion < 1 || !validAssistantCatalogDigest(input.RuntimeRevisionDigest) ||
		!validAssistantResourceRef(input.RunRef) || !validAssistantResourceRef(input.NodeRef) || !validAssistantResourceRef(input.SessionRef) || !validAssistantResourceRef(input.TurnRef) || input.Attempt < 1) {
		return nil, invalid
	}
	if kind == "CURRENT_CONFIGURATION" && (request.Query != "" || request.Offset != 0) {
		return nil, invalidInput
	}
	return request, nil
}

func assistantConfigurationCatalogKindKnown(kind string) bool {
	if kind == "PROJECT_INTEGRATION_GRANTS" || kind == "RECIPIENT_INTEGRATION_GRANTS" || kind == "WORKFLOW_CONFIGURATION" || kind == "AGENT_CONFIGURATION" || kind == "AGENT_RUNTIME_CONFIGURATION" {
		return true
	}
	for _, candidate := range assistantConfigurationCatalogKinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func (server *Server) assistantConfigurationCatalog(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, raw any, catalog map[string]any) (any, error) {
	request, err := parseAssistantConfigurationCatalog(input, arguments, raw)
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, AssistantConfigurationCatalog: request,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetAssistantTaskSession() != nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.GetResults()) != 0 || response.GetTruncated() || len(response.GetDefinitions()) != 0 || response.GetNextDefinitionOffset() != 0 {
		return nil, errors.New("assistant configuration catalog response is invalid")
	}
	configuration, err := castAssistantConfigurationCatalog(input, request, response.GetAssistantConfigurationCatalog())
	if err != nil {
		return nil, err
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_CURRENT_CONFIGURATION {
		configuration["execution_snapshot"].(map[string]any)["provider_process"] = server.coordinator.providerProcessSnapshot(input)
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_WORKFLOW_CONFIGURATION || request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_CONFIGURATION || request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_RUNTIME_CONFIGURATION {
		page, pageErr := parseAssistantConfigurationPage(raw.(map[string]any), configuration["kind"].(string))
		if pageErr != nil {
			return nil, pageErr
		}
		return boundedAssistantConfigurationPageCatalog(configuration, page, input.ProjectRef, maximumAssistantConfigurationPageModelBytes)
	}
	catalog["assistant_configuration_catalog"] = configuration
	return catalog, nil
}

// Полный owner snapshot проходит прежний caster; размер модельной страницы
// задаёт только сервер, независимо от бюджета общего индекса сотрудников.
func boundedAssistantConfigurationPageCatalog(configuration map[string]any, page assistantConfigurationPageRequest, projectRef string, maximumModelBytes int) (map[string]any, error) {
	for {
		projected, projectionErr := pageAssistantConfigurationSnapshot(configuration, page)
		if projectionErr != nil {
			return nil, projectionErr
		}
		catalog := map[string]any{"current_project_ref": projectRef, "agents": []any{}, "assistant_configuration_catalog": projected}
		encoded, encodeErr := json.Marshal(catalog)
		if encodeErr == nil && len(encoded) <= maximumModelBytes {
			return catalog, nil
		}
		if encodeErr != nil || page.maximum <= utf8.UTFMax {
			return nil, errAssistantConfigurationPage
		}
		// Escaping учитывается до выдачи; next offset описывает фактические bytes.
		page.maximum = max(utf8.UTFMax, page.maximum/2)
	}
}

func castAssistantConfigurationCatalog(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant configuration catalog response is invalid")
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_RUNTIME_CONFIGURATION {
		return castAssistantAgentRuntimeConfiguration(input, request, response)
	}
	if response != nil && response.GetAgentRuntimeConfiguration() != nil {
		return nil, invalid
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_CONFIGURATION {
		return castAssistantAgentConfiguration(input, request, response)
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_WORKFLOW_CONFIGURATION {
		return castAssistantWorkflowConfiguration(input, request, response)
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_RECIPIENT_INTEGRATION_GRANTS {
		return castAssistantRecipientIntegrationCatalog(input, request, response)
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() ||
		response.GetRecipientIntegrationGrants() != nil || response.GetWorkflowConfiguration() != nil || response.GetAgentConfiguration() != nil ||
		response.GetAssistantRef() != request.GetAssistantRef() || response.GetOrganizationRef() != input.OrganizationRef ||
		!validAssistantCatalogScope(response.GetScopeKind(), response.GetProjectRef(), response.GetAssistantProfileRef()) ||
		len(response.GetEntries()) > maximumAssistantConfigurationEntries || response.GetNextOffset() < 0 || response.GetNextOffset() > 10000 ||
		response.GetNextOffset() != 0 && response.GetNextOffset() <= request.GetOffset() {
		return nil, invalid
	}
	if input.AssistantScope == runtimecontract.AssistantScopeProject && (response.GetScopeKind() != "PROJECT" ||
		response.GetProjectRef() != input.ProjectRef || response.GetAssistantProfileRef() != input.AssistantProfileRef) {
		return nil, invalid
	}
	if input.IsSystemAssistant() && ((request.GetAssistantRef() == input.AgentRef && response.GetScopeKind() != "ORGANIZATION") ||
		(request.GetAssistantRef() != input.AgentRef && response.GetScopeKind() != "PROJECT")) {
		return nil, invalid
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_PROJECT_INTEGRATION_GRANTS {
		return castProjectAssistantIntegrationCatalog(input, request, response)
	}
	if len(response.GetProjectIntegrationGrants()) != 0 {
		return nil, invalid
	}
	if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_CURRENT_CONFIGURATION {
		return castAssistantOwnCurrentConfiguration(input, request, response)
	}
	if response.GetCurrentConfiguration() != nil {
		return nil, invalid
	}
	entries := make([]map[string]any, 0, len(response.GetEntries()))
	seen := make(map[string]struct{}, len(response.GetEntries()))
	for _, entry := range response.GetEntries() {
		if entry == nil || len(entry.ProtoReflect().GetUnknown()) != 0 || entry.GetOrganizationRef() != input.OrganizationRef ||
			!validAssistantConfigurationCatalogEntry(input, request.GetKind(), response, entry) {
			return nil, invalid
		}
		if _, duplicate := seen[entry.GetRef()]; duplicate {
			return nil, invalid
		}
		seen[entry.GetRef()] = struct{}{}
		projection := map[string]any{"ref": entry.GetRef(), "name": truncateRunes(entry.GetName(), 160),
			"scope_kind": entry.GetScopeKind(), "organization_ref": entry.GetOrganizationRef(), "project_ref": entry.GetProjectRef(),
			"assistant_profile_ref": entry.GetAssistantProfileRef(), "provider": entry.GetProvider(), "model": entry.GetModel(),
			"version": entry.GetVersion(), "recipe_generation": entry.GetRecipeGeneration(), "reference": entry.GetReference(),
			"manifest_digest": entry.GetManifestDigest(), "catalog_revision": entry.GetCatalogRevision(), "catalog_digest": entry.GetCatalogDigest(),
			"reasoning_efforts": append([]string{}, entry.GetReasoningEfforts()...), "default_reasoning_effort": entry.GetDefaultReasoningEffort()}
		if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ROLE_IMAGE_RECIPES {
			projection["environment_key"] = entry.GetEnvironmentKey()
		}
		if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ASSISTANTS {
			if entry.GetScopeKind() == "ORGANIZATION" {
				projection["name"] = assistantCatalogResourceName(input, entry.GetRef(), entry.GetName())
			}
			projection["runtime_environment_ref"] = entry.GetRuntimeEnvironmentRef()
		}
		if request.GetKind() == controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_IMAGE_ARTIFACTS {
			raw, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(entry.GetVerifiedToolInventory())
			if err != nil || len(raw) > maximumAssistantCurrentConfigurationBytes {
				return nil, invalid
			}
			var inventory map[string]any
			if json.Unmarshal(raw, &inventory) != nil {
				return nil, invalid
			}
			projection["admission_verdict"], projection["promotion_state"] = entry.GetAdmissionVerdict(), entry.GetPromotionState()
			projection["verified_tool_inventory"] = inventory
		}
		entries = append(entries, projection)
	}
	return map[string]any{"kind": strings.TrimPrefix(response.GetKind().String(), "ASSISTANT_CONFIGURATION_CATALOG_KIND_"),
		"assistant_ref": response.GetAssistantRef(), "scope_kind": response.GetScopeKind(), "organization_ref": response.GetOrganizationRef(),
		"project_ref": response.GetProjectRef(), "assistant_profile_ref": response.GetAssistantProfileRef(), "entries": entries,
		"next_offset": response.GetNextOffset()}, nil
}

func validAssistantCatalogScope(scope, projectRef, profileRef string) bool {
	return scope == "ORGANIZATION" && projectRef == "" && profileRef == "" ||
		scope == "PROJECT" && validAssistantResourceRef(projectRef) && validAssistantResourceRef(profileRef)
}

var assistantCatalogPinnedImagePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:/_-]*@sha256:[a-f0-9]{64}$`)
var assistantCatalogModelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
var assistantCatalogEnvironmentKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,99}$`)

func validAssistantConfigurationCatalogEntry(input runtimecontract.RunnerInput, kind controlplanev1.AssistantConfigurationCatalogKind, response *controlplanev1.AssistantConfigurationCatalogResponse, entry *controlplanev1.AssistantConfigurationCatalogEntry) bool {
	if !utf8.ValidString(entry.GetName()) || strings.TrimSpace(entry.GetName()) == "" || utf8.RuneCountInString(entry.GetName()) > 160 ||
		!validAssistantCatalogScope(entry.GetScopeKind(), entry.GetProjectRef(), entry.GetAssistantProfileRef()) || entry.GetVersion() < 0 || entry.GetVersion() > 9007199254740991 || entry.GetRecipeGeneration() < 0 || entry.GetRecipeGeneration() > 9007199254740991 {
		return false
	}
	if kind != controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ASSISTANTS &&
		(entry.GetScopeKind() != response.GetScopeKind() || entry.GetProjectRef() != response.GetProjectRef() || entry.GetAssistantProfileRef() != response.GetAssistantProfileRef()) {
		return false
	}
	allowed := map[string]bool{}
	switch kind {
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ASSISTANTS:
		if !validAssistantResourceRef(entry.GetRef()) || entry.GetVersion() < 1 || entry.GetScopeKind() == "ORGANIZATION" && entry.GetRef() != input.AgentRef ||
			entry.GetRuntimeEnvironmentRef() != "" && !validAssistantResourceRef(entry.GetRuntimeEnvironmentRef()) ||
			input.AssistantScope == runtimecontract.AssistantScopeProject && (entry.GetRef() != input.AgentRef || entry.GetScopeKind() != "PROJECT" || entry.GetProjectRef() != input.ProjectRef || entry.GetAssistantProfileRef() != input.AssistantProfileRef) {
			return false
		}
		allowed["version"], allowed["runtime_environment_ref"] = true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_RUNTIME_PROFILES:
		if !safeInvocationRef(entry.GetRef()) || entry.GetVersion() < 1 || !validAssistantCatalogProvider(entry.GetProvider()) || !assistantCatalogModelPattern.MatchString(entry.GetModel()) {
			return false
		}
		allowed["version"], allowed["provider"], allowed["model"] = true, true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_PROVIDER_ACCOUNTS:
		if !validAssistantResourceRef(entry.GetRef()) || entry.GetVersion() < 1 || !validAssistantCatalogProvider(entry.GetProvider()) {
			return false
		}
		allowed["version"], allowed["provider"] = true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_MODELS:
		if !assistantCatalogModelPattern.MatchString(entry.GetRef()) || entry.GetModel() != entry.GetRef() || entry.GetName() != entry.GetRef() ||
			!validAssistantCatalogProvider(entry.GetProvider()) || entry.GetCatalogRevision() == "" || len(entry.GetCatalogRevision()) > 128 ||
			!utf8.ValidString(entry.GetCatalogRevision()) || !validAssistantCatalogDigest(entry.GetCatalogDigest()) || !validAssistantCatalogReasoning(entry.GetReasoningEfforts(), entry.GetDefaultReasoningEffort()) {
			return false
		}
		allowed["provider"], allowed["model"], allowed["catalog_revision"], allowed["catalog_digest"], allowed["reasoning_efforts"], allowed["default_reasoning_effort"] = true, true, true, true, true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ROLE_IMAGE_RECIPES:
		if !validAssistantResourceRef(entry.GetRef()) || entry.GetVersion() < 1 || entry.GetRecipeGeneration() < 1 || !assistantCatalogEnvironmentKeyPattern.MatchString(entry.GetEnvironmentKey()) {
			return false
		}
		allowed["version"], allowed["recipe_generation"], allowed["environment_key"] = true, true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_IMAGE_ARTIFACTS:
		if !validAssistantResourceRef(entry.GetRef()) || entry.GetVersion() < 1 || entry.GetRecipeGeneration() < 1 ||
			!strings.HasPrefix(entry.GetManifestDigest(), "sha256:") || !validAssistantCatalogDigest(strings.TrimPrefix(entry.GetManifestDigest(), "sha256:")) ||
			!assistantCatalogPinnedImagePattern.MatchString(entry.GetReference()) || !strings.HasSuffix(entry.GetReference(), "@"+entry.GetManifestDigest()) ||
			entry.GetAdmissionVerdict() != "ACCEPTED" || entry.GetPromotionState() != "PROMOTED" ||
			entry.GetVerifiedToolInventory() == nil || !validAssistantCurrentReadMessage(entry.GetVerifiedToolInventory().ProtoReflect(), 0) ||
			!validAssistantImageToolInventory(entry.GetVerifiedToolInventory(), &controlplanev1.RuntimeEnvironmentImage{ArtifactRef: entry.GetRef(), Digest: entry.GetManifestDigest()}) {
			return false
		}
		allowed["version"], allowed["recipe_generation"], allowed["reference"], allowed["manifest_digest"] = true, true, true, true
		allowed["admission_verdict"], allowed["promotion_state"], allowed["verified_tool_inventory"] = true, true, true
	case controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ROLE_ENVIRONMENTS:
		if !assistantCatalogEnvironmentKeyPattern.MatchString(entry.GetRef()) {
			return false
		}
	default:
		return false
	}
	present := map[string]bool{"provider": entry.GetProvider() != "", "model": entry.GetModel() != "", "version": entry.GetVersion() != 0,
		"recipe_generation": entry.GetRecipeGeneration() != 0, "reference": entry.GetReference() != "", "manifest_digest": entry.GetManifestDigest() != "",
		"catalog_revision": entry.GetCatalogRevision() != "", "catalog_digest": entry.GetCatalogDigest() != "", "reasoning_efforts": len(entry.GetReasoningEfforts()) != 0,
		"default_reasoning_effort": entry.GetDefaultReasoningEffort() != "", "runtime_environment_ref": entry.GetRuntimeEnvironmentRef() != "",
		"admission_verdict": entry.GetAdmissionVerdict() != "", "promotion_state": entry.GetPromotionState() != "", "verified_tool_inventory": entry.GetVerifiedToolInventory() != nil,
		"environment_key": entry.GetEnvironmentKey() != ""}
	for field, supplied := range present {
		if supplied && !allowed[field] {
			return false
		}
	}
	return true
}

func validAssistantCatalogProvider(provider string) bool {
	return provider != "" && len(provider) <= 64 && utf8.ValidString(provider) && strings.TrimSpace(provider) == provider
}

func validAssistantCatalogDigest(digest string) bool {
	raw, err := hex.DecodeString(digest)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == digest
}

func validAssistantCatalogReasoning(efforts []string, defaultEffort string) bool {
	if len(efforts) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, effort := range efforts {
		if runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil {
			return false
		}
		if seen[effort] {
			return false
		}
		seen[effort] = true
	}
	return len(efforts) == 0 && defaultEffort == "" || seen[defaultEffort]
}
