package platform

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantOperationCommandUsesClosedSpecializedRegistry checks a valid project create
// mapping, rejection of a forged ownerID field and rejection of an unsupported delete kind using
// pure command adapters. It does not create a project or prove live authorization.
// TestAssistantOperationCommandUsesClosedSpecializedRegistry проверяет корректное сопоставление
// project create, отказ при подставленном ownerID и неподдерживаемом delete через чистые command
// adapters. Не создаёт проект и не доказывает живую authorization.
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

// TestAssistantProjectFileOperation checks text-file command metadata/hash shape and rejects path
// traversal, unsupported media and an extra secret field using independent forged input copies. No
// object storage or malware scanner is invoked.
// TestAssistantProjectFileOperation проверяет метаданные/форму hash текстового файла, отклоняет
// path traversal, неподдерживаемый media type и лишнее secret поле на отдельных копиях input. Не
// вызывает object storage и malware scanner.
func TestAssistantProjectFileOperation(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_PROJECT_FILE", Summary: "Create project notes",
		Input: map[string]any{
			"projectRef": "prj_example", "fileName": "notes.md",
			"mediaType": "text/markdown", "content": "# Notes\n",
		},
	}
	mapped, err := assistantOperationCommand(operation)
	if err != nil {
		t.Fatalf("map project file: %v", err)
	}
	payload, ok := mapped.Payload.(command.ProjectFileInput)
	if mapped.Kind != command.CreateProjectFile || !ok || payload.ProjectRef != "prj_example" ||
		payload.FileName != "notes.md" || payload.SizeBytes != int64(len("# Notes\n")) || len(payload.SHA256) != 64 {
		t.Fatalf("unexpected project file command: %#v", mapped)
	}
	for name, mutate := range map[string]func(map[string]any){
		"path":  func(input map[string]any) { input["fileName"] = "../secret" },
		"media": func(input map[string]any) { input["mediaType"] = "application/octet-stream" },
		"extra": func(input map[string]any) { input["secret"] = "forbidden" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := operation
			forged.Input = cloneAssistantFields(operation.Input)
			mutate(forged.Input)
			if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("forged project file accepted: %v", err)
			}
		})
	}
}

// TestAssistantBinaryProjectFileOperation checks exact base64-decoded bytes/size and rejects
// invalid encoding/media combinations and decoded content over1MiB. An empty binary draft remains
// accepted for editing but is not content-ready; synthetic PNG-like bytes do not prove real image
// decoding or storage.
// TestAssistantBinaryProjectFileOperation проверяет точные base64 decoded bytes/size, отклоняет
// неверные encoding/media сочетания и декодированное содержимое свыше1MiB. Пустой binary draft
// допускается к редактированию, но не content-ready; синтетические PNG-подобные байты не
// доказывают реальное декодирование изображения и storage.
func TestAssistantBinaryProjectFileOperation(t *testing.T) {
	t.Parallel()
	png := []byte("\x89PNG\r\n\x1a\ncontent")
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_PROJECT_FILE", Summary: "Create project image",
		Input: map[string]any{
			"projectRef": "prj_example", "fileName": "pixel.png", "mediaType": "image/png",
			"contentEncoding": "BASE64", "content": base64.StdEncoding.EncodeToString(png),
		},
	}
	mapped, err := assistantOperationCommand(operation)
	if err != nil {
		t.Fatalf("map binary project file: %v", err)
	}
	payload := mapped.Payload.(command.ProjectFileInput)
	if !reflect.DeepEqual(payload.Content, png) || payload.SizeBytes != int64(len(png)) {
		t.Fatalf("binary content was not decoded exactly: %#v", payload)
	}
	for name, mutate := range map[string]func(map[string]any){
		"invalid base64": func(input map[string]any) { input["content"] = "%%%" },
		"binary as utf8": func(input map[string]any) { input["contentEncoding"] = "UTF8" },
		"text as base64": func(input map[string]any) { input["mediaType"] = "text/plain" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := operation
			forged.Input = cloneAssistantFields(operation.Input)
			mutate(forged.Input)
			if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("invalid binary project file accepted: %v", err)
			}
		})
	}
	overLimit := operation
	overLimit.Input = cloneAssistantFields(operation.Input)
	overLimit.Input["content"] = base64.StdEncoding.EncodeToString(make([]byte, (1<<20)+1))
	if _, err := assistantOperationCommand(overLimit); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("oversized decoded file accepted: %v", err)
	}
	draft := operation
	draft.Input = cloneAssistantFields(operation.Input)
	draft.Input["content"] = ""
	if _, err := assistantOperationCommand(draft); err != nil || assistantProjectFileContentReady(draft) {
		t.Fatalf("empty binary draft must be accepted but remain incomplete: %v", err)
	}
}

