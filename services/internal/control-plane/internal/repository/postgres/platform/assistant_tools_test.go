package platform

import (
	"errors"
	"strings"
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

func TestAssistantOperationCommandBuildsHydratedProjectUpdate(t *testing.T) {
	t.Parallel()
	version := int64(7)
	operation := entity.AssistantPlanOperation{
		Type: "UPDATE_PROJECT", Summary: "Rename project",
		Target:          entity.AssistantPlanTarget{Kind: "PROJECT", Ref: "prj_12345678", Name: "Sales", Version: &version},
		ExpectedVersion: &version,
		Input: map[string]any{
			"projectRef": "prj_12345678", "name": "Enterprise sales", "purpose": "Qualify leads", "language": "en", "expectedVersion": version,
		},
	}
	mapped, err := assistantOperationCommand(operation)
	if err != nil || mapped.Kind != command.UpdateProject || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != version {
		t.Fatalf("map project update: command=%#v err=%v", mapped, err)
	}
	payload := mapped.Payload.(command.ProjectInput)
	if payload.Ref != "prj_12345678" || payload.Name != "Enterprise sales" || payload.Purpose != "Qualify leads" || payload.Language != "en" {
		t.Fatalf("unexpected hydrated project payload: %#v", payload)
	}
}

func TestAssistantAgentUpdateRequiresExactContextAndOwnerSnapshot(t *testing.T) {
	t.Parallel()
	proposed := entity.AssistantPlanOperation{Type: "UPDATE_AGENT", Key: "update-coordinator", Title: "Update coordinator", Summary: "Update coordinator",
		Parameters: map[string]any{"agentRef": "agt_current", "purpose": "Coordinate releases"}}
	if !assistantOperationMatchesContext("AGENT", "agt_current", proposed) ||
		assistantOperationMatchesContext("AGENT", "agt_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", "agt_current", proposed) {
		t.Fatal("agent update accepted a different context")
	}
	hydrated, err := hydrateAssistantAgentFields("agt_current", "Coordinator", "Coordinate work", "Manage agents", "avatar-ref", 7, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, "prj_current")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(bound)
	if err != nil || mapped.Kind != command.UpdateAgent || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("map agent update: command=%#v err=%v", mapped, err)
	}
	payload := mapped.Payload.(command.AgentInput)
	if payload.Ref != "agt_current" || payload.Name != "Coordinator" || payload.Purpose != "Coordinate releases" ||
		payload.RoleDescription != "Manage agents" || payload.AvatarURL != "avatar-ref" ||
		assistantString(hydrated.Before, "purpose") != "Coordinate work" || assistantString(hydrated.After, "purpose") != "Coordinate releases" {
		t.Fatalf("agent snapshot lost unchanged fields or version: %#v", hydrated)
	}
	for _, invalid := range []map[string]any{
		{"agentRef": "agt_current", "name": "Coordinator"},
		{"agentRef": "agt_current", "avatarUrl": "untrusted"},
		{"agentRef": "agt_current", "name": " "},
	} {
		if _, err := hydrateAssistantAgentFields("agt_current", "Coordinator", "Coordinate work", "Manage agents", "avatar-ref", 7,
			entity.AssistantPlanOperation{Type: "UPDATE_AGENT", Parameters: invalid}); !errors.Is(err, errs.ErrInvalid) && !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("invalid or no-op agent update accepted: %v", err)
		}
	}
}

func TestAssistantEditedAgentUpdateRehydratesAuthorityEnvelope(t *testing.T) {
	t.Parallel()
	original, err := hydrateAssistantAgentFields("agt_current", "Coordinator", "Coordinate work", "Manage agents", "avatar-ref", 7,
		entity.AssistantPlanOperation{Type: "UPDATE_AGENT", Key: "update-agent", Title: "Update agent", Summary: "Update agent",
			Parameters: map[string]any{"agentRef": "agt_current", "purpose": "Coordinate releases"}, Selected: true})
	if err != nil {
		t.Fatal(err)
	}
	edited := original
	edited.Parameters = cloneAssistantFields(original.Parameters)
	edited.Parameters["purpose"] = "Coordinate delivery"
	edited.Before = map[string]any{"name": "forged"}
	edited.After = map[string]any{"purpose": "forged"}
	edited.Target.Ref = "agt_other"
	edited.ExpectedVersion = nil
	rehydrated, err := rehydrateEditedAssistantAgent(original, edited)
	if err != nil {
		t.Fatal(err)
	}
	if rehydrated.Target.Ref != "agt_current" || rehydrated.ExpectedVersion == nil || *rehydrated.ExpectedVersion != 7 ||
		assistantString(rehydrated.Before, "name") != "Coordinator" || assistantString(rehydrated.After, "purpose") != "Coordinate delivery" ||
		assistantString(rehydrated.After, "avatarUrl") != "avatar-ref" {
		t.Fatalf("user edit changed server-owned agent authority: %#v", rehydrated)
	}
	edited.Parameters["avatarUrl"] = "forged"
	if _, err := rehydrateEditedAssistantAgent(original, edited); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("user edit changed immutable avatar: %v", err)
	}
	edited.Parameters["avatarUrl"] = "avatar-ref"
	edited.Parameters["agentRef"] = "agt_other"
	if _, err := rehydrateEditedAssistantAgent(original, edited); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("user edit changed agent ref: %v", err)
	}
}

