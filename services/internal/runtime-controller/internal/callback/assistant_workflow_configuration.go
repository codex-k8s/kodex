package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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

// Закрытая read-модель сохраняет полную семантику draft, включая dependencies и defaults.
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

type assistantWorkflowDraftInput struct {
	Key, Label, Type, Help, DefaultValue string
	Required                             bool
	Options                              []string
}

type assistantWorkflowDraftStep struct {
	Key, Name, AgentRef, Instructions, ExpectedResult string
	Position, ParallelGroup, TimeoutSeconds           int32
	Parallel, HumanGateAfter                          bool
	DependsOn, GateDecisions, RequiredCapabilityKeys  []string
}

type assistantWorkflowReadSnapshot struct {
	WorkflowRef, ProjectRef, Name, Purpose, CoordinatorAgentRef, Instructions, CompletionCriteria string
	MaxConcurrency                                                                                int32
	TimeoutSeconds                                                                                int64
	InputFields                                                                                   []assistantWorkflowReadInput
	Steps                                                                                         []assistantWorkflowReadStep
	Draft                                                                                         assistantWorkflowDraft
}

type assistantWorkflowReadInput struct {
	Key, Label, Description, ValueType string
	Required                           bool
	Options                            []string
}

type assistantWorkflowReadStep struct {
	Key, Name, Purpose, AgentRef, ExpectedResult string
	Parallel, HumanGate                          bool
	ParallelGroup, TimeoutSeconds                int32
	GateDecisions, RequiredCapabilityKeys        []string
}

func castAssistantWorkflowConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant workflow configuration response is invalid")
	if !assistantWorkflowConfigurationAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "WORKFLOW" || request.GetEntityRef() != input.AssistantContext.EntityRef || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef ||
		response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil {
		return nil, invalid
	}
	configuration := response.GetWorkflowConfiguration()
	if configuration == nil || len(configuration.ProtoReflect().GetUnknown()) != 0 || configuration.GetWorkflowRef() != request.GetEntityRef() || configuration.GetProjectRef() != response.GetProjectRef() || configuration.GetVersion() != *input.AssistantContext.EntityVersion || configuration.GetVersion() > 9007199254740991 || len(configuration.GetConfigurationJson()) == 0 || len(configuration.GetConfigurationJson()) > maximumAssistantCurrentConfigurationBytes {
		return nil, invalid
	}
	raw := configuration.GetConfigurationJson()
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != configuration.GetConfigurationSha256() {
		return nil, invalid
	}
	var typed assistantWorkflowReadSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil || typed.WorkflowRef != configuration.GetWorkflowRef() || typed.ProjectRef != configuration.GetProjectRef() || !validAssistantWorkflowReadSnapshot(typed) {
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

func validAssistantWorkflowReadSnapshot(value assistantWorkflowReadSnapshot) bool {
	draft := value.Draft
	if strings.TrimSpace(value.Name) == "" || len(value.Name) > 160 || len(value.Purpose) > 2000 || value.Name != draft.Name || value.Purpose != draft.Purpose || value.CoordinatorAgentRef != draft.CoordinatorAgentRef || !validAssistantResourceRef(value.CoordinatorAgentRef) || value.Instructions != draft.Instructions || value.CompletionCriteria != draft.CompletionCriteria ||
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
		if step.Key == "" || len(step.Key) > 96 || seen[step.Key] || strings.TrimSpace(step.Name) == "" || len(step.Name) > 160 || strings.TrimSpace(step.Purpose) == "" || len(step.Purpose) > 1000 || len(step.ExpectedResult) > 1000 || step.TimeoutSeconds < 1 || step.TimeoutSeconds > 86400 || step.ParallelGroup < 0 || step.ParallelGroup > 50 || original.Position != int32(index+1) || step.Key != original.Key || step.Name != original.Name || step.Purpose != original.Instructions || step.AgentRef != original.AgentRef || !validAssistantResourceRef(step.AgentRef) || step.Parallel != original.Parallel || step.ParallelGroup != original.ParallelGroup || step.TimeoutSeconds != original.TimeoutSeconds || step.ExpectedResult != original.ExpectedResult || step.HumanGate != original.HumanGateAfter ||
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
