package callback

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantWorkflowConfigurationAvailable requires assistant scope, a versioned
// WORKFLOW native context and snapshot UPDATE_WORKFLOW permission for full source
// discovery; safe metadata availability has a separate, less context-bound predicate.
// assistantWorkflowConfigurationAvailable требует assistant scope,versioned WORKFLOW
// native context и snapshot UPDATE_WORKFLOW permission для full source discovery;
// safe metadata имеет отдельный predicate без этой привязки к экрану.
func assistantWorkflowConfigurationAvailable(input runtimecontract.RunnerInput) bool {
	context := input.AssistantContext
	if !input.IsAssistant() || context == nil || context.EntityKind != "WORKFLOW" || !validAssistantResourceRef(context.EntityRef) || context.EntityVersion == nil || *context.EntityVersion < 1 {
		return false
	}
	for _, operation := range context.AllowedOperations {
		if operation == "UPDATE_WORKFLOW" {
			return true
		}
	}
	return false
}

// assistantWorkflowDraft retains the complete owner draft read semantics, including
// input defaults, ordered dependency edges, capability ceilings and result schema.
// It is neither an executable grant nor a replacement for the owner workflow entity.
// assistantWorkflowDraft сохраняет полную семантику owner draft read, включая input
// defaults,ordered dependencies,capability ceilings и result schema. Это не executable
// grant и не замена owner workflow entity.
type assistantWorkflowDraft struct {
	Ref, Name, Purpose, CoordinatorAgentRef, Instructions, CompletionCriteria string
	VersionNumber                                                             int32
	Inputs                                                                    []assistantWorkflowDraftInput
	Steps                                                                     []assistantWorkflowDraftStep
	AgentRefs                                                                 []string
	Concurrency                                                               int32
	TimeoutSeconds                                                            int64
	GateDecisions                                                             []string
	ResultSchema                                                              map[string]any
}

// assistantWorkflowDraftInput preserves draft field identity, type, options and
// default value alongside display metadata for exact update readback.
// assistantWorkflowDraftInput сохраняет draft field identity,type,options,default
// value и display metadata для точного update readback.
type assistantWorkflowDraftInput struct {
	Key, Label, Type, Help, DefaultValue string
	Required                             bool
	Options                              []string
}

// assistantWorkflowDraftStep preserves full draft step identity, positional ordering,
// dependencies, parallelism, gates and capability ceiling; these fields describe
// configuration and do not authorize a child execution.
// assistantWorkflowDraftStep сохраняет draft step identity,position,dependencies,
// parallelism,gates и capability ceiling; это описание configuration,
// не авторизация дочернего execution.
type assistantWorkflowDraftStep struct {
	Key, Name, AgentRef, Instructions, ExpectedResult string
	Position, ParallelGroup, TimeoutSeconds           int32
	Parallel, HumanGateAfter                          bool
	DependsOn, GateDecisions, RequiredCapabilityKeys  []string
}

// assistantWorkflowReadSnapshot pairs editable top-level workflow/field/step views
// with their full original draft so semantic consistency can be checked before
// an owner-reviewed update; it does not represent a running immutable workflow revision.
// assistantWorkflowReadSnapshot связывает editable workflow/field/step views с полным
// исходным draft для semantic consistency проверки перед owner-reviewed update;
// это не running immutable workflow revision.
type assistantWorkflowReadSnapshot struct {
	WorkflowRef, ProjectRef, Name, Purpose, CoordinatorAgentRef, Instructions, CompletionCriteria string
	MaxConcurrency                                                                                int32
	TimeoutSeconds                                                                                int64
	InputFields                                                                                   []assistantWorkflowReadInput
	Steps                                                                                         []assistantWorkflowReadStep
	Draft                                                                                         assistantWorkflowDraft
}

// assistantWorkflowReadInput contains the editable field projection without its
// draft-only default; validation compares it to the original draft input.
// assistantWorkflowReadInput содержит editable field projection без draft-only
// default; validation сравнивает его с исходным draft input.
type assistantWorkflowReadInput struct {
	Key, Label, Description, ValueType string
	Required                           bool
	Options                            []string
}

// assistantWorkflowReadStep contains editable ordered step metadata, gates and
// capability keys; dependency edges stay in the paired full draft, not this projection.
// assistantWorkflowReadStep содержит editable ordered step metadata,gates
// и capability keys; dependency edges находятся в paired full draft, не этой projection.
type assistantWorkflowReadStep struct {
	Key, Name, Purpose, AgentRef, ExpectedResult string
	Parallel, HumanGate                          bool
	ParallelGroup, TimeoutSeconds                int32
	GateDecisions, RequiredCapabilityKeys        []string
}