func TestAssistantCreateTargetUsesClosedKinds(t *testing.T) {
	t.Parallel()
	parameters := map[string]any{"name": "Analyst"}
	if kind, name, ok := assistantCreateTarget("CREATE_AGENT", parameters); !ok || kind != "AGENT" || name != "Analyst" {
		t.Fatalf("unexpected agent target: kind=%q name=%q ok=%v", kind, name, ok)
	}
	if _, _, ok := assistantCreateTarget("DELETE_AGENT", parameters); ok {
		t.Fatal("unknown operation received a server-owned target")
	}
	if kind, name, ok := assistantCreateTarget("CREATE_RUNTIME_ENVIRONMENT_DRAFT", parameters); !ok || kind != "RUNTIME_ENVIRONMENT_DRAFT" || name != "Analyst" {
		t.Fatalf("unexpected environment draft target: kind=%q name=%q ok=%v", kind, name, ok)
	}
}

func TestAssistantEnvironmentDraftUsesProjectBoundSpecializedCommand(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_RUNTIME_ENVIRONMENT_DRAFT", Key: "environment-draft", Title: "Prepare environment", Summary: "Prepare environment draft",
		Input: map[string]any{"projectRef": "current", "name": "Developer environment", "description": "Build and test project code"},
	}
	hydrated, err := (&Repository{}).hydrateAssistantOperation(t.Context(), nil, scope{}, "prj_example", operation)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, "prj_example")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(bound)
	if err != nil || mapped.Kind != command.CreateRuntimeEnvironmentDraft {
		t.Fatalf("map environment draft: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.RuntimeEnvironmentDraftInput)
	if payload.ProjectRef != "prj_example" || payload.Specification.Name != "Developer environment" || payload.Specification.Description != "Build and test project code" || payload.Specification.ImageArtifactRef != "" {
		t.Fatalf("unexpected environment draft payload: %#v", payload)
	}
	forged := hydrated
	forged.Target.Kind = "AGENT"
	if _, err := normalizeAssistantOperation(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("forged create target accepted: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"projectRef": "prj_example", "name": "Environment", "secretValue": "forged"},
		{"projectRef": "prj_example", "name": "Environment", "imageArtifactRef": "https://untrusted.example/image"},
		{"projectRef": "", "name": "Environment"},
	} {
		bound.Input = invalid
		if _, err := assistantOperationCommand(bound); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid environment draft accepted: %v", err)
		}
	}
}

func TestAssistantRoleImageRecipeUsesAgentSnapshotAndClosedFields(t *testing.T) {
	t.Parallel()
	parameters := map[string]any{"projectRef": "prj_example", "agentRef": "agt_example", "agentVersion": int64(3),
		"name": "Developer image", "environmentKey": "standard"}
	operation := entity.AssistantPlanOperation{Type: "CREATE_ROLE_IMAGE_RECIPE", Key: "image-1", Title: "Developer image",
		Summary: "Create image", Action: "CREATE", Target: entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Name: "Developer image"},
		Parameters: parameters, Before: map[string]any{}, After: cloneAssistantFields(parameters), Selected: true}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, "prj_example")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(bound)
	if err != nil || mapped.Kind != command.CreateAssistantRoleImageRecipe {
		t.Fatalf("map role image: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.AssistantRoleImageRecipeInput)
	if payload.AgentRef != "agt_example" || payload.AgentVersion != 3 || payload.Environment.EnvironmentKey != "standard" {
		t.Fatalf("role image lost agent snapshot: %#v", payload)
	}
	edited := operation
	edited.Parameters = cloneAssistantFields(parameters)
	edited.Parameters["name"] = "Analyst image"
	rehydrated, err := rehydrateEditedAssistantRoleImage(operation, edited)
	if err != nil || rehydrated.Target.Name != "Analyst image" || assistantString(rehydrated.After, "name") != "Analyst image" {
		t.Fatalf("role image edit lost trusted target: operation=%#v err=%v", rehydrated, err)
	}
	for _, field := range []string{"agentRef", "agentVersion", "projectRef"} {
		forged := edited
		forged.Parameters = cloneAssistantFields(edited.Parameters)
		forged.Parameters[field] = "forged"
		if _, err := rehydrateEditedAssistantRoleImage(operation, forged); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("role image edit changed %s: %v", field, err)
		}
	}
	forged := operation
	forged.Input = cloneAssistantFields(parameters)
	forged.Input["secretValue"] = "must-not-enter-plan"
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("role image accepted secret field: %v", err)
	}
}