// TestNormalizeAssistantProjectFileStoresBinaryContentOnce verifies normalization keeps inline
// binary content only in Parameters and removes it from the public After snapshot, also accepting
// an already redacted After. It does not persist or upload that content.
// TestNormalizeAssistantProjectFileStoresBinaryContentOnce проверяет сохранение inline binary
// content лишь в Parameters с удалением из публичного After при нормализации, допускает уже
// очищенный After. Не сохраняет и не загружает содержимое.
func TestNormalizeAssistantProjectFileStoresBinaryContentOnce(t *testing.T) {
	t.Parallel()
	parameters := map[string]any{
		"projectRef": "prj_example", "fileName": "pixel.png", "mediaType": "image/png",
		"contentEncoding": "BASE64", "content": "iVBORw0KGgo=",
	}
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_PROJECT_FILE", Key: "operation-file", Action: "CREATE",
		Title: "Create pixel", Summary: "Create project image",
		Target:     entity.AssistantPlanTarget{Kind: "ARTIFACT", Name: "pixel.png"},
		Parameters: cloneAssistantFields(parameters), Before: map[string]any{}, After: cloneAssistantFields(parameters),
		Selected: true, Permitted: true, ValidationProblems: []string{},
	}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatalf("normalize project file: %v", err)
	}
	if normalized.Parameters["content"] == nil || normalized.After["content"] != nil {
		t.Fatalf("binary content must be kept once: parameters=%#v after=%#v", normalized.Parameters, normalized.After)
	}
	delete(operation.After, "content")
	if _, err := normalizeAssistantOperation(operation); err != nil {
		t.Fatalf("safe project file after snapshot must be accepted: %v", err)
	}
}

// TestAssistantOperationTextLimitsCountUnicodeCharacters uses multibyte Russian text to
// check200-character titles and1000-character summaries survive normalization/command mapping,
// while a201-character title is rejected. It covers these text limits rather than all OpenAPI
// fields.
// TestAssistantOperationTextLimitsCountUnicodeCharacters использует многобайтный русский текст для
// проверки title200 и summary1000 символов через normalization/command mapping, отклоняя title201.
// Покрывает эти текстовые пределы, не все OpenAPI поля.
func TestAssistantOperationTextLimitsCountUnicodeCharacters(t *testing.T) {
	t.Parallel()

	parameters := map[string]any{"name": "Проект", "purpose": "Проверка", "language": "ru"}
	operation := entity.AssistantPlanOperation{
		Type:       "CREATE_PROJECT",
		Key:        "operation-unicode",
		Title:      strings.Repeat("я", 200),
		Summary:    strings.Repeat("ё", 1000),
		Action:     "CREATE",
		Target:     entity.AssistantPlanTarget{Kind: "PROJECT", Name: "Проект"},
		Parameters: parameters,
		Before:     map[string]any{},
		After:      cloneAssistantFields(parameters),
		Selected:   true,
	}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatalf("unicode text within OpenAPI maxLength was rejected: %v", err)
	}
	if _, err := assistantOperationCommand(normalized); err != nil {
		t.Fatalf("unicode summary within OpenAPI maxLength was rejected by command mapping: %v", err)
	}
	operation.Title = strings.Repeat("я", 201)
	if _, err := normalizeAssistantOperation(operation); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("title beyond OpenAPI maxLength was accepted: %v", err)
	}
}

// TestAssistantIntegrationDefinitionPublicationRequiresPinnedMetadata checks organization-context
// publication with exact configuration/revision/digest/version, rejects unapproved input fields
// and malformed digest, and forbids changing the pinned revision through a draft edit. It
// exercises preview/typed-command helpers without publishing a definition.
// TestAssistantIntegrationDefinitionPublicationRequiresPinnedMetadata проверяет publication в
// organization context с точными configuration/revision/digest/version, отклоняет неразрешённые
// поля и неверный digest, запрещает смену pinned revision через draft edit. Проверяет
// preview/typed-command helpers без публикации definition.
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

// TestAssistantOperationCommandBuildsHydratedProjectUpdate checks that a fully hydrated
// project-update input maps to the specialized UpdateProject payload with exact reference, fields
// and OCC version, without executing persistence.
// TestAssistantOperationCommandBuildsHydratedProjectUpdate проверяет преобразование полного
// hydrated project-update в специализированный UpdateProject payload с точными reference, полями и
// OCC version без сохранения.
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

// TestAssistantAgentUpdateRequiresExactContextAndOwnerSnapshot checks exact agent-context matching
// and the hydrate→normalize→bind→command chain, preserving unchanged owner fields/avatar/version
// while applying the requested purpose. No-op, noneditable-only and blank changes must fail; no
// database or access evaluator is used.
// TestAssistantAgentUpdateRequiresExactContextAndOwnerSnapshot проверяет точное соответствие agent
// context и цепочку hydrate→normalize→bind→command с сохранением неизменённых owner
// fields/avatar/version при новом purpose. No-op, лишь не редактируемое поле и пустое изменение
// должны отказать; БД и access evaluator не используются.
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