// castAssistantWorkflowConfiguration checks the exact versioned native workflow
// and tenant/project response, then validates digest, closed canonical JSON and
// full draft/projection consistency. Errors return no snapshot; owner authorization
// and update/publish effects remain outside this read caster.
// castAssistantWorkflowConfiguration проверяет exact versioned native workflow
// и tenant/project response, затем digest,closed canonical JSON и соответствие
// full draft/projection. При ошибке snapshot не выдаётся; owner authorization
// и update/publish effects остаются вне read caster.
func castAssistantWorkflowConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant workflow configuration response is invalid")
	if !assistantWorkflowConfigurationAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "WORKFLOW" || request.GetEntityRef() != input.AssistantContext.EntityRef || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef ||
		response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil || response.GetAgentConfiguration() != nil {
		return nil, invalid
	}
	configuration := response.GetWorkflowConfiguration()
	if configuration == nil || len(configuration.ProtoReflect().GetUnknown()) != 0 || configuration.GetWorkflowRef() != request.GetEntityRef() || configuration.GetProjectRef() != response.GetProjectRef() || configuration.GetVersion() != *input.AssistantContext.EntityVersion || configuration.GetVersion() > 9007199254740991 || len(configuration.GetConfigurationJson()) == 0 || len(configuration.GetConfigurationJson()) > maximumAssistantCurrentConfigurationBytes {
		return nil, invalid
	}
	raw := configuration.GetConfigurationJson()
	var typed assistantWorkflowReadSnapshot
	if !decodeAssistantConfigurationSnapshot(raw, configuration.GetConfigurationSha256(), &typed) || typed.WorkflowRef != configuration.GetWorkflowRef() || typed.ProjectRef != configuration.GetProjectRef() || !validAssistantWorkflowReadSnapshot(typed) {
		return nil, invalid
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 12 || !onlyKeys(snapshot, "workflowRef", "projectRef", "name", "purpose", "coordinatorAgentRef", "instructions", "completionCriteria", "maxConcurrency", "timeoutSeconds", "inputFields", "steps", "draft") || !closedAssistantWorkflowSnapshot(snapshot) {
		return nil, invalid
	}
	canonical, err := json.Marshal(snapshot)
	// Canonical bytes отклоняют duplicates, неоднозначные числа и trailing data.
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	return map[string]any{"kind": "WORKFLOW_CONFIGURATION", "assistant_ref": input.AgentRef, "scope_kind": "PROJECT", "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "entries": []any{}, "next_offset": 0,
		"workflow_configuration": map[string]any{"workflow_ref": configuration.GetWorkflowRef(), "project_ref": configuration.GetProjectRef(), "version": configuration.GetVersion(), "configuration_sha256": configuration.GetConfigurationSha256(), "configuration": snapshot}}, nil
}

// closedAssistantWorkflowSnapshot rejects unknown draft keys and requires exact
// field sets for each present input/step row in both projections. Nil collections
// are permitted here; required cardinality and semantic equality are checked separately.
// closedAssistantWorkflowSnapshot отвергает unknown draft keys и требует exact fields
// каждой присутствующей input/step row обеих projections. Nil collections здесь
// допускаются; обязательные cardinality и semantic equality проверяются отдельно.
func closedAssistantWorkflowSnapshot(snapshot map[string]any) bool {
	draft, ok := snapshot["draft"].(map[string]any)
	if !ok || !onlyKeys(draft, "Ref", "Name", "Purpose", "CoordinatorAgentRef", "Instructions", "CompletionCriteria", "VersionNumber", "Inputs", "Steps", "AgentRefs", "Concurrency", "TimeoutSeconds", "GateDecisions", "ResultSchema") {
		return false
	}
	for _, collection := range []struct {
		parent map[string]any
		key    string
		fields []string
	}{
		{snapshot, "inputFields", []string{"key", "label", "description", "valueType", "required", "options"}},
		{snapshot, "steps", []string{"key", "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys"}},
		{draft, "Inputs", []string{"Key", "Label", "Type", "Help", "DefaultValue", "Required", "Options"}},
		{draft, "Steps", []string{"Key", "Name", "AgentRef", "Instructions", "ExpectedResult", "Position", "ParallelGroup", "TimeoutSeconds", "Parallel", "HumanGateAfter", "DependsOn", "GateDecisions", "RequiredCapabilityKeys"}},
	} {
		raw := collection.parent[collection.key]
		if raw == nil {
			continue
		}
		rows, ok := raw.([]any)
		if !ok {
			return false
		}
		for _, rawRow := range rows {
			row, ok := rawRow.(map[string]any)
			if !ok || !onlyKeys(row, collection.fields...) || len(row) != len(collection.fields) {
				return false
			}
		}
	}
	return true
}

