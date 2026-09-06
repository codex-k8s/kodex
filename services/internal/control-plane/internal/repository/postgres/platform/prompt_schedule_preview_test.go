package platform

import (
	"strings"
	"testing"

	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAutomationVariablesRequireRealRevisionAndRenderOnce(t *testing.T) {
	snapshot := entity.PromptMaterializationSnapshot{TargetKind: "AGENT", TargetRef: "agt_fixture", ProjectRef: "prj_fixture", TemplateRef: "ins_fixture", TemplateDigest: strings.Repeat("a", 64), ServiceTemplateRevision: promptservice.ServiceTemplateRevision, Locale: "en"}
	applyAutomationPromptVariables(&snapshot, "", "Draft", "Literal {{.user.name}}", "2026-09-07T12:00:00+00:00", "UTC", "")
	if snapshot.Variables["automation.scheduled_at"] != "2026-09-07T12:00:00Z" {
		t.Fatal("runtime and preview timestamp formatting diverged")
	}
	result, err := promptservice.Materialize("{{.automation.name}} {{.automation.task}} {{.automation.scheduled_at}} {{.automation.timezone}}", promptservice.FromSnapshot(snapshot))
	if err != nil || !result.Complete || !strings.Contains(result.Prompt, "Literal {{.user.name}}") {
		t.Fatalf("Automation interpolation: complete=%v err=%v", result.Complete, err)
	}
	unavailable, err := promptservice.Materialize("{{.automation.revision}}", promptservice.FromSnapshot(snapshot))
	if err != nil || unavailable.Complete || len(unavailable.Diagnostics) == 0 || unavailable.Diagnostics[0].Code != "REVISION_NOT_SAVED" {
		t.Fatalf("invented draft revision: complete=%v err=%v", unavailable.Complete, err)
	}
	applyAutomationPromptVariables(&snapshot, "sch_fixture", "Saved", "Task", "2026-09-07T12:00:00Z", "UTC", "srev_fixture")
	available, err := promptservice.Materialize("{{.automation.revision}}", promptservice.FromSnapshot(snapshot))
	if err != nil || !available.Complete || !strings.Contains(available.Prompt, "srev_fixture") {
		t.Fatalf("saved revision unavailable: %v", err)
	}
}