// TestAssistantInstructionDraftRequiresExactAgentAndSeparatePublication checks draft-only
// instruction mapping and exact agent context, then verifies rehydration restores owner
// target/version/before metadata despite forged preview fields. Foreign agents, short instructions
// and a publish field are rejected without publishing instructions.
// TestAssistantInstructionDraftRequiresExactAgentAndSeparatePublication проверяет draft-only
// сопоставление инструкций и точный agent context, затем восстановление owner
// target/version/before при подставленных preview fields. Чужой агент, короткие инструкции и поле
// publish отклоняются без публикации инструкций.
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

// TestAssistantSystemInstructionPlanPreservesOwnerBoundary checks system-instruction planning
// across screen contexts, exact original/new instructions and owner version, then verifies edited
// previews restore the system assistant target and version. This is pure envelope validation, not
// owner approval or runtime application.
// TestAssistantSystemInstructionPlanPreservesOwnerBoundary проверяет план системных инструкций
// между screen contexts, точные old/new instructions и owner version, затем восстановление
// target/version системного помощника при редактировании preview. Это чистая проверка envelope, не
// owner approval и не применение runtime.
func TestAssistantSystemInstructionPlanPreservesOwnerBoundary(t *testing.T) {
	t.Parallel()
	proposed := entity.AssistantPlanOperation{
		Type: "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", Key: "update-kodex-instructions",
		Title: "Update Kodex instructions", Summary: "Add an owner instruction after confirmation.",
		Parameters: map[string]any{
			"systemAssistantRef": "agt_system123",
			"instructions":       "Before changing an entity, summarize the expected result.",
		},
	}
	if !assistantOperationMatchesContext("PROJECT", "prj_12345678", proposed) {
		t.Fatal("system assistant instruction plan was tied to the current screen")
	}
	hydrated, err := hydrateAssistantSystemInstructionFields(entity.SystemAssistant{
		Ref: "agt_system123", Name: "Kodex", OwnerInstructions: "Keep responses concise.", Version: 9,
	}, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.UpdateAssistantInstructions || mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 9 {
		t.Fatalf("system assistant instruction plan mapped incorrectly: %#v, %v", mapped, err)
	}
	payload := mapped.Payload.(command.AssistantInstructionsInput)
	if payload.Instructions != "Before changing an entity, summarize the expected result." ||
		assistantString(hydrated.Before, "instructions") != "Keep responses concise." {
		t.Fatalf("system assistant instruction snapshots changed: %#v", hydrated)
	}
	edited := hydrated
	edited.Parameters = map[string]any{
		"systemAssistantRef": "agt_system123",
		"instructions":       "Before changing an entity, list the expected result and important risks.",
	}
	edited.Target.Ref = "agt_forged"
	edited.ExpectedVersion = nil
	updated, err := rehydrateEditedAssistantSystemInstructions(hydrated, edited)
	if err != nil || updated.Target.Ref != "agt_system123" || updated.ExpectedVersion == nil || *updated.ExpectedVersion != 9 {
		t.Fatalf("edited instruction plan escaped the owner snapshot: %#v, %v", updated, err)
	}
}

// TestAssistantSelfConfigurationOperationRequiresPinnedAssistant checks supported system/project
// self-configuration exceptions only for the pinned assistant and appropriate scope, rejecting
// foreign targets and an ordinary operation with an injected system reference. It does not
// independently authorize a command.
// TestAssistantSelfConfigurationOperationRequiresPinnedAssistant проверяет поддерживаемые
// system/project self-configuration исключения лишь для закреплённого помощника и нужной области,
// отклоняет чужие цели и обычную операцию с подставленным system ref. Самостоятельно команду не
// авторизует.
func TestAssistantSelfConfigurationOperationRequiresPinnedAssistant(t *testing.T) {
	t.Parallel()
	for _, operationType := range []string{
		"PREPARE_RUNTIME_ENVIRONMENT_REVISION",
		"UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS",
	} {
		operation := entity.AssistantPlanOperation{Type: operationType, Parameters: map[string]any{
			"systemAssistantRef": "agt_system123",
		}}
		if !assistantSelfConfigurationOperation("agt_system123", "SYSTEM", operation) ||
			assistantSelfConfigurationOperation("agt_system123", "SYSTEM", entity.AssistantPlanOperation{
				Type: operationType, Parameters: map[string]any{"systemAssistantRef": "system-assistant"},
			}) || assistantSelfConfigurationOperation("agt_other", "SYSTEM", operation) {
			t.Fatalf("self-configuration boundary changed for %s", operationType)
		}
	}
	if assistantSelfConfigurationOperation("agt_system123", "SYSTEM", entity.AssistantPlanOperation{
		Type: "UPDATE_PROJECT", Parameters: map[string]any{"systemAssistantRef": "agt_system123"},
	}) {
		t.Fatal("ordinary operation escaped the context allowlist")
	}
	for _, operationType := range []string{"CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "UPDATE_AGENT"} {
		operation := entity.AssistantPlanOperation{Type: operationType, Parameters: map[string]any{"agentRef": "agt_project123"}}
		if !assistantSelfConfigurationOperation("agt_project123", "PROJECT", operation) ||
			assistantSelfConfigurationOperation("agt_other", "PROJECT", operation) ||
			assistantSelfConfigurationOperation("agt_project123", "SYSTEM", operation) {
			t.Fatalf("project self-configuration escaped its pinned profile: %s", operationType)
		}
	}
}

// TestProjectAssistantSelfCapabilityGuard checks a supported project assistant self-capability
// proposal is classified only for its exact PROJECT assistant, rejecting other scopes and a
// foreign assistant. The classifier result does not execute the capability grant.
// TestProjectAssistantSelfCapabilityGuard проверяет классификацию поддерживаемого self-capability
// предложения лишь для точного PROJECT помощника, отклоняя другие области и чужого помощника.
// Классификация не исполняет grant capability.
func TestProjectAssistantSelfCapabilityGuard(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{Type: "CHANGE_CAPABILITY", Parameters: map[string]any{"agentRef": "agt_project123", "capabilityKey": "platform.artifact.manage", "enabled": true}}
	if !projectAssistantOperation(operation.Type) || !assistantSelfConfigurationOperation("agt_project123", "PROJECT", operation) {
		t.Fatal("owner-confirmed self capability cannot reach the project helper plan")
	}
	for _, scope := range []string{"SYSTEM", "NONE", "STAFF", "UNKNOWN"} {
		if assistantSelfConfigurationOperation("agt_project123", scope, operation) {
			t.Fatal("self capability escaped the PROJECT profile")
		}
	}
	if assistantSelfConfigurationOperation("agt_other123", "PROJECT", operation) {
		t.Fatal("foreign helper received a self-configuration exception")
	}
}

// TestProjectAssistantOperationScopeIsClosed checks representative organization-only/unknown
// operations are absent from the project assistant allowlist while supported project operations
// remain recognized. This allowlist test is separate from actor and resource authorization.
// TestProjectAssistantOperationScopeIsClosed проверяет отсутствие примеров
// organization-only/unknown операций в списке проектного помощника и распознавание поддерживаемых
// проектных операций. Проверка списка отделена от actor/resource authorization.
func TestProjectAssistantOperationScopeIsClosed(t *testing.T) {
	t.Parallel()
	for _, operationType := range []string{"CREATE_PROJECT", "CREATE_PROJECT_ASSISTANT", "CREATE_INTEGRATION_CONNECTION", "UPDATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION", "PUBLISH_INTEGRATION_DEFINITION", "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", "UNKNOWN"} {
		if projectAssistantOperation(operationType) {
			t.Fatalf("project assistant accepted organization operation: %s", operationType)
		}
	}
	for _, operationType := range []string{"CREATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "CHANGE_INTEGRATION_GRANT"} {
		if !projectAssistantOperation(operationType) {
			t.Fatalf("project assistant rejected project operation: %s", operationType)
		}
	}
}

// TestAssistantOperationCreatesProjectProfileThroughSpecializedCommand checks a normalized
// profile-create preview with an explicit project reaches CreateProjectAssistant from organization
// context and rejects binding the same proposal to another project. No profile is persisted.
// TestAssistantOperationCreatesProjectProfileThroughSpecializedCommand проверяет прохождение
// нормализованного profile-create с явным проектом в CreateProjectAssistant из organization
// context и отказ при привязке к иному проекту. Профиль не сохраняется.
func TestAssistantOperationCreatesProjectProfileThroughSpecializedCommand(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{Key: "create-project-assistant", Type: "CREATE_PROJECT_ASSISTANT",
		Title: "Create project assistant", Summary: "Prepare isolated project assistant",
		Parameters: map[string]any{"projectRef": "prj_target123", "name": "Assistant", "purpose": "Configure project", "instructions": "Use this project only."},
		Before:     map[string]any{}, After: map[string]any{"projectRef": "prj_target123", "name": "Assistant", "purpose": "Configure project", "instructions": "Use this project only."},
		Target: entity.AssistantPlanTarget{Kind: "PROJECT_ASSISTANT", Name: "Assistant"}}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, "")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(bound)
	if err != nil || mapped.Kind != command.CreateProjectAssistant || mapped.Payload.(command.ProjectAssistantInput).ProjectRef != "prj_target123" {
		t.Fatalf("project assistant draft did not use its specialized command: %#v %v", mapped, err)
	}
	if _, err := bindAssistantOperationProject(normalized, "prj_foreign123"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("profile draft escaped selected project context: %v", err)
	}
}

