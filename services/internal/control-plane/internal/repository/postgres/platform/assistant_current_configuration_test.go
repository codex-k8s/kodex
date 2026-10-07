package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantCurrentConfigurationProjectsOnlySafeOwnMetadata(t *testing.T) {
	const private = "PRIVATE_DESCRIPTOR_SENTINEL"
	view := entity.AgentRuntimeConfigurationView{AgentVersion: 7, Configuration: entity.AgentRuntimeConfiguration{Ref: "cfg_current123", Version: 3},
		Environment: entity.RuntimeEnvironmentSet{Ref: "env_current123", Version: 4, CurrentVersion: entity.RuntimeEnvironmentVersion{
			Values: []entity.RuntimeEnvironmentValue{{Name: "PUBLIC_SETTING", Value: "plain-public-value"}},
			SecretDescriptors: []entity.RuntimeSecretDescriptor{{Name: "CREDENTIAL", SecretRef: "sec_owned123", Revision: 2,
				Namespace: private, SecretName: private, SecretKey: private, SecretUID: private, SecretResourceVersion: private, ContentSHA256: private}},
		}}}
	snapshot := entity.PromptMaterializationSnapshot{TemplateRef: "tpl_current123", TemplateDigest: strings.Repeat("a", 64), TemplateContent: "{{ .agent.name }}",
		Variables: map[string]string{"agent.name": "Assistant", "task": private}, StructuredVariables: map[string]any{},
		UnavailableVariables: map[string]string{"task": "RUNTIME_CONTEXT_REQUIRED"}}
	result := projectAssistantCurrentConfiguration(view, snapshot)
	raw, err := json.Marshal(result)
	if err != nil || strings.Contains(string(raw), private) || len(result.Environment.SecretDescriptors) != 0 ||
		len(result.SecretBindings) != 1 || result.SecretBindings[0].SecretRef != "sec_owned123" || result.SecretBindings[0].Revision != 2 ||
		result.Environment.Values[0].Value != "plain-public-value" || result.AgentVersion != 7 || result.InstructionTemplateRef != snapshot.TemplateRef {
		t.Fatal("current configuration disclosed private descriptors or lost public versioned settings")
	}
	if len(view.Environment.CurrentVersion.SecretDescriptors) != 1 {
		t.Fatal("projection mutated the authoritative source")
	}
	var name, task, files bool
	for _, variable := range result.TemplateVariables {
		switch variable.Name {
		case "agent.name":
			name = variable.Available && variable.Reason == "AVAILABLE"
		case "task":
			task = !variable.Available && variable.Reason == "RUNTIME_CONTEXT_REQUIRED"
		case "input.files":
			files = !variable.Available
		}
	}
	if !name || !task || !files {
		t.Fatal("template catalog did not preserve fresh context availability")
	}
}
