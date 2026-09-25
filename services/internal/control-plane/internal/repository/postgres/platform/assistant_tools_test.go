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

func TestAssistantIntegrationDefinitionPublicationRequiresPinnedMetadata(t *testing.T) {
	t.Parallel()
	version := int64(4)
	digest := strings.Repeat("a", 64)
	operation := entity.AssistantPlanOperation{
		Type: "PUBLISH_INTEGRATION_DEFINITION", Key: "publish-definition", Title: "Publish definition", Summary: "Publish validated definition",
		Target:          entity.AssistantPlanTarget{Kind: "INTEGRATION_DEFINITION", Ref: "mcfg_test", Name: "Tickets", Version: &version},
		ExpectedVersion: &version,
		Parameters:      map[string]any{"configurationRef": "mcfg_test", "revisionRef": "mrev_test", "revisionDigest": digest},
		Before:          map[string]any{"currentRevisionRef": "", "revisionState": "VALID", "revisionDigest": digest},
		After:           map[string]any{"currentRevisionRef": "mrev_test", "revisionState": "PUBLISHED", "revisionDigest": digest},
		Selected:        true,
	}
	if !assistantOperationMatchesContext("", "", operation) || assistantOperationMatchesContext("PROJECT", "prj_test", operation) {
		t.Fatal("publication was permitted outside organization context")
	}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.PublishIntegrationDefinition || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != version {
		t.Fatalf("publication did not map to specialized command: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.ManagedConfigurationInput)
	if payload.ConfigurationRef != "mcfg_test" || payload.RevisionRef != "mrev_test" || payload.Content != "" {
		t.Fatalf("publication command carried unapproved content: %#v", payload)
	}
	for _, key := range []string{"source", "secretValue", "ownerID"} {
		forged := normalized
		forged.Input = cloneAssistantFields(normalized.Input)
		forged.Input[key] = "untrusted"
		if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("publication accepted %s: %v", key, err)
		}
	}
	forged := normalized
	forged.Input = cloneAssistantFields(normalized.Input)
	forged.Input["revisionDigest"] = strings.Repeat("z", 64)
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("publication accepted invalid digest: %v", err)
	}
	edited := normalized
	edited.Parameters = cloneAssistantFields(normalized.Parameters)
	edited.Parameters["revisionRef"] = "mrev_other"
	if _, err := rehydrateEditedAssistantIntegrationDefinitionPublication(operation, edited); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("draft edit switched revision: %v", err)
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

func TestAssistantInstructionDraftRequiresExactAgentAndSeparatePublication(t *testing.T) {
	t.Parallel()
	proposed := entity.AssistantPlanOperation{Type: "CREATE_INSTRUCTION_DRAFT", Key: "draft-agent-instructions",
		Title: "Prepare instructions", Summary: "Prepare an unpublished instruction draft",
		Parameters: map[string]any{"agentRef": "agt_current", "instructions": "Coordinate the project and report verified progress."}}
	if !assistantOperationMatchesContext("AGENT", "agt_current", proposed) ||
		assistantOperationMatchesContext("AGENT", "agt_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", "agt_current", proposed) {
		t.Fatal("instruction draft accepted a different context")
	}
	hydrated, err := hydrateAssistantInstructionDraftFields("agt_current", "Coordinator", 7, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.CreateInstructions || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("instruction plan did not map to draft-only command: %#v, %v", mapped, err)
	}
	edited := hydrated
	edited.Parameters = map[string]any{"agentRef": "agt_current", "instructions": "Coordinate delivery and report only verified project progress."}
	edited.Target.Ref = "agt_forged"
	edited.ExpectedVersion = nil
	edited.Before = map[string]any{"name": "forged"}
	rehydrated, err := rehydrateEditedAssistantInstructionDraft(hydrated, edited)
	if err != nil || rehydrated.Target.Ref != "agt_current" || rehydrated.ExpectedVersion == nil || *rehydrated.ExpectedVersion != 7 ||
		assistantString(rehydrated.Before, "name") != "Coordinator" {
		t.Fatalf("instruction edit did not restore owner snapshot: %#v, %v", rehydrated, err)
	}
	for _, invalid := range []map[string]any{
		{"agentRef": "agt_other", "instructions": "Coordinate delivery and report verified progress."},
		{"agentRef": "agt_current", "instructions": "short"},
		{"agentRef": "agt_current", "instructions": "Coordinate delivery and report progress.", "publish": true},
	} {
		edited.Parameters = invalid
		if _, err := rehydrateEditedAssistantInstructionDraft(hydrated, edited); err == nil {
			t.Fatalf("invalid instruction edit accepted: %#v", invalid)
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
	bound.Input = map[string]any{
		"projectRef": "prj_example", "name": "Developer environment",
		"publicValues":      []any{map[string]any{"name": "PUBLIC_ENDPOINT", "value": "https://example.test"}},
		"secretBindings":    []any{map[string]any{"name": "SERVICE_AUTH", "secretRef": "sec_example1", "revision": float64(2)}},
		"secretSuggestions": []any{map[string]any{"name": "SERVICE_AUTH", "description": "Service credential", "valueType": "STRING", "sourceHelp": "Create a token in the provider dashboard."}},
		"tools":             []any{map[string]any{"name": "Git", "command": "git", "description": "Manage source files", "usageHint": "Use the verified command"}},
		"policy":            assistantTestEnvironmentPolicy(),
	}
	withFields, err := assistantOperationCommand(bound)
	if err != nil {
		t.Fatalf("map environment fields: %v", err)
	}
	fields := withFields.Payload.(command.RuntimeEnvironmentDraftInput).Specification
	if len(fields.Values) != 1 || fields.Values[0].Name != "PUBLIC_ENDPOINT" ||
		fields.Values[0].Value != "https://example.test" || len(fields.SecretBindings) != 1 ||
		fields.SecretBindings[0].Name != "SERVICE_AUTH" || fields.SecretBindings[0].Revision != 2 ||
		len(fields.Tools) != 1 || fields.Tools[0].Command != "git" || fields.Policy.Resources.CPURequestMilli != 1000 {
		t.Fatalf("environment fields lost: %#v", fields)
	}
	forged := hydrated
	forged.Target.Kind = "AGENT"
	if _, err := normalizeAssistantOperation(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("forged create target accepted: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"projectRef": "prj_example", "name": "Environment", "secretValue": "forged"},
		{"projectRef": "prj_example", "name": "Environment", "publicValues": []any{map[string]any{"name": "API_TOKEN", "value": "forged"}}},
		{"projectRef": "prj_example", "name": "Environment", "publicValues": []any{map[string]any{"name": "VISIBLE", "value": "safe", "secretValue": "forged"}}},
		{"projectRef": "prj_example", "name": "Environment", "secretBindings": []any{map[string]any{"name": "SERVICE_AUTH", "secretRef": "sec_example1", "value": "forged"}}},
		{"projectRef": "prj_example", "name": "Environment", "secretSuggestions": []any{map[string]any{"name": "SERVICE_AUTH", "valueType": "STRING", "sourceHelp": "Provider dashboard", "value": "forged"}}},
		{"projectRef": "prj_example", "name": "Environment", "secretSuggestions": []any{map[string]any{"name": "SERVICE_AUTH", "valueType": "STRING", "sourceHelp": "Provider dashboard"}, map[string]any{"name": "SERVICE_AUTH", "valueType": "JSON", "sourceHelp": "Provider dashboard"}}},
		{"projectRef": "prj_example", "name": "Environment", "secretSuggestions": []any{map[string]any{"name": "SERVICE_AUTH", "valueType": "FILE", "sourceHelp": "Provider dashboard"}}},
		{"projectRef": "prj_example", "name": "Environment", "tools": []any{map[string]any{"name": "Shell", "command": "sh;rm", "description": "Unsafe command"}}},
		{"projectRef": "prj_example", "name": "Environment", "tools": []any{map[string]any{"name": "Git", "command": "git", "description": "First"}, map[string]any{"name": "Git again", "command": "git", "description": "Second"}}},
		{"projectRef": "prj_example", "name": "Environment", "policy": map[string]any{"kubernetesAccess": "READ_OWN_EXECUTION", "networkDestinations": []any{"DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK", "ANY"}}},
		{"projectRef": "prj_example", "name": "Environment", "publicValues": []any{map[string]any{"name": "DUPLICATE", "value": "safe"}}, "secretBindings": []any{map[string]any{"name": "DUPLICATE", "secretRef": "sec_example1"}}},
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
		"name": "Developer image", "environmentKey": "standard", "dockerfile": "FROM example@sha256:abc\n"}
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
	if payload.AgentRef != "agt_example" || payload.AgentVersion != 3 || payload.Environment.EnvironmentKey != "standard" || payload.Environment.Dockerfile != "FROM example@sha256:abc\n" {
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

func TestAssistantRoleImageUpdateKeepsExactTargetAndClosedFields(t *testing.T) {
	t.Parallel()
	version := int64(4)
	before := map[string]any{"projectRef": "prj_example", "recipeRef": "imgrec_exact", "name": "Old image", "environmentKey": "standard", "dockerfile": "FROM example@sha256:abc\n"}
	after := cloneAssistantFields(before)
	after["name"] = "New image"
	operation := entity.AssistantPlanOperation{Type: "UPDATE_ROLE_IMAGE_RECIPE", Key: "image-update", Title: "New image",
		Summary: "Update image", Action: "UPDATE", Target: entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Ref: "imgrec_exact", Name: "Old image", Version: &version},
		ExpectedVersion: &version, Parameters: after, Before: before, After: cloneAssistantFields(after), Selected: true}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.UpdateAssistantRoleImageRecipe {
		t.Fatalf("map image update: kind=%q err=%v", mapped.Kind, err)
	}
	payload := mapped.Payload.(command.AssistantRoleImageUpdateInput)
	if payload.ProjectRef != "prj_example" || payload.RecipeRef != "imgrec_exact" || payload.Name != "New image" || payload.Environment.Dockerfile != "FROM example@sha256:abc\n" || *mapped.Mutation.ExpectedVersion != version {
		t.Fatalf("image update lost trusted target: %#v", payload)
	}
	edited := operation
	edited.Parameters = cloneAssistantFields(after)
	edited.Parameters["environmentKey"] = "documents"
	rehydrated, err := rehydrateEditedAssistantRoleImageUpdate(operation, edited)
	if err != nil || rehydrated.Target.Ref != "imgrec_exact" || assistantString(rehydrated.After, "environmentKey") != "documents" {
		t.Fatalf("image update edit lost target: operation=%#v err=%v", rehydrated, err)
	}
	for _, key := range []string{"projectRef", "recipeRef"} {
		forged := edited
		forged.Parameters = cloneAssistantFields(edited.Parameters)
		forged.Parameters[key] = "forged"
		if _, err := rehydrateEditedAssistantRoleImageUpdate(operation, forged); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("image update changed %s: %v", key, err)
		}
	}
	forged := operation
	forged.Input = cloneAssistantFields(after)
	forged.Input["secretValue"] = "must-not-enter-plan"
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("image update accepted secret field: %v", err)
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
		`Product: {{ .Kodex }}`,
		`Project: {{ .Marketplace }}`,
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
	grant.Input["approvalScopePaths"] = []any{"/action", "/ticket/id"}
	mapped, err = assistantOperationCommand(grant)
	if err != nil || len(mapped.Payload.(command.IntegrationGrantInput).ApprovalScopePaths) != 2 {
		t.Fatalf("map owner-selected approval scope: command=%#v err=%v", mapped, err)
	}
	for _, invalid := range []any{[]any{"/action", 3}, []any{""}, []any{1, 2}} {
		grant.Input["approvalScopePaths"] = invalid
		if _, err := assistantOperationCommand(grant); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid approval scope accepted: %v", invalid)
		}
	}
	grant.Input["approvalScopePaths"] = []any{"/action"}
	grant.Input["enabled"] = false
	if _, err := assistantOperationCommand(grant); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("revoked grant retained approval scope")
	}
	grant.Input["approvalScopePaths"] = []any{}
	if _, err := assistantOperationCommand(grant); err != nil {
		t.Fatalf("revocation with cleared approval scope was rejected: %v", err)
	}
	grant.Input["enabled"] = true
	delete(grant.Input, "approvalScopePaths")
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

func TestAssistantConnectionUpdateRequiresExactContextAndPreservesAuthority(t *testing.T) {
	t.Parallel()
	connection := entity.IntegrationConnection{Ref: "con_current", DefinitionKey: "github", Name: "Source", Version: 5,
		PublicConfiguration: map[string]any{"owner": "team", "repository": "app"}}
	proposed := entity.AssistantPlanOperation{Type: "UPDATE_INTEGRATION_CONNECTION", Key: "update-connection",
		Title: "Update connection", Summary: "Update connection", Selected: true,
		Parameters: map[string]any{"connectionRef": connection.Ref, "name": "Source code"}}
	if !assistantOperationMatchesContext("INTEGRATION_CONNECTION", connection.Ref, proposed) ||
		assistantOperationMatchesContext("INTEGRATION_CONNECTION", "con_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", connection.Ref, proposed) {
		t.Fatal("connection update accepted a different context")
	}
	hydrated, err := hydrateAssistantConnectionFields(connection, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.UpdateConnection || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 5 {
		t.Fatalf("connection update command invalid: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.ConnectionInput)
	if payload.Ref != connection.Ref || payload.Name != "Source code" || payload.PublicConfiguration["repository"] != "app" ||
		payload.CredentialRevision != nil || payload.DefinitionKey != "" {
		t.Fatalf("connection update changed immutable or secret fields: %#v", payload)
	}
	edited := normalized
	edited.Parameters = cloneAssistantFields(normalized.Parameters)
	edited.Parameters["name"] = "Production source"
	edited.Before = map[string]any{"name": "forged"}
	edited.After = map[string]any{"name": "forged"}
	edited.Target.Ref = "con_other"
	edited.ExpectedVersion = nil
	rehydrated, err := rehydrateEditedAssistantConnection(normalized, edited)
	if err != nil || rehydrated.Target.Ref != connection.Ref || *rehydrated.ExpectedVersion != 5 ||
		assistantString(rehydrated.Before, "name") != "Source" || assistantString(rehydrated.After, "name") != "Production source" {
		t.Fatalf("draft edit lost authoritative envelope: %#v %v", rehydrated, err)
	}
	for _, invalid := range []map[string]any{
		{"connectionRef": connection.Ref, "name": "Source"},
		{"connectionRef": connection.Ref, "name": " "},
		{"connectionRef": connection.Ref, "credential": "secret"},
		{"connectionRef": connection.Ref, "publicConfiguration": map[string]any{"repository": 1}},
	} {
		if _, err := hydrateAssistantConnectionFields(connection, entity.AssistantPlanOperation{Type: proposed.Type, Parameters: invalid}); err == nil {
			t.Fatalf("invalid connection change accepted: %#v", invalid)
		}
	}
	edited.Parameters["definitionKey"] = "gitlab"
	if _, err := rehydrateEditedAssistantConnection(normalized, edited); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("definition switch accepted: %v", err)
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

func TestAssistantScheduleUpdatePinsSnapshotAndRejectsForgedDraft(t *testing.T) {
	t.Parallel()
	before := map[string]any{
		"scheduleRef": "sch_12345678", "projectRef": "prj_12345678", "name": "Daily review",
		"targetType": "AGENT", "targetRef": "agt_12345678", "preset": "DAILY",
		"cronExpression": "30 9 * * *", "timeOfDay": "09:30", "dayOfWeek": "",
		"timezone": "Europe/Saratov", "input": map[string]any{}, "automationText": "Review the work",
		"sessionPolicy": "NEW_EACH_RUN", "notificationPolicy": "CONTROL_CENTER_ONLY",
		"dstGapPolicy": "SHIFT_FORWARD", "dstFoldPolicy": "RUN_ONCE_EARLIEST", "misfirePolicy": "COALESCE",
		"overlapPolicy": "FORBID", "promptInputs": map[string]any{},
	}
	proposed := entity.AssistantPlanOperation{Type: "UPDATE_SCHEDULE", Key: "schedule-update",
		Title: "Update schedule", Summary: "Update schedule", Parameters: map[string]any{
			"scheduleRef": "sch_12345678", "name": "Weekly review", "preset": "WEEKLY", "dayOfWeek": "MONDAY",
		}}
	if !assistantOperationMatchesContext("SCHEDULE", "sch_12345678", proposed) ||
		assistantOperationMatchesContext("SCHEDULE", "sch_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", "sch_12345678", proposed) {
		t.Fatal("schedule update accepted a different context")
	}
	hydrated, err := hydrateAssistantScheduleFields(before, 7, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.UpdateSchedule || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("schedule update command invalid: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.ScheduleInput)
	if payload.Ref != "sch_12345678" || payload.ProjectRef != "prj_12345678" || payload.Name != "Weekly review" ||
		payload.Preset != "WEEKLY" || payload.DSTFoldPolicy != "RUN_ONCE_EARLIEST" || payload.Target.Ref != "agt_12345678" {
		t.Fatalf("schedule update lost the preserved snapshot: %#v", payload)
	}
	edited := normalized
	edited.Parameters = cloneAssistantFields(normalized.Parameters)
	edited.Parameters["name"] = "Team review"
	edited.Before = map[string]any{"name": "forged"}
	edited.After = map[string]any{"name": "forged"}
	edited.Target.Ref = "sch_other"
	edited.ExpectedVersion = nil
	rehydrated, err := rehydrateEditedAssistantSchedule(normalized, edited)
	if err != nil || rehydrated.Target.Ref != "sch_12345678" || *rehydrated.ExpectedVersion != 7 ||
		assistantString(rehydrated.After, "name") != "Team review" {
		t.Fatalf("schedule draft edit lost authoritative envelope: %#v %v", rehydrated, err)
	}
	for _, key := range []string{"scheduleRef", "projectRef", "dstGapPolicy", "promptInputs"} {
		forged := edited
		forged.Parameters = cloneAssistantFields(normalized.Parameters)
		forged.Parameters[key] = "other"
		if _, err := rehydrateEditedAssistantSchedule(normalized, forged); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("schedule field %s was mutable: %v", key, err)
		}
	}
}

func TestAssistantWorkflowUpdatePreservesGraphAndRejectsForgedDraft(t *testing.T) {
	t.Parallel()
	draft := entity.WorkflowVersion{Name: "Weekly report", Purpose: "Summarize work", CoordinatorAgentRef: "agt_12345678",
		VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{},
		Inputs: []entity.WorkflowInputField{{Key: "field-001", Label: "Topic", Type: "TEXT", DefaultValue: "weekly"}},
		Steps: []entity.WorkflowStep{{Key: "step-1", Position: 1, Name: "Collect", AgentRef: "agt_12345678",
			Instructions: "Collect completed work.", ExpectedResult: "Summary", TimeoutSeconds: 900}}}
	fields, steps := assistantWorkflowGraphFields(draft)
	before := map[string]any{
		"workflowRef": "wfl_12345678", "projectRef": "prj_12345678", "name": draft.Name, "purpose": draft.Purpose,
		"coordinatorAgentRef": draft.CoordinatorAgentRef, "inputFields": fields, "steps": steps,
		"instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria,
		"maxConcurrency": float64(draft.Concurrency), "timeoutSeconds": float64(draft.TimeoutSeconds), "draft": draft,
	}
	proposed := entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "workflow-update", Title: "Update workflow", Summary: "Update workflow",
		Parameters: map[string]any{"workflowRef": "wfl_12345678", "name": "Monthly report", "instructions": "Use verified sources."},
		Input:      map[string]any{"steps": []any{}}}
	if !assistantOperationMatchesContext("WORKFLOW", "wfl_12345678", proposed) ||
		assistantOperationMatchesContext("WORKFLOW", "wfl_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", "wfl_12345678", proposed) {
		t.Fatal("workflow update accepted a different context")
	}
	hydrated, err := hydrateAssistantWorkflowFields(before, 7, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := assistantUpdateWorkflow(normalized); err != nil {
		t.Fatalf("workflow payload invalid: %v", err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.UpdateWorkflow || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("workflow update command invalid: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.WorkflowInput)
	if payload.Ref != "wfl_12345678" || payload.ProjectRef != "prj_12345678" || payload.Name != "Monthly report" ||
		payload.Draft == nil || len(payload.Draft.Steps) != 1 || payload.Draft.Steps[0].Key != "step-1" ||
		payload.Draft.CoordinatorAgentRef != "agt_12345678" || payload.Draft.Inputs[0].DefaultValue != "weekly" {
		t.Fatalf("workflow update changed protected draft graph: %#v", payload)
	}
	graphEdit := normalized
	graphEdit.Parameters = cloneAssistantFields(normalized.Parameters)
	updatedSteps := []any{cloneAssistantFields(steps[0].(map[string]any))}
	updatedSteps[0].(map[string]any)["purpose"] = "Collect verified work."
	updatedSteps = append(updatedSteps, map[string]any{"name": "Summarize", "purpose": "Summarize work.",
		"agentRef": "agt_12345678", "parallel": false, "parallelGroup": float64(0),
		"timeoutSeconds": float64(900), "expectedResult": "Report", "humanGate": false,
		"gateDecisions": []any{}, "requiredCapabilityKeys": []any{}})
	graphEdit.Parameters["steps"] = updatedSteps
	graphEdit, err = rehydrateEditedAssistantWorkflow(normalized, graphEdit)
	if err != nil {
		t.Fatalf("workflow graph edit rejected: %v", err)
	}
	graphEdit, err = normalizeAssistantOperation(graphEdit)
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := assistantUpdateWorkflow(graphEdit)
	if err != nil || updated.Draft == nil || len(updated.Draft.Steps) != 2 ||
		updated.Draft.Steps[0].Key != "step-1" || updated.Draft.Steps[1].Key == "step-1" ||
		len(updated.Draft.Steps[1].DependsOn) != 1 || updated.Draft.Steps[1].DependsOn[0] != "step-1" ||
		updated.Draft.Inputs[0].DefaultValue != "weekly" {
		t.Fatalf("workflow graph identity or dependency changed unexpectedly: %#v %v", updated, err)
	}
	for _, invalidKey := range []string{"step-foreign", "step-1"} {
		forgedGraph := normalized
		forgedGraph.Parameters = cloneAssistantFields(normalized.Parameters)
		forgedSteps := []any{cloneAssistantFields(steps[0].(map[string]any)), cloneAssistantFields(updatedSteps[1].(map[string]any))}
		forgedSteps[1].(map[string]any)["key"] = invalidKey
		forgedGraph.Parameters["steps"] = forgedSteps
		if _, err := rehydrateEditedAssistantWorkflow(normalized, forgedGraph); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("workflow graph accepted foreign or duplicate key %s: %v", invalidKey, err)
		}
	}
	edited := normalized
	edited.Parameters = cloneAssistantFields(normalized.Parameters)
	edited.Parameters["purpose"] = "Monthly team summary"
	edited.Before = map[string]any{"draft": "forged"}
	edited.Target.Ref = "wfl_other"
	edited.ExpectedVersion = nil
	rehydrated, err := rehydrateEditedAssistantWorkflow(normalized, edited)
	if err != nil || rehydrated.Target.Ref != "wfl_12345678" || *rehydrated.ExpectedVersion != 7 ||
		assistantString(rehydrated.After, "purpose") != "Monthly team summary" {
		t.Fatalf("workflow edit lost authoritative envelope: %#v %v", rehydrated, err)
	}
	for _, key := range []string{"workflowRef", "projectRef", "draft", "expectedVersion"} {
		forged := edited
		forged.Parameters = cloneAssistantFields(normalized.Parameters)
		forged.Parameters[key] = "other"
		if _, err := rehydrateEditedAssistantWorkflow(normalized, forged); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("workflow field %s was mutable: %v", key, err)
		}
	}
}
