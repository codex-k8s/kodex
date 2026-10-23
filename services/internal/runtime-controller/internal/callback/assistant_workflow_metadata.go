package callback

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantWorkflowMetadataSnapshot is the safe current published-workflow metadata
// shape with revision, ordered steps and gates, excluding draft instructions, input
// defaults, result schema and execution grants.
// assistantWorkflowMetadataSnapshot — safe current published-workflow metadata shape
// с revision,ordered steps/gates без draft instructions,input defaults,result schema
// и execution grants.
type assistantWorkflowMetadataSnapshot struct {
	WorkflowRef         string                          `json:"workflow_ref"`
	ProjectRef          string                          `json:"project_ref"`
	Version             int64                           `json:"version"`
	State               string                          `json:"state"`
	Name                string                          `json:"name"`
	PublishedVersionRef string                          `json:"published_version_ref"`
	PublishedRevision   int64                           `json:"published_revision"`
	CoordinatorAgentRef string                          `json:"coordinator_agent_ref"`
	MaxConcurrency      int64                           `json:"max_concurrency"`
	TimeoutSeconds      int64                           `json:"timeout_seconds"`
	Steps               []assistantWorkflowMetadataStep `json:"steps"`
}

// assistantWorkflowMetadataStep describes one published step's order, dependencies,
// parallelism and human-gate options, without task instructions or capability grants.
// assistantWorkflowMetadataStep описывает order,dependencies,parallelism и human-gate
// options published step без task instructions и capability grants.
type assistantWorkflowMetadataStep struct {
	Key            string   `json:"key"`
	Position       int64    `json:"position"`
	Name           string   `json:"name"`
	AgentRef       string   `json:"agent_ref"`
	TimeoutSeconds int64    `json:"timeout_seconds"`
	Parallel       bool     `json:"parallel"`
	ParallelGroup  int64    `json:"parallel_group"`
	HumanGate      bool     `json:"human_gate"`
	GateDecisions  []string `json:"gate_decisions"`
	DependsOn      []string `json:"depends_on"`
}

// assistantWorkflowMetadataAvailable advertises named safe reads for SYSTEM or a
// PROJECT assistant with a valid project ref, independent of the current screen.
// It does not prove that a supplied workflow is currently eligible.
// assistantWorkflowMetadataAvailable публикует named safe reads для SYSTEM или PROJECT
// assistant с valid project ref независимо от экрана. Текущая eligibility
// переданного workflow этим не доказывается.
func assistantWorkflowMetadataAvailable(input runtimecontract.RunnerInput) bool {
	return input.IsAssistant() && (input.AssistantScope == runtimecontract.AssistantScopeSystem ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && validAssistantResourceRef(input.ProjectRef))
}

