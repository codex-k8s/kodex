package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// serverVersionFixture supplies only the server-specific graph fields; generic UI/domain field
// validation and runtime admission are separate checks and are not represented by this fixture.
// serverVersionFixture задаёт только серверные поля графа; общая проверка UI/доменных полей и
// runtime-допуск являются отдельными проверками и этой fixture не представлены.
func serverVersionFixture() entity.WorkflowVersion {
	return entity.WorkflowVersion{ExecutionMode: ServerDAGV1, Concurrency: 2, Steps: []entity.WorkflowStep{
		{Key: "developer", AgentRef: "agt_developer", ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo"}}},
		{Key: "reviewer", AgentRef: "agt_reviewer", DependsOn: []string{"developer"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "developer"}}},
	}}
}

// TestServerVersionGraphAndIndependence rejects ambiguous graph identity and self-review while
// permitting the full agreed twelve-step limit. It does not prove DB authority or execution.
// TestServerVersionGraphAndIndependence отклоняет неоднозначность графа и саморевью, допуская
// согласованный предел двенадцати этапов. Полномочия БД и исполнение тест не подтверждает.
func TestServerVersionGraphAndIndependence(t *testing.T) {
	if !ValidServerVersion(serverVersionFixture()) {
		t.Fatal("valid independent graph rejected")
	}
	mutations := map[string]func(*entity.WorkflowVersion){
		"absent mode":          func(v *entity.WorkflowVersion) { v.ExecutionMode = "" },
		"empty graph":          func(v *entity.WorkflowVersion) { v.Steps = nil },
		"too much concurrency": func(v *entity.WorkflowVersion) { v.Concurrency = 3 },
		"missing contract":     func(v *entity.WorkflowVersion) { v.Steps[0].ResultRequirements = nil },
		"self review":          func(v *entity.WorkflowVersion) { v.Steps[1].AgentRef = v.Steps[0].AgentRef },
		"same reviewer twice": func(v *entity.WorkflowVersion) {
			step := v.Steps[1]
			step.Key = "second_review"
			v.Steps = append(v.Steps, step)
		},
		"duplicate step":           func(v *entity.WorkflowVersion) { v.Steps[1].Key = v.Steps[0].Key },
		"reserved coordinator key": func(v *entity.WorkflowVersion) { v.Steps[1].Key = "workflow.coordinator.exception.1" },
		"foreign dependency":       func(v *entity.WorkflowVersion) { v.Steps[1].DependsOn = []string{"foreign"} },
		"duplicate dependency":     func(v *entity.WorkflowVersion) { v.Steps[1].DependsOn = []string{"developer", "developer"} },
		"cycle":                    func(v *entity.WorkflowVersion) { v.Steps[0].DependsOn = []string{"reviewer"} },
		"delegate capability":      func(v *entity.WorkflowVersion) { v.Steps[0].RequiredCapabilityKeys = []string{"platform.run.delegate"} },
		"launch capability":        func(v *entity.WorkflowVersion) { v.Steps[0].RequiredCapabilityKeys = []string{"platform.run.launch"} },
		"text budget":              func(v *entity.WorkflowVersion) { v.Steps[0].Instructions = strings.Repeat("x", 8193) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := serverVersionFixture()
			mutate(&value)
			if ValidServerVersion(value) {
				t.Fatal("invalid server graph accepted")
			}
		})
	}
	value := serverVersionFixture()
	for index := 2; index < MaximumSteps; index++ {
		value.Steps = append(value.Steps, entity.WorkflowStep{Key: fmt.Sprintf("step-%02d", index), AgentRef: "agt_analyst", ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}})
	}
	if !ValidServerVersion(value) {
		t.Fatal("twelve-step profile rejected")
	}
	value.Steps = append(value.Steps, entity.WorkflowStep{Key: "thirteenth"})
	if ValidServerVersion(value) {
		t.Fatal("thirteenth step accepted")
	}
}