// validAssistantWorkflowReadSnapshot compares draft and editable views positionally,
// checks Unicode text bounds, refs, gates and capability strings, and requires every
// dependency to refer to an already-seen step. It preserves edges/defaults as read
// data rather than inferring a new DAG or resolving effective execution permissions.
// validAssistantWorkflowReadSnapshot позиционно сравнивает draft/editable views,
// проверяет Unicode bounds,refs,gates,capability strings и требует ссылки dependencies
// на уже просмотренный step. Edges/defaults сохраняются как read data; новый DAG
// не выводится и effective execution permissions не разрешаются.
func validAssistantWorkflowReadSnapshot(value assistantWorkflowReadSnapshot) bool {
	draft := value.Draft
	if strings.TrimSpace(value.Name) == "" || utf8.RuneCountInString(value.Name) > 160 || utf8.RuneCountInString(value.Purpose) > 2000 || value.Name != draft.Name || value.Purpose != draft.Purpose || value.CoordinatorAgentRef != draft.CoordinatorAgentRef || !validAssistantResourceRef(value.CoordinatorAgentRef) || value.Instructions != draft.Instructions || value.CompletionCriteria != draft.CompletionCriteria ||
		value.MaxConcurrency != draft.Concurrency || value.MaxConcurrency < 1 || value.MaxConcurrency > 100 || value.TimeoutSeconds != draft.TimeoutSeconds || value.TimeoutSeconds < 1 || value.TimeoutSeconds > 604800 || len(value.Steps) == 0 || len(value.Steps) > 200 || len(value.Steps) != len(draft.Steps) || len(value.InputFields) > 100 || len(value.InputFields) != len(draft.Inputs) {
		return false
	}
	for index, field := range value.InputFields {
		original := draft.Inputs[index]
		if field.Key != original.Key || field.Label != original.Label || field.Description != original.Help || field.ValueType != original.Type || field.Required != original.Required || !reflect.DeepEqual(field.Options, append([]string{}, original.Options...)) {
			return false
		}
	}
	seen := map[string]bool{}
	for index, step := range value.Steps {
		original := draft.Steps[index]
		if step.Key == "" || len(step.Key) > 96 || seen[step.Key] || strings.TrimSpace(step.Name) == "" || utf8.RuneCountInString(step.Name) > 160 || strings.TrimSpace(step.Purpose) == "" || utf8.RuneCountInString(step.Purpose) > 1000 || utf8.RuneCountInString(step.ExpectedResult) > 1000 || step.TimeoutSeconds < 1 || step.TimeoutSeconds > 86400 || step.ParallelGroup < 0 || step.ParallelGroup > 50 || original.Position != int32(index+1) || step.Key != original.Key || step.Name != original.Name || step.Purpose != original.Instructions || step.AgentRef != original.AgentRef || !validAssistantResourceRef(step.AgentRef) || step.Parallel != original.Parallel || step.ParallelGroup != original.ParallelGroup || step.TimeoutSeconds != original.TimeoutSeconds || step.ExpectedResult != original.ExpectedResult || step.HumanGate != original.HumanGateAfter ||
			!reflect.DeepEqual(step.GateDecisions, append([]string{}, original.GateDecisions...)) || !reflect.DeepEqual(step.RequiredCapabilityKeys, append([]string{}, original.RequiredCapabilityKeys...)) || len(step.RequiredCapabilityKeys) > 50 {
			return false
		}
		for _, text := range []string{step.Key, step.Name, step.Purpose, step.ExpectedResult} {
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return false
			}
		}
		for _, capability := range step.RequiredCapabilityKeys {
			if len(capability) == 0 || len(capability) > 80 || strings.Trim(capability, "abcdefghijklmnopqrstuvwxyz0123456789._-") != "" {
				return false
			}
		}
		for _, decision := range step.GateDecisions {
			if decision != "APPROVE" && decision != "REJECT" && decision != "REQUEST_CHANGES" && decision != "CANCEL" {
				return false
			}
		}
		for _, dependency := range original.DependsOn {
			if !seen[dependency] {
				return false
			}
		}
		seen[step.Key] = true
	}
	return true
}