// TestHydrateAssistantAgentCapabilityFields checks capability-toggle preview action/target/version
// and before/after enabled values from a fixed owner snapshot, rejecting an unchanged toggle. It
// does not grant the capability.
// TestHydrateAssistantAgentCapabilityFields проверяет action/target/version preview переключения
// capability и before/after enabled из фиксированного owner snapshot, отклоняя неизменённое
// переключение. Capability не предоставляет.
func TestHydrateAssistantAgentCapabilityFields(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CHANGE_CAPABILITY", Key: "capability-1", Title: "Разрешить работу с файлами",
		Summary: "Сотрудник сможет читать и изменять файлы проекта.",
	}
	hydrated, err := hydrateAssistantAgentCapabilityFields(
		"agt_12345678", "Analyst", "platform.artifact.manage", false, true, 7, operation,
	)
	if err != nil {
		t.Fatalf("hydrate capability: %v", err)
	}
	if hydrated.Action != "UPDATE" || hydrated.Target.Ref != "agt_12345678" || hydrated.Target.Version == nil ||
		*hydrated.Target.Version != 7 || hydrated.ExpectedVersion == nil || *hydrated.ExpectedVersion != 7 ||
		hydrated.Before["enabled"] != false || hydrated.After["enabled"] != true {
		t.Fatalf("unexpected hydrated capability: %#v", hydrated)
	}
	if _, err := hydrateAssistantAgentCapabilityFields(
		"agt_12345678", "Analyst", "platform.artifact.manage", true, true, 7, operation,
	); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("no-op capability change was accepted: %v", err)
	}
}