// castAssistantWorkflowMetadata verifies exact tenant/project/entity/version and
// digest, then requires both map-canonical bytes and typed round-trip canonical bytes
// for the closed metadata projection. It exposes published configuration only,
// not private full draft content or a running workflow revision.
// castAssistantWorkflowMetadata проверяет exact tenant/project/entity/version,digest,
// затем требует map-canonical и typed round-trip canonical bytes closed metadata
// projection. Выдаётся published configuration, не private full draft content
// и не running workflow revision.
func castAssistantWorkflowMetadata(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant workflow metadata response is invalid")
	if !assistantWorkflowMetadataAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "WORKFLOW" || !validAssistantResourceRef(request.GetEntityRef()) || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef || response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil || response.GetAgentConfiguration() != nil || response.GetAgentRuntimeConfiguration() != nil {
		return nil, invalid
	}
	c := response.GetWorkflowConfiguration()
	if c == nil || len(c.ProtoReflect().GetUnknown()) != 0 || c.GetWorkflowRef() != request.GetEntityRef() || c.GetProjectRef() != response.GetProjectRef() || c.GetVersion() < 1 || c.GetVersion() > 9007199254740991 || len(c.GetConfigurationJson()) == 0 || len(c.GetConfigurationJson()) > maximumAssistantCurrentConfigurationBytes {
		return nil, invalid
	}
	raw := c.GetConfigurationJson()
	var typed assistantWorkflowMetadataSnapshot
	if !decodeAssistantConfigurationSnapshot(raw, c.GetConfigurationSha256(), &typed) || typed.WorkflowRef != c.GetWorkflowRef() || typed.ProjectRef != c.GetProjectRef() || typed.Version != c.GetVersion() || !validAssistantWorkflowMetadata(typed) {
		return nil, invalid
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 11 {
		return nil, invalid
	}
	steps, ok := snapshot["steps"].([]any)
	if !ok {
		return nil, invalid
	}
	for _, step := range steps {
		m, ok := step.(map[string]any)
		if !ok || len(m) != 10 {
			return nil, invalid
		}
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	typedRaw, err := json.Marshal(typed)
	var typedMap map[string]any
	if err != nil || json.Unmarshal(typedRaw, &typedMap) != nil {
		return nil, invalid
	}
	typedCanonical, err := json.Marshal(typedMap)
	if err != nil || !bytes.Equal(typedCanonical, raw) {
		return nil, invalid
	}
	return map[string]any{"kind": "WORKFLOW_METADATA", "assistant_ref": input.AgentRef, "scope_kind": "PROJECT", "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "entries": []any{}, "next_offset": 0,
		"workflow_metadata": map[string]any{"workflow_ref": c.GetWorkflowRef(), "project_ref": c.GetProjectRef(), "version": c.GetVersion(), "configuration_sha256": c.GetConfigurationSha256(), "configuration": snapshot}}, nil
}

// validAssistantWorkflowMetadata distinguishes an unpublished empty metadata state
// from a bounded published graph, validates step positions, unique keys and gate
// decisions, and requires unique dependencies strictly earlier than their dependent.
// It does not infer edges or read live execution state.
// validAssistantWorkflowMetadata отличает unpublished empty metadata state от bounded
// published graph, проверяет step positions,unique keys/gate decisions и unique
// dependencies строго раньше dependent. Edges не выводятся, live execution state
// не читается.
func validAssistantWorkflowMetadata(v assistantWorkflowMetadataSnapshot) bool {
	if !slices.Contains([]string{"DRAFT", "VALID", "PUBLISHED", "ARCHIVED"}, v.State) || strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 160 || v.Steps == nil {
		return false
	}
	if v.PublishedVersionRef == "" {
		return v.PublishedRevision == 0 && v.CoordinatorAgentRef == "" && v.MaxConcurrency == 0 && v.TimeoutSeconds == 0 && len(v.Steps) == 0 && v.State != "PUBLISHED"
	}
	if !validAssistantResourceRef(v.PublishedVersionRef) || v.PublishedRevision < 1 || v.PublishedRevision > 9007199254740991 || !validAssistantResourceRef(v.CoordinatorAgentRef) || v.MaxConcurrency < 1 || v.MaxConcurrency > 100 || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 604800 || len(v.Steps) < 1 || len(v.Steps) > 200 {
		return false
	}
	keys := map[string]int{}
	for i, s := range v.Steps {
		if _, duplicate := keys[s.Key]; duplicate || strings.TrimSpace(s.Key) == "" || len(s.Key) > 96 || s.Position != int64(i+1) || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 160 || !validAssistantResourceRef(s.AgentRef) || s.TimeoutSeconds < 1 || s.TimeoutSeconds > 86400 || s.ParallelGroup < 0 || s.ParallelGroup > 50 || s.GateDecisions == nil || s.DependsOn == nil || len(s.GateDecisions) > 4 || len(s.DependsOn) > 200 {
			return false
		}
		keys[s.Key] = i
		seen := map[string]bool{}
		for _, d := range s.GateDecisions {
			if !s.HumanGate || !slices.Contains([]string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}, d) || seen[d] {
				return false
			}
			seen[d] = true
		}
		if s.HumanGate && len(s.GateDecisions) == 0 {
			return false
		}
		seen = map[string]bool{}
		for _, dependency := range s.DependsOn {
			position, ok := keys[dependency]
			if !ok || position >= i || seen[dependency] {
				return false
			}
			seen[dependency] = true
		}
	}
	return true
}