func TestAssistantCreateAgentProposesOnlyExplicitInitialCapabilities(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"projectRef": "prj_example", "name": "Coordinator", "purpose": "Coordinate project work",
		"roleDescription": "Coordinate assigned work", "instructions": "Coordinate work using verified project context.",
		"capabilities": []any{"platform.artifact.manage", "platform.run.launch", "platform.run.delegate"},
	}
	operation := entity.AssistantPlanOperation{Type: "CREATE_AGENT", Summary: "Create coordinator", Input: input}
	commandValue, err := assistantOperationCommand(operation)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commandValue.Payload.(command.AgentInput).InitialCapabilities
	if len(capabilities) != 3 || capabilities[0] != "platform.artifact.manage" || capabilities[2] != "platform.run.delegate" {
		t.Fatalf("initial capabilities lost: %v", capabilities)
	}
	for _, invalid := range [][]any{{"platform.run.launch", "platform.run.launch"}, {"platform.project.manage"}, {"unknown"}} {
		operation.Input["capabilities"] = invalid
		if _, err := assistantOperationCommand(operation); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid capability input %v accepted: %v", invalid, err)
		}
	}
	templated := withAssistantAgentTemplateContext(input)
	instructions := assistantString(templated, "instructions")
	for _, variable := range []string{"{{ .organization.name }}", "{{ .project.name }}", "{{ .agent.name }}"} {
		if !strings.Contains(instructions, variable) {
			t.Fatalf("assistant template is missing %s", variable)
		}
	}
	if assistantString(input, "instructions") != "Coordinate work using verified project context." {
		t.Fatal("assistant template hydration mutated the original operation")
	}
	for _, invalid := range []string{
		`Name: {{ index . "i18n:SYSTEM_ASSISTANT_NAME" }}`,
		`Name: i18n:SYSTEM_ASSISTANT_NAME`,
	} {
		operation.Input["instructions"] = "Coordinate work using verified project context. " + invalid
		if _, err := assistantOperationCommand(operation); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("internal localization key accepted in employee instructions: %v", err)
		}
	}
}

func TestAssistantCreateAgentHydrationReachesApprovedCommand(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_AGENT", Key: "create-coordinator", Title: "Create coordinator", Summary: "Create coordinator",
		Input: map[string]any{
			"projectRef": "current", "name": "Coordinator", "purpose": "Coordinate project work",
			"roleDescription": "Coordinate assigned work", "instructions": "Coordinate work using verified project context.",
			"capabilities": []any{"platform.artifact.manage", "platform.run.launch", "platform.run.delegate"},
		},
	}
	hydrated, err := (&Repository{}).hydrateAssistantOperation(t.Context(), nil, scope{}, "prj_example", operation)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, "prj_example")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(bound)
	if err != nil {
		t.Fatal(err)
	}
	payload := mapped.Payload.(command.AgentInput)
	if payload.ProjectRef != "prj_example" || len(payload.InitialCapabilities) != 3 ||
		!strings.Contains(payload.Instructions, "{{ .organization.name }}") ||
		!strings.Contains(payload.Instructions, "{{ .project.name }}") ||
		!strings.Contains(payload.Instructions, "{{ .agent.name }}") {
		t.Fatalf("approved plan lost project, capabilities, or template context: project=%q capabilities=%v instructions_length=%d", payload.ProjectRef, payload.InitialCapabilities, len(payload.Instructions))
	}
}

