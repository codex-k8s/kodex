package controlplaneapi

import (
	"reflect"
	"strings"
	"testing"
)

// serverPublicationFixture composes a PR/review graph with an exact owner gate deadline for
// transport checks; it does not materialize runtime state or claim provider evidence.
// serverPublicationFixture строит граф PR/review с точным сроком Human Gate для проверок передачи;
// runtime-состояние не материализуется, доказательства провайдера не подменяются.
func serverPublicationFixture() WorkflowPublication {
	value := publicationFixture()
	value.Version, value.ExecutionMode, value.Concurrency = 2, workflowServerMode, 2
	value.Steps[0].ResultRequirements = []WorkflowPublicationRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo"}}
	ttl := int32(1800)
	value.Steps = append(value.Steps, WorkflowPublicationStep{Key: "review", Position: 2, AgentRef: "agt_reviewer01", TimeoutSeconds: 900, DependsOn: []string{"step-001"}, HumanGateAfter: true, ApprovalTimeoutSeconds: &ttl, GateDecisions: []string{"APPROVE", "REQUEST_CHANGES"}, ResultRequirements: []WorkflowPublicationRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "step-001"}}})
	return value
}

// TestWorkflowPublicationServerRoundtrip preserves exact dependencies, mandatory predicates and
// optional gate deadlines through canonical producer/consumer bytes while retaining legacy wire.
// TestWorkflowPublicationServerRoundtrip сохраняет точные зависимости, обязательные условия и
// срок Human Gate через канонические байты producer/consumer, сохраняя прежний формат.
func TestWorkflowPublicationServerRoundtrip(t *testing.T) {
	value := serverPublicationFixture()
	raw, digest, err := EncodeWorkflowPublication(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWorkflowPublication(raw, digest)
	if err != nil || !reflect.DeepEqual(value, decoded) {
		t.Fatal("server publication lost pinned data")
	}
	legacy := publicationFixture()
	legacyRaw, legacyDigest, err := EncodeWorkflowPublication(legacy)
	if err != nil || strings.Contains(string(legacyRaw), "execution_mode") || strings.Contains(string(legacyRaw), "result_requirements") {
		t.Fatal("legacy publication gained server policy")
	}
	actual, err := DecodeWorkflowPublication(legacyRaw, legacyDigest)
	if err != nil || !reflect.DeepEqual(legacy, actual) {
		t.Fatal("legacy publication changed")
	}
	changed := serverPublicationFixture()
	changed.Steps[0].ResultRequirements[0].Repository = "codex-k8s/other-qa"
	_, otherDigest, err := EncodeWorkflowPublication(changed)
	if err != nil || otherDigest == digest {
		t.Fatal("requirement change did not change commitment")
	}
}

// TestWorkflowPublicationServerRejectsIncompleteProfile closes version confusion, unbounded or
// duplicate graph data, undeclared results and cross-kind fields before any consumer uses them.
// TestWorkflowPublicationServerRejectsIncompleteProfile закрывает путаницу версий, безграничные
// либо повторные данные графа, пустые условия и поля чужого вида до использования потребителем.
func TestWorkflowPublicationServerRejectsIncompleteProfile(t *testing.T) {
	mutations := map[string]func(*WorkflowPublication){
		"legacy server mode":   func(v *WorkflowPublication) { v.Version = 1 },
		"legacy requirements":  func(v *WorkflowPublication) { v.Version, v.ExecutionMode = 1, "" },
		"unknown mode":         func(v *WorkflowPublication) { v.ExecutionMode = "SERVER_DAG_V2" },
		"missing mode":         func(v *WorkflowPublication) { v.ExecutionMode = "" },
		"unknown version":      func(v *WorkflowPublication) { v.Version = 3 },
		"concurrency":          func(v *WorkflowPublication) { v.Concurrency = 3 },
		"step budget":          func(v *WorkflowPublication) { v.Steps = append(v.Steps, make([]WorkflowPublicationStep, 11)...) },
		"dependency budget":    func(v *WorkflowPublication) { v.Steps[1].DependsOn = make([]string, 9) },
		"dependency duplicate": func(v *WorkflowPublication) { v.Steps[1].DependsOn = []string{"step-001", "step-001"} },
		"foreign dependency":   func(v *WorkflowPublication) { v.Steps[1].DependsOn = []string{"foreign"} },
		"missing requirements": func(v *WorkflowPublication) { v.Steps[0].ResultRequirements = nil },
		"duplicate requirement": func(v *WorkflowPublication) {
			v.Steps[0].ResultRequirements = append(v.Steps[0].ResultRequirements, v.Steps[0].ResultRequirements[0])
		},
		"unknown result kind": func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].Kind = "URL" },
		"foreign subject":     func(v *WorkflowPublication) { v.Steps[1].ResultRequirements[0].SubjectStepKey = "foreign" },
		"foreign field":       func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].FileName = "report.md" },
		"repository URL": func(v *WorkflowPublication) {
			v.Steps[0].ResultRequirements[0].Repository = "https://example.test/repo"
		},
		"owner target":          func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].TargetRef = "ssh_12345678" },
		"explicit delegation":   func(v *WorkflowPublication) { v.Steps[0].RequiredCapabilityKeys = []string{"platform.run.delegate"} },
		"explicit launch":       func(v *WorkflowPublication) { v.Steps[0].RequiredCapabilityKeys = []string{"platform.run.launch"} },
		"NUL requirement":       func(v *WorkflowPublication) { v.Steps[1].ResultRequirements[0].Key = "review\x00" },
		"oversize instructions": func(v *WorkflowPublication) { v.Instructions = strings.Repeat("x", 8193) },
		"lost gate":             func(v *WorkflowPublication) { v.Steps[1].HumanGateAfter = false },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := serverPublicationFixture()
			mutate(&value)
			if _, _, err := EncodeWorkflowPublication(value); err == nil {
				t.Fatal("invalid server profile accepted")
			}
		})
	}
}
