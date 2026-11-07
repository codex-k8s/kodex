package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalLaunchTargetsKeepsPublishedStepScope prevents coordinator or unrelated agent grants
// from becoming requirements and verifies the input snapshot cannot be mutated through its result.
// TestPersonalLaunchTargetsKeepsPublishedStepScope исключает coordinator и unrelated agent grants
// из requirements и проверяет невозможность изменить исходный snapshot через результат функции.
func TestPersonalLaunchTargetsKeepsPublishedStepScope(t *testing.T) {
	version := entity.WorkflowVersion{CoordinatorAgentRef: "agent_coordinator", Steps: []entity.WorkflowStep{
		{AgentRef: "agent_read", RequiredCapabilityKeys: []string{"gitlab.project.metadata.read"}},
		{AgentRef: "agent_without_requirements"},
		{AgentRef: "agent_write", RequiredCapabilityKeys: []string{"jira.issue.create"}},
	}}
	targets := personalLaunchTargets([]string{"agent_coordinator", "agent_read", "agent_without_requirements", "agent_write"}, &version)
	expected := []personalLaunchTarget{{AgentRef: "agent_read", Capabilities: []string{"gitlab.project.metadata.read"}}, {AgentRef: "agent_write", Capabilities: []string{"jira.issue.create"}}}
	if !reflect.DeepEqual(targets, expected) {
		t.Fatal("workflow personal requirements escaped published step capabilities")
	}
	targets[0].Capabilities[0] = "forged"
	if version.Steps[0].RequiredCapabilityKeys[0] != "gitlab.project.metadata.read" {
		t.Fatal("requirement result mutated the published workflow snapshot")
	}
	direct := personalLaunchTargets([]string{"agent_direct"}, nil)
	if len(direct) != 1 || !direct[0].All || direct[0].AgentRef != "agent_direct" {
		t.Fatal("direct-agent requirements lost explicitly assigned personal grants")
	}
}