// TestAssistantEditedAgentUpdateRehydratesAuthorityEnvelope forges editable preview metadata and
// verifies rehydration restores the original agent target/version/name/avatar while retaining the
// allowed purpose edit; changing immutable avatar or agent reference is Forbidden.
// TestAssistantEditedAgentUpdateRehydratesAuthorityEnvelope подставляет preview metadata и
// проверяет восстановление исходных agent target/version/name/avatar с сохранением допустимого
// purpose edit; изменение immutable avatar либо agent ref даёт Forbidden.
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

// TestAssistantCreateTargetUsesClosedKinds checks supported agent/environment create kinds receive
// their expected preview target/name and an unsupported delete kind receives no target. Target
// construction is not resource creation or authority.
// TestAssistantCreateTargetUsesClosedKinds проверяет ожидаемые preview target/name для
// agent/environment create и отсутствие target у неподдерживаемого delete. Построение target не
// создаёт ресурс и не даёт полномочий.
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

// TestAssistantEnvironmentDraftUsesProjectBoundSpecializedCommand exercises create
// hydration/normalization/project binding into the environment-draft command, preserving public
// values, secret descriptors, tools and policy. It rejects forged target, secret values,
// duplicate/overlapping names, unsafe tools, invalid policy/image locator and missing project
// using synthetic inputs only.
// TestAssistantEnvironmentDraftUsesProjectBoundSpecializedCommand проводит create
// hydration/normalization/project binding в environment-draft command с сохранением public values,
// secret descriptors, tools и policy. Отклоняет подставленную цель, значения секретов,
// duplicate/overlap names, unsafe tools, неверные policy/image locator и пустой проект лишь на
// синтетических inputs.
func TestAssistantEnvironmentDraftUsesProjectBoundSpecializedCommand(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CREATE_RUNTIME_ENVIRONMENT_DRAFT", Key: "environment-draft", Title: "Prepare environment", Summary: "Prepare environment draft",
		Input: map[string]any{"projectRef": "current", "name": "Developer environment", "description": "Build and test project code"},
	}
	hydrated, bound := prepareAssistantCreateForTest(t, operation, "prj_example")
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
		{"projectRef": "prj_example", "name": "Environment", "policy": map[string]any{"kubernetesAccess": "UNSUPPORTED", "networkDestinations": []any{"DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK", "ANY"}}},
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

// TestAssistantRoleImageRecipeUsesAgentSnapshotAndClosedFields checks role-image create mapping
// retains the exact agent/version/environment/Dockerfile, allows a name edit through trusted
// rehydration and forbids project/agent/version changes or secret fields. It does not build or
// admit an image.
// TestAssistantRoleImageRecipeUsesAgentSnapshotAndClosedFields проверяет сохранение точных
// agent/version/environment/Dockerfile при role-image create, допускает name edit через доверенное
// rehydration, запрещает смену project/agent/version и secret fields. Не собирает и не допускает
// image.
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