func TestHydrateAssistantProjectOperationBuildsCompleteAuthoritativeSnapshot(t *testing.T) {
	t.Parallel()
	operation, err := hydrateAssistantProjectOperation("prj_12345678", "Sales", "Qualify leads", "en", 7,
		entity.AssistantPlanOperation{Type: "UPDATE_PROJECT", Parameters: map[string]any{"projectRef": "current", "name": "Enterprise sales"}})
	if err != nil {
		t.Fatalf("hydrate project operation: %v", err)
	}
	if operation.Action != "UPDATE" || operation.Target.Kind != "PROJECT" || operation.Target.Ref != "prj_12345678" ||
		operation.ExpectedVersion == nil || *operation.ExpectedVersion != 7 || !operation.Selected {
		t.Fatalf("project authority envelope is incomplete: %#v", operation)
	}
	if assistantString(operation.Before, "name") != "Sales" || assistantString(operation.After, "name") != "Enterprise sales" ||
		assistantString(operation.After, "purpose") != "Qualify leads" || assistantString(operation.Parameters, "language") != "en" {
		t.Fatalf("project before/after snapshot is incomplete: before=%#v after=%#v parameters=%#v", operation.Before, operation.After, operation.Parameters)
	}
	if _, err := hydrateAssistantProjectOperation("prj_12345678", "Sales", "Qualify leads", "en", 7,
		entity.AssistantPlanOperation{Type: "UPDATE_PROJECT", Parameters: map[string]any{"name": "Sales"}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("no-op project update must be rejected, got %v", err)
	}
}

func TestAssistantOperationProjectBindingUsesConversationAuthority(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{Type: "CREATE_AGENT", Summary: "Create analyst", Input: map[string]any{
		"projectRef": "current", "name": "Analyst", "purpose": "Analyze leads",
		"roleDescription": "Sales analyst", "instructions": "Analyze facts and mark assumptions.",
	}}
	bound, err := bindAssistantOperationProject(operation, "prj_authoritative")
	if err != nil || assistantString(bound.Input, "projectRef") != "prj_authoritative" {
		t.Fatalf("bind current project: operation=%#v err=%v", bound, err)
	}
	if assistantString(operation.Input, "projectRef") != "current" {
		t.Fatal("binding must not mutate the runtime tool payload")
	}
	operation.Input["projectRef"] = "prj_other"
	if _, err := bindAssistantOperationProject(operation, "prj_authoritative"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("cross-project operation must be rejected, got %v", err)
	}
	operation.Input["projectRef"] = "current"
	if _, err := bindAssistantOperationProject(operation, ""); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("project-scoped operation without conversation project must be rejected, got %v", err)
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

func TestAssistantOperationCommandNormalizesNamedParallelGroups(t *testing.T) {
	t.Parallel()
	workflow := entity.AssistantPlanOperation{Type: "CREATE_WORKFLOW", Summary: "Create workflow", Input: map[string]any{
		"projectRef": "prj_12345678", "name": "Lead qualification", "purpose": "Qualify inbound leads", "coordinatorAgentRef": "agt_coord001",
		"maxConcurrency": float64(2), "timeoutSeconds": float64(7200),
		"steps": []any{
			map[string]any{"name": "Research", "purpose": "Research the lead", "agentRef": "agt_analyst1", "parallel": true,
				"parallelGroup": "lead-qualification", "timeoutSeconds": float64(3600), "expectedResult": "Lead profile", "humanGate": false,
				"gateDecisions": []any{}, "requiredCapabilityKeys": []any{}},
			map[string]any{"name": "Draft", "purpose": "Draft the offer", "agentRef": "agt_writer01", "parallel": true,
				"parallelGroup": "lead-qualification", "timeoutSeconds": float64(3600), "expectedResult": "Offer", "humanGate": true,
				"gateDecisions": []any{"APPROVE", "REJECT", "REQUEST_CHANGES"}, "requiredCapabilityKeys": []any{}},
		},
	}}
	mapped, err := assistantOperationCommand(workflow)
	if err != nil {
		t.Fatalf("map workflow with named parallel group: %v", err)
	}
	draft := mapped.Payload.(command.WorkflowInput).Draft
	if draft.Steps[0].ParallelGroup != 1 || draft.Steps[1].ParallelGroup != 1 || len(draft.Steps[0].DependsOn) != 0 || len(draft.Steps[1].DependsOn) != 0 {
		t.Fatalf("named parallel group was not normalized consistently: %#v", draft.Steps)
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
		"automationText": "Review today's leads and report changes",
		"sessionPolicy":  "NEW_EACH_RUN", "notificationPolicy": "CONTROL_CENTER_ONLY",
	}}
	mapped, err := assistantOperationCommand(schedule)
	if err != nil || mapped.Kind != command.CreateSchedule {
		t.Fatalf("map owner-friendly schedule operation: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.ScheduleInput)
	if payload.TimeOfDay != "09:30" || payload.CronExpression != "" || payload.AutomationText != "Review today's leads and report changes" {
		t.Fatalf("assistant must not synthesize a hidden cron expression: %#v", payload)
	}
	delete(schedule.Input, "automationText")
	if _, err := assistantOperationCommand(schedule); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("schedule without task must be rejected, got %v", err)
	}
	schedule.Input["automationText"] = "Review today's leads and report changes"
	schedule.Input["preset"] = "CUSTOM"
	schedule.Input["cronExpression"] = "0 9 * * 1-5"
	mapped, err = assistantOperationCommand(schedule)
	if err != nil || mapped.Payload.(command.ScheduleInput).CronExpression != "0 9 * * 1-5" {
		t.Fatalf("custom cron expression must be preserved: %#v err=%v", mapped, err)
	}
}
