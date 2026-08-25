package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantOperationCommandUsesClosedSpecializedRegistry(t *testing.T) {
	t.Parallel()
	project := entity.AssistantPlanOperation{Type: "CREATE_PROJECT", Summary: "Create project", Input: map[string]any{"name": "Sales", "purpose": "Qualify leads", "language": "en"}}
	result, err := assistantOperationCommand(project)
	if err != nil || result.Kind != command.CreateProject {
		t.Fatalf("map project operation: kind=%q err=%v", result.Kind, err)
	}
	project.Input["ownerID"] = "untrusted"
	if _, err := assistantOperationCommand(project); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unknown authority field must be rejected, got %v", err)
	}
	unknown := entity.AssistantPlanOperation{Type: "DELETE_PROJECT", Summary: "Delete", Input: map[string]any{"projectRef": "prj_12345678"}}
	if _, err := assistantOperationCommand(unknown); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unknown operation must be rejected, got %v", err)
	}
}

func TestAssistantOperationCommandBuildsWorkflowAndSystemAssistantRun(t *testing.T) {
	t.Parallel()
	workflow := entity.AssistantPlanOperation{Type: "CREATE_WORKFLOW", Summary: "Create workflow", Input: map[string]any{
		"projectRef": "prj_12345678", "name": "Lead qualification", "purpose": "Qualify inbound leads", "coordinatorAgentRef": "agt_12345678",
		"maxConcurrency": float64(2), "timeoutSeconds": float64(7200), "completionCriteria": "Every lead has a decision",
		"inputFields": []any{map[string]any{"label": "Company", "description": "Lead company name", "valueType": "TEXT", "required": true, "options": []any{}}},
		"steps": []any{map[string]any{"name": "Research", "purpose": "Research the lead", "agentRef": "agt_12345678", "parallel": false,
			"parallelGroup": float64(0), "timeoutSeconds": float64(3600), "expectedResult": "Lead profile", "humanGate": false,
			"gateDecisions": []any{}, "requiredCapabilityKeys": []any{}}},
	}}
	mapped, err := assistantOperationCommand(workflow)
	if err != nil || mapped.Kind != command.CreateWorkflow {
		t.Fatalf("map workflow operation: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.WorkflowInput)
	if payload.Draft == nil || len(payload.Draft.Steps) != 1 || payload.Draft.Steps[0].Key != "step-001" ||
		len(payload.Draft.Inputs) != 1 || payload.Draft.Inputs[0].Key != "field-001" {
		t.Fatalf("unexpected workflow draft: %#v", payload.Draft)
	}
	run := entity.AssistantPlanOperation{Type: "LAUNCH_RUN", Summary: "Launch", Input: map[string]any{
		"projectRef": "prj_12345678", "targetType": "AGENT", "targetRef": "agt_12345678", "title": "Qualify lead", "task": "Qualify ACME",
		"input": map[string]any{"company": "ACME"},
	}}
	mapped, err = assistantOperationCommand(run)
	if err != nil || mapped.Payload.(command.LaunchRunInput).Source != "SYSTEM_ASSISTANT" {
		t.Fatalf("assistant launch source must be server-owned: %#v err=%v", mapped, err)
	}
}

func TestAssistantOperationCommandBuildsIntegrationOperationsWithOCC(t *testing.T) {
	t.Parallel()
	create := entity.AssistantPlanOperation{Type: "CREATE_INTEGRATION_CONNECTION", Summary: "Create CRM connection", Input: map[string]any{
		"definitionKey": "crm", "name": "Primary CRM", "publicConfiguration": map[string]any{"tenant": "sales"},
	}}
	mapped, err := assistantOperationCommand(create)
	if err != nil || mapped.Kind != command.CreateConnection {
		t.Fatalf("map create connection operation: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.ConnectionInput)
	if payload.DefinitionKey != "crm" || payload.PublicConfiguration["tenant"] != "sales" {
		t.Fatalf("unexpected connection input: %#v", payload)
	}

	testConnection := entity.AssistantPlanOperation{Type: "TEST_INTEGRATION_CONNECTION", Summary: "Test CRM connection", Input: map[string]any{
		"connectionRef": "con_12345678", "expectedVersion": float64(3),
	}}
	mapped, err = assistantOperationCommand(testConnection)
	if err != nil || mapped.Kind != command.TestConnection || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 3 {
		t.Fatalf("map test connection operation with OCC: command=%#v err=%v", mapped, err)
	}

	grant := entity.AssistantPlanOperation{Type: "CHANGE_INTEGRATION_GRANT", Summary: "Grant CRM read", Input: map[string]any{
		"connectionRef": "con_12345678", "capabilityKey": "crm.read", "agentRef": "agt_12345678", "enabled": true, "expectedVersion": float64(4),
	}}
	mapped, err = assistantOperationCommand(grant)
	if err != nil || mapped.Kind != command.ChangeIntegrationGrant || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 4 {
		t.Fatalf("map integration grant operation with OCC: command=%#v err=%v", mapped, err)
	}
	delete(grant.Input, "expectedVersion")
	if _, err := assistantOperationCommand(grant); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("grant without authoritative connection version must be rejected, got %v", err)
	}
	grant.Input["expectedVersion"] = float64(4)
	grant.Input["workflowRef"] = "wfl_12345678"
	if _, err := assistantOperationCommand(grant); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("grant with competing targets must be rejected, got %v", err)
	}
}

func TestAssistantOperationCommandBuildsOwnerFriendlySchedule(t *testing.T) {
	t.Parallel()
	schedule := entity.AssistantPlanOperation{Type: "CREATE_SCHEDULE", Summary: "Schedule lead review", Input: map[string]any{
		"projectRef": "prj_12345678", "name": "Daily lead review", "targetType": "AGENT", "targetRef": "agt_12345678",
		"preset": "DAILY", "timeOfDay": "09:30", "timezone": "Europe/Saratov", "input": map[string]any{},
		"sessionPolicy": "NEW_EACH_RUN", "notificationPolicy": "CONTROL_CENTER_ONLY",
	}}
	mapped, err := assistantOperationCommand(schedule)
	if err != nil || mapped.Kind != command.CreateSchedule {
		t.Fatalf("map owner-friendly schedule operation: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.ScheduleInput)
	if payload.TimeOfDay != "09:30" || payload.CronExpression != "" {
		t.Fatalf("assistant must not synthesize a hidden cron expression: %#v", payload)
	}
}