// TestAssistantRoleImageUpdateKeepsExactTargetAndClosedFields checks image-update payload/OCC and
// exact specification hash shape, allows a supported environment edit and rejects changes to
// project/recipe/spec pins or secret fields. Synthetic recipe text is not build or admission
// evidence.
// TestAssistantRoleImageUpdateKeepsExactTargetAndClosedFields проверяет image-update payload/OCC и
// точную форму specification hash, допускает разрешённый environment edit, отклоняет смену
// project/recipe/spec pins и secret fields. Синтетический recipe text не доказывает
// build/admission.
func TestAssistantRoleImageUpdateKeepsExactTargetAndClosedFields(t *testing.T) {
	t.Parallel()
	version := int64(4)
	before := map[string]any{"projectRef": "prj_example", "recipeRef": "imgrec_exact", "name": "Old image", "environmentKey": "standard", "dockerfile": "FROM example@sha256:abc\n", "specSha256": strings.Repeat("a", 64)}
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
	for _, pin := range []any{nil, "", "forged", " " + strings.Repeat("a", 64)} {
		invalid := normalized
		invalid.Input = cloneAssistantFields(normalized.Input)
		invalid.Input["specSha256"] = pin
		if _, err := assistantOperationCommand(invalid); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("image update accepted malformed specification pin: %v", err)
		}
	}
	edited := operation
	edited.Parameters = cloneAssistantFields(after)
	edited.Parameters["environmentKey"] = "documents"
	rehydrated, err := rehydrateEditedAssistantRoleImageUpdate(operation, edited)
	if err != nil || rehydrated.Target.Ref != "imgrec_exact" || assistantString(rehydrated.After, "environmentKey") != "documents" {
		t.Fatalf("image update edit lost target: operation=%#v err=%v", rehydrated, err)
	}
	for _, key := range []string{"projectRef", "recipeRef", "specSha256"} {
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

// TestAssistantCreateAgentProposesOnlyExplicitInitialCapabilities checks explicit initial
// capability preservation and rejects duplicates/unsupported kinds, then verifies template
// context/integration enrichment leaves the input unchanged. Internal
// localization/index/unknown-variable template fragments must be rejected without running a model.
// TestAssistantCreateAgentProposesOnlyExplicitInitialCapabilities проверяет сохранение явных
// initial capabilities и отказ при duplicates/unsupported kinds, затем template
// context/integration enrichment без изменения input. Internal localization/index/unknown-variable
// fragments должны отклоняться без запуска модели.
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
	for _, fragment := range []string{"{{ range .integrations.items }}", "{{ .name }}", "{{ .description }}", "{{ .capability }}", "{{ else }}"} {
		if !strings.Contains(instructions, fragment) {
			t.Fatalf("assistant template is missing dynamic integration fragment %s", fragment)
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

// TestAssistantCreateAgentHydrationReachesApprovedCommand checks pure create hydration through
// normalization/project binding into CreateAgent, retaining explicit capabilities and injected
// organization/project/agent/integration template context. The fixture reaches a typed command,
// not a real approval or persisted agent.
// TestAssistantCreateAgentHydrationReachesApprovedCommand проверяет чистую create hydration через
// normalization/project binding в CreateAgent с сохранением явных capabilities и добавленного
// organization/project/agent/integration template context. Оснастка достигает typed command, не
// реального approval и не сохранённого агента.
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
	_, bound := prepareAssistantCreateForTest(t, operation, "prj_example")
	mapped, err := assistantOperationCommand(bound)
	if err != nil {
		t.Fatal(err)
	}
	payload := mapped.Payload.(command.AgentInput)
	if payload.ProjectRef != "prj_example" || len(payload.InitialCapabilities) != 3 ||
		!strings.Contains(payload.Instructions, "{{ .organization.name }}") ||
		!strings.Contains(payload.Instructions, "{{ .project.name }}") ||
		!strings.Contains(payload.Instructions, "{{ .agent.name }}") ||
		!strings.Contains(payload.Instructions, "{{ range .integrations.items }}") {
		t.Fatalf("approved plan lost project, capabilities, or template context: project=%q capabilities=%v instructions_length=%d", payload.ProjectRef, payload.InitialCapabilities, len(payload.Instructions))
	}
}

// TestHydrateAssistantProjectOperationBuildsCompleteAuthoritativeSnapshot checks project-update
// preview target/version/selection and complete before/after snapshots with unchanged
// purpose/language, rejecting a no-op rename. It uses a supplied owner snapshot without querying
// or updating a project.
// TestHydrateAssistantProjectOperationBuildsCompleteAuthoritativeSnapshot проверяет
// target/version/selection preview проекта и полные before/after с неизменёнными purpose/language,
// отклоняя no-op rename. Использует переданный owner snapshot без чтения и изменения проекта.
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

// TestAssistantOperationProjectBindingUsesConversationAuthority checks current-project alias binds
// only to the supplied conversation project in a copied input map and rejects explicit different
// or absent authoritative projects. It preserves the original tool payload.
// TestAssistantOperationProjectBindingUsesConversationAuthority проверяет привязку current-project
// alias лишь к переданному проекту беседы в копии input, отклоняет явно иной либо отсутствующий
// авторитетный проект. Сохраняет исходный tool payload.
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

// TestAssistantOperationCommandBuildsWorkflowAndSystemAssistantRun checks a synthetic workflow
// draft gets stable step/input keys and a launch payload receives the server-owned
// SYSTEM_ASSISTANT source. It validates adapters without publishing or executing the workflow/run.
// TestAssistantOperationCommandBuildsWorkflowAndSystemAssistantRun проверяет стабильные step/input
// keys синтетического workflow draft и назначенный сервером source SYSTEM_ASSISTANT launch
// payload. Проверяет adapters без публикации и исполнения workflow/run.
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

// TestAssistantOperationConflictClassifiesLaunchReadiness checks launch-readiness conflict
// presentation has the expected runtime field, READY/UNAVAILABLE values and exact target
// reference. No live readiness probe is involved.
// TestAssistantOperationConflictClassifiesLaunchReadiness проверяет ожидаемые runtime field,
// READY/UNAVAILABLE и точную target ref в представлении launch-readiness conflict. Реальной
// проверки готовности нет.
func TestAssistantOperationConflictClassifiesLaunchReadiness(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{Key: "launch", Type: "LAUNCH_RUN",
		Target: entity.AssistantPlanTarget{Kind: "AGENT", Ref: "agt_exact"}}
	conflict, problem := assistantOperationConflict(operation)
	if problem != "operation-runtime-unavailable" || conflict.Field != "runtime" ||
		conflict.Expected != "READY" || conflict.Actual != "UNAVAILABLE" || conflict.TargetRef != "agt_exact" {
		t.Fatalf("unexpected launch conflict: problem=%q conflict=%#v", problem, conflict)
	}
}

// TestAssistantOperationCommandNormalizesNamedParallelGroups checks two steps sharing a named
// parallel group receive the same numeric group and no artificial dependency between them in the
// typed draft. It does not schedule concurrent work.
// TestAssistantOperationCommandNormalizesNamedParallelGroups проверяет одинаковую числовую группу
// и отсутствие искусственной зависимости у двух шагов с общей именованной parallel group в typed
// draft. Не планирует параллельное исполнение.
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

// TestAssistantOperationCommandBuildsIntegrationOperationsWithOCC checks specialized connection
// create/test and grant-change mapping with expected versions and owner-selected approval paths.
// It rejects malformed paths, retained scope on revoke, missing version and competing recipients,
// while allowing revocation with cleared paths; no provider call is made.
// TestAssistantOperationCommandBuildsIntegrationOperationsWithOCC проверяет специализированное
// сопоставление connection create/test и grant-change с expected versions и выбранными approval
// paths. Отклоняет неверные paths, scope при revoke, отсутствие версии и конкурирующих
// получателей, допускает revoke с очищенными paths; провайдера не вызывает.
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
		"connectionRef": "con_12345678", "capabilityKey": "crm.read", "agentRef": "agt_12345678", "enabled": true, "expectedVersion": float64(4), "approvalPolicy": "HUMAN_SCOPED",
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

// TestHydrateAssistantIntegrationGrantFieldsUsesAuthoritySnapshot checks grant hydration retains
// connection/recipient version and name in the preview, enforces exact recipient context and maps
// through normalization to the typed connection-OCC grant command with approval paths. The
// snapshot is synthetic, not a live authorization grant.
// TestHydrateAssistantIntegrationGrantFieldsUsesAuthoritySnapshot проверяет сохранение
// connection/recipient version/name в grant preview, точный recipient context и прохождение
// normalization в typed connection-OCC grant command с approval paths. Снимок синтетический, не
// живой authorization grant.
func TestHydrateAssistantIntegrationGrantFieldsUsesAuthoritySnapshot(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "CHANGE_INTEGRATION_GRANT", Key: "grant-github", Title: "Разрешить чтение GitHub",
		Summary: "Сотрудник сможет читать данные через подключение GitHub.",
	}
	snapshot := assistantIntegrationGrantSnapshot{
		connectionName: "GitHub", recipientName: "Разработчик", reason: "READY",
		connectionVersion: 7, recipientVersion: 3, enabled: false, approvalScopePaths: []string{},
		selectedApprovalPolicy: "HUMAN_SCOPED", defaultApprovalPolicy: "HUMAN_EACH_EFFECT", allowedApprovalPolicies: []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"},
	}
	hydrated, err := hydrateAssistantIntegrationGrantFields(
		"con_12345678", "github.read", "agt_12345678", "", snapshot, true,
		[]string{"/repository", "/pull_request/number"}, operation,
	)
	if err != nil {
		t.Fatalf("hydrate integration grant: %v", err)
	}
	if hydrated.Action != "UPDATE" || hydrated.Target.Kind != "INTEGRATION_CONNECTION" ||
		hydrated.Target.Ref != "con_12345678" || hydrated.Target.Name != "GitHub" ||
		hydrated.ExpectedVersion == nil || *hydrated.ExpectedVersion != 7 ||
		hydrated.Before["enabled"] != false || hydrated.After["enabled"] != true ||
		hydrated.Before["recipientVersion"] != int64(3) || hydrated.After["recipientName"] != "Разработчик" {
		t.Fatalf("unexpected hydrated integration grant: %#v", hydrated)
	}
	if !assistantOperationMatchesContext("AGENT", "agt_12345678", hydrated) ||
		assistantOperationMatchesContext("AGENT", "agt_other", hydrated) ||
		assistantOperationMatchesContext("WORKFLOW", "agt_12345678", hydrated) {
		t.Fatalf("integration grant escaped its recipient context: %#v", hydrated.Parameters)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatalf("normalize integration grant: %v", err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.ChangeIntegrationGrant || mapped.Mutation.ExpectedVersion == nil ||
		*mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("map hydrated integration grant: command=%#v err=%v", mapped, err)
	}
	payload := mapped.Payload.(command.IntegrationGrantInput)
	if payload.AgentRef != "agt_12345678" || payload.WorkflowRef != "" || !payload.Enabled ||
		len(payload.ApprovalScopePaths) != 2 {
		t.Fatalf("unexpected integration grant command payload: %#v", payload)
	}
}

// TestAssistantJSONEqualPreservesStoredPlanSemantics round-trips a synthetic plan map through
// ordinary JSON to change numeric/collection Go types, verifies semantic equality and rejects a
// changed owner version. It covers these stored-plan representations rather than universal JSON
// canonicalization.
// TestAssistantJSONEqualPreservesStoredPlanSemantics проводит синтетическую plan map через обычный
// JSON для изменения Go типов чисел/коллекций, проверяет семантическое равенство и отказ при иной
// owner version. Покрывает эти stored-plan representations, не универсальную канонизацию JSON.
func TestAssistantJSONEqualPreservesStoredPlanSemantics(t *testing.T) {
	t.Parallel()
	original := map[string]any{
		"approvalScopePaths": []string{},
		"recipientVersion":   int64(3),
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(original, stored) {
		t.Fatal("fixture must exercise JSON-decoded numeric and collection types")
	}
	if !assistantJSONEqual(original, stored) {
		t.Fatalf("semantically equal stored plan changed: original=%#v stored=%#v", original, stored)
	}
	stored["recipientVersion"] = float64(4)
	if assistantJSONEqual(original, stored) {
		t.Fatal("different authority version was accepted")
	}
}

// TestAssistantConnectionUpdateRequiresExactContextAndPreservesAuthority checks connection context
// and full hydration/command mapping preserve public fields/version without credential or
// definition mutation. Edited metadata is restored from the original envelope;
// malformed/no-op/secret changes and a definition switch are rejected without contacting the
// provider.
// TestAssistantConnectionUpdateRequiresExactContextAndPreservesAuthority проверяет connection
// context и полное hydration/command mapping с сохранением публичных полей/версии без изменения
// credential/definition. Edited metadata восстанавливается из исходного envelope;
// malformed/no-op/secret changes и смена definition отклоняются без провайдера.
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

// TestAssistantOperationCommandBuildsOwnerFriendlySchedule checks a DAILY schedule retains
// explicit time/task without synthesizing hidden cron, rejects missing automation text and
// preserves an explicitly supplied CUSTOM cron. It does not compute real occurrences.
// TestAssistantOperationCommandBuildsOwnerFriendlySchedule проверяет сохранение явных
// времени/задачи DAILY расписания без скрытой генерации cron, отказ без automation text и
// сохранение явно переданного CUSTOM cron. Не вычисляет реальные occurrences.
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

// TestAssistantScheduleUpdatePinsSnapshotAndRejectsForgedDraft checks exact schedule context and
// hydration/typed-command OCC retain target/project/DST policy. Draft edits restore authoritative
// target/version and allow a name change while rejecting immutable schedule/project/policy/prompt
// fields; no schedule is persisted.
// TestAssistantScheduleUpdatePinsSnapshotAndRejectsForgedDraft проверяет точный schedule context и
// сохранение target/project/DST policy в hydration/typed-command OCC. Draft edits восстанавливают
// target/version и допускают name change, отклоняя immutable schedule/project/policy/prompt
// fields; расписание не сохраняется.
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

// TestAssistantWorkflowUpdatePreservesGraphAndRejectsForgedDraft checks exact workflow context,
// graph/default preservation and OCC through hydration/command mapping. An edited graph retains
// existing step identity and gets a server-assigned new key/dependency; foreign/duplicate keys and
// immutable project/workflow/draft/version edits fail. It exercises pure snapshot helpers without
// publishing or executing a workflow.
// TestAssistantWorkflowUpdatePreservesGraphAndRejectsForgedDraft проверяет точный workflow
// context, сохранение graph/default и OCC через hydration/command mapping. Изменённый граф
// сохраняет identity прежнего шага и получает серверный новый key/dependency; чужие/duplicate keys
// и immutable project/workflow/draft/version edits отклоняются. Проверяет чистые snapshot helpers
// без публикации и исполнения workflow.
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

// prepareAssistantCreateForTest runs the shared pure-create fixture preparation with a nil
// database transaction: hydrate, normalize, then bind to the supplied project, failing at the
// original stage. It returns both the hydrated and bound operations so each caller retains its own
// target-forgery and typed-payload assertions. It is limited to create branches that require no
// database and does not supply real authorization.
// prepareAssistantCreateForTest выполняет общую подготовку pure-create оснастки с nil транзакцией
// БД: hydrate, normalize, затем bind к переданному проекту с отказом на исходном stage. Возвращает
// hydrated и bound operations для отдельных target-forgery и typed-payload assertions каждого
// caller. Ограничен create ветками без БД, реальную authorization не предоставляет.
func prepareAssistantCreateForTest(t *testing.T, operation entity.AssistantPlanOperation, projectRef string) (entity.AssistantPlanOperation, entity.AssistantPlanOperation) {
	t.Helper()
	hydrated, err := (&Repository{}).hydrateAssistantOperation(t.Context(), nil, scope{}, projectRef, operation)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindAssistantOperationProject(normalized, projectRef)
	if err != nil {
		t.Fatal(err)
	}
	return hydrated, bound
}
