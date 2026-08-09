package resource

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type ownerReadbackObjects struct {
	projectID, key, digest string
}

func (*ownerReadbackObjects) Check(context.Context) error { return nil }

func (objects *ownerReadbackObjects) Put(_ context.Context, projectID, key string, content []byte, mediaType, expectedSHA256 string) (domainobjectstore.Object, error) {
	objects.projectID, objects.key, objects.digest = projectID, key, expectedSHA256
	return domainobjectstore.Object{Reference: "s3://owner-content/" + key, VersionID: "version-1",
		SHA256: expectedSHA256, Size: uint64(len(content)), MediaType: mediaType}, nil
}

type ownerReadbackRepository struct {
	domainrepo.Repository
	resources map[string]entity.Resource
}

func (repository *ownerReadbackRepository) Get(_ context.Context, organizationID, projectID, id string, kind enum.Kind) (entity.Resource, error) {
	resource, ok := repository.resources[id]
	if !ok || resource.OrganizationID != organizationID || resource.ProjectID != projectID || resource.Kind != kind {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func TestAgentRuntimeSelectionIsDerivedFromCurrentRoleAndRecipe(t *testing.T) {
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	recipeID := "44444444-4444-4444-8444-444444444444"
	roleID := "55555555-5555-4555-8555-555555555555"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	recipeSpec := entity.RoleImageRecipeSpec{Input: entity.RoleImageRecipeInput{
		BaseImageReference: "registry.example/base:1", BaseImageDigest: "sha256:" + digest,
		SourceRef: "git://repository", SourceRevision: "rev1", SourceSHA256: digest,
		ContextRef: "oci://registry.example/context@sha256:" + digest, ContextSHA256: digest,
		BuilderSHA256: digest, FrontendSHA256: digest, ToolchainSHA256: digest,
		Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
	}, Generation: 1, SpecSHA256: digest, PolicyRevision: 1, PolicySHA256: digest,
		RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: digest}
	recipe := entity.Resource{ID: recipeID, OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, Kind: enum.KindRoleImageRecipe, Name: "Runtime recipe",
		State: enum.StateActive, Version: 4, Spec: recipeSpec, CreatedAt: now, UpdatedAt: now}
	recipeSHA, err := entity.ProjectionSHA256(recipe)
	if err != nil {
		t.Fatal(err)
	}
	role := entity.Resource{ID: roleID, Name: "Разработчик", OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, Kind: enum.KindRoleDefinition, State: enum.StateActive, Version: 3,
		Spec: entity.RoleDefinitionSpec{StableKey: "developer", RoleImageRecipeID: recipeID,
			RoleImageRecipeVersion: recipe.Version, RoleImageRecipeSHA256: recipeSHA,
			Capabilities: []string{"runtime.execute"}, Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}},
		CreatedAt: now, UpdatedAt: now}
	roleSHA, err := entity.ProjectionSHA256(role)
	if err != nil {
		t.Fatal(err)
	}
	agent := entity.Resource{ID: "66666666-6666-4666-8666-666666666666", Name: "Агент",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindAgent, State: enum.StateActive, Version: 2,
		Spec: entity.AgentSpec{StableKey: "developer-agent", RoleDefinitionID: roleID,
			RoleDefinitionVersion: role.Version, RoleDefinitionSHA256: roleSHA,
			RuntimeProfileRef:     "control-plane://runtime-profile/" + recipeID,
			RuntimeProfileVersion: recipe.Version, RuntimeProfileSHA256: recipeSHA,
			BotUsername: "mattercodex-bot", BotMaskedStatus: "AVAILABLE", BotProviderGeneration: 7}}
	repository := &ownerReadbackRepository{resources: map[string]entity.Resource{roleID: role, recipeID: recipe}}
	service := &Service{repository: repository}
	principal := value.Principal{ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID}
	projection, err := service.AgentOwnerProjection(context.Background(), principal, agent)
	if err != nil || projection.RuntimeSelection.Status != OwnerProjectionPresent ||
		projection.RuntimeSelection.SelectionKey != "developer" || projection.BotIdentity.Status != "BOUND" ||
		projection.BotIdentity.Username != "mattercodex-bot" || projection.BotIdentity.ProviderGeneration != 7 {
		t.Fatalf("current selection: %#v %v", projection.RuntimeSelection, err)
	}
	foreign := principal
	foreign.ActorID = "77777777-7777-4777-8777-777777777777"
	if _, err := service.AgentOwnerProjection(context.Background(), foreign, agent); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign owner was not hidden: %v", err)
	}
	recipe.Version++
	repository.resources[recipeID] = recipe
	projection, err = service.AgentOwnerProjection(context.Background(), principal, agent)
	if err != nil || projection.RuntimeSelection.Status != OwnerProjectionStale {
		t.Fatalf("stale selection: %#v %v", projection.RuntimeSelection, err)
	}
}

func TestOwnerSchedulePresetDefaultsAndOverrides(t *testing.T) {
	basic, err := buildOwnerScheduleSpec(OwnerScheduleSelection{PresetKey: "daily", Timezone: "UTC",
		Overrides: OwnerScheduleOverrides{Present: map[string]bool{}}})
	if err != nil || len(basic.AdvancedOverrides) != 0 || basic.Cron != "0 9 * * *" {
		t.Fatalf("basic preset: %#v %v", basic, err)
	}
	selection := OwnerScheduleSelection{PresetKey: "hourly", Timezone: "UTC",
		Overrides: OwnerScheduleOverrides{Present: map[string]bool{
			"maximum_attempts": true, "notification_policy": true,
		}, MaximumAttempts: 5, NotificationPolicy: "ON_FAILURE"}}
	spec, err := buildOwnerScheduleSpec(selection)
	if err != nil {
		t.Fatalf("build schedule: %v", err)
	}
	if spec.Cron != "0 * * * *" || spec.MisfirePolicy != "RUN_ONCE" || !spec.Coalesce ||
		spec.SessionPolicy != "NEW" || spec.NotificationPolicy != "ON_FAILURE" || spec.MaximumAttempts != 5 {
		t.Fatalf("unexpected effective schedule: %#v", spec)
	}
	if spec.OwnerPresetRevision == 0 || !validSHA256Text(spec.OwnerPresetSHA256) ||
		spec.OwnerDefaultsRevision == 0 || !validSHA256Text(spec.OwnerDefaultsSHA256) {
		t.Fatal("preset/default pins are absent")
	}
	if strings.Join(spec.AdvancedOverrides, ",") != "maximum_attempts,notification_policy" {
		t.Fatalf("unexpected overrides: %v", spec.AdvancedOverrides)
	}
	if _, err := buildOwnerScheduleSpec(OwnerScheduleSelection{PresetKey: "daily", Timezone: "Unknown/Zone",
		Overrides: OwnerScheduleOverrides{Present: map[string]bool{}}}); err == nil {
		t.Fatal("unknown timezone was accepted before prompt materialization")
	}
}

func TestOwnerScheduleInlinePromptUsesContentAddressedVersionPinnedObject(t *testing.T) {
	objects := &ownerReadbackObjects{}
	service := &Service{instructionObjects: objects}
	projectID := "33333333-3333-4333-8333-333333333333"
	prompt, err := service.prepareOwnerSchedulePrompt(context.Background(), value.Principal{ProjectID: projectID},
		OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: "# Проверка\n"})
	if err != nil || prompt.Object.VersionID == "" || prompt.Object.SHA256 == "" ||
		objects.projectID != projectID || objects.key != "schedule-prompts/"+prompt.Object.SHA256+".md" ||
		objects.digest != prompt.Object.SHA256 {
		t.Fatalf("inline prompt materialization: %#v %#v %v", prompt, objects, err)
	}
}

func TestOwnerNextActionsMatrices(t *testing.T) {
	now := time.Now().UTC()
	process := entity.Resource{Kind: enum.KindProcessRun, State: enum.StateRunning}
	if got := strings.Join(runNextActions(process, entity.Resource{}, nil), ","); got != "CANCEL" {
		t.Fatalf("running actions: %s", got)
	}
	process.State = enum.StateFailed
	turn := entity.Resource{ID: "turn", State: enum.StateFailed}
	if got := strings.Join(runNextActions(process, turn, &RuntimeExecution{State: "FAILED"}), ","); got != "RETRY" {
		t.Fatalf("failed actions: %s", got)
	}
	process.State = enum.StateSucceeded
	if got := runNextActions(process, turn, &RuntimeExecution{State: "SUCCEEDED"}); len(got) != 0 {
		t.Fatalf("succeeded actions: %v", got)
	}
	if got := runtimeIncidentNextActions("OPEN", "FAILED", true); strings.Join(got, ",") != "ACKNOWLEDGE,RETRY" {
		t.Fatalf("incident actions: %v", got)
	}
	if got := runtimeIncidentNextActions("CLOSED", "FAILED", true); len(got) != 0 {
		t.Fatalf("closed incident actions: %v", got)
	}
	for _, scenario := range []struct {
		incident, execution string
		current             bool
		want                string
	}{
		{"OPEN", "RUNNING", true, "ACKNOWLEDGE"},
		{"ACKNOWLEDGED", "RUNNING", true, "RELEASE"},
		{"ACKNOWLEDGED", "FAILED", true, "RETRY,CLOSE"},
		{"ACKNOWLEDGED", "FAILED", false, "CLOSE"},
		{"RETRYING", "RUNNING", true, ""},
		{"RETRYING", "FAILED", false, "CLOSE"},
		{"RELEASED", "RUNNING", true, ""},
		{"RELEASED", "CANCELLED", true, "RETRY,CLOSE"},
		{"RELEASED", "CANCELLED", false, "CLOSE"},
	} {
		if got := strings.Join(runtimeIncidentNextActions(scenario.incident, scenario.execution, scenario.current), ","); got != scenario.want {
			t.Fatalf("incident %s/%s/current=%v: got %q want %q", scenario.incident, scenario.execution,
				scenario.current, got, scenario.want)
		}
	}
	restore := entity.Resource{State: enum.StateFailed, Spec: entity.WorkspaceRestoreSpec{
		MembershipSHA256: strings.Repeat("a", 64), RestoreState: "FAILED", Attempt: 1, Generation: 1,
	}}
	backup := entity.Resource{State: enum.StateSucceeded, Spec: entity.WorkspaceBackupSpec{
		MembershipSHA256: strings.Repeat("a", 64), BackupState: "AVAILABLE", RetainUntil: now.Add(time.Hour),
	}}
	if got := strings.Join(workspaceRestoreNextActions(restore, backup, now), ","); got != "RETRY" {
		t.Fatalf("restore actions: %s", got)
	}
	backup.Spec = entity.WorkspaceBackupSpec{MembershipSHA256: strings.Repeat("a", 64),
		BackupState: "AVAILABLE", RetainUntil: now.Add(-time.Second)}
	if got := workspaceRestoreNextActions(restore, backup, now); len(got) != 0 {
		t.Fatalf("expired backup actions: %v", got)
	}
	backup.Spec = entity.WorkspaceBackupSpec{MembershipSHA256: strings.Repeat("b", 64),
		BackupState: "AVAILABLE", RetainUntil: now.Add(time.Hour)}
	if got := workspaceRestoreNextActions(restore, backup, now); len(got) != 0 {
		t.Fatalf("stale membership actions: %v", got)
	}
	timeline := RunTimelineOwnerProjections([]domainrepo.Audit{{ID: "private-event-id", Action: "unknown",
		Outcome: "private-result-ref", ResourceVersion: 2, OccurredAt: now}}, []string{"RETRY"})
	if len(timeline) != 1 || timeline[0].EventRef == "private-event-id" || timeline[0].Outcome != "OTHER" ||
		strings.Join(timeline[0].NextActions, ",") != "RETRY" {
		t.Fatalf("safe timeline: %#v", timeline)
	}
	lineage := RunLineageResult{Processes: []domainrepo.RunGraphNode{{NodeType: "PROCESS", ID: "process",
		State: "FAILED", Version: 2}}, Attempts: []domainrepo.RunGraphNode{{NodeType: "ATTEMPT", ID: "execution",
		ProcessRunID: "process", State: "FAILED", Version: 3, Attempt: 1}}}
	lineageProjection, err := RunLineageOwnerProjections(lineage)
	if err != nil || len(lineageProjection) != 2 || lineageProjection[0].NodeRef == "process" {
		t.Fatalf("safe lineage: %#v %v", lineageProjection, err)
	}
	lineage.Attempts[0].State = "UNKNOWN"
	if _, err := RunLineageOwnerProjections(lineage); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("unknown lineage state was accepted: %v", err)
	}
}

func TestConfigurationDiffIsBoundedRedactedAndSnapshotBound(t *testing.T) {
	service := &Service{leaseSigningKey: []byte(strings.Repeat("k", 32))}
	leftSHA, rightSHA, comparison := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	leftSnapshot, rightSnapshot := strings.Repeat("d", 64), strings.Repeat("e", 64)
	page, err := service.buildConfigurationDiffPage(1, leftSHA, leftSnapshot, "name: old\ntoken: secret\nkeep",
		2, rightSHA, rightSnapshot, "name: new\ntoken: changed\nadded", comparison, "", 1)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(page.Changes) != 1 || !page.Truncated || page.NextPageToken == "" ||
		page.Changes[0].Kind != "CHANGED" || page.Changes[0].Path != "/content/lines/1" {
		t.Fatalf("unexpected first page: %#v", page)
	}
	second, err := service.buildConfigurationDiffPage(1, leftSHA, leftSnapshot, "name: old\ntoken: secret\nkeep",
		2, rightSHA, rightSnapshot, "name: new\ntoken: changed\nadded", comparison, page.NextPageToken, 1)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Changes) != 1 || second.Changes[0].Display != "REDACTED" ||
		second.Changes[0].Before != "[REDACTED]" || second.Changes[0].After != "[REDACTED]" {
		t.Fatalf("redaction failed: %#v", second)
	}
	if _, err := service.buildConfigurationDiffPage(1, leftSHA, leftSnapshot, "a", 3, rightSHA,
		rightSnapshot, "b", comparison, page.NextPageToken, 1); err == nil {
		t.Fatal("continuation was accepted for another exact version pair")
	}
	if _, err := service.buildConfigurationDiffPage(1, leftSHA, strings.Repeat("f", 64),
		"name: old\ntoken: secret\nkeep", 2, rightSHA, rightSnapshot,
		"name: new\ntoken: changed\nadded", comparison, page.NextPageToken, 1); err == nil {
		t.Fatal("continuation was accepted for another snapshot digest")
	}
}

func TestRunArtifactProjectionExcludesStorageLocator(t *testing.T) {
	items, err := RunArtifactOwnerProjections([]entity.Resource{{ID: "artifact-id", Name: "Отчёт",
		Kind: enum.KindArtifact, CreatedAt: time.Now(), Spec: entity.ArtifactSpec{
			ArtifactKind: "runtime-result", MediaType: "text/markdown", SizeBytes: 10,
			SHA256: strings.Repeat("a", 64), ScanStatus: "CLEAN", StorageRef: "s3://private/object",
		}}})
	if err != nil || len(items) != 1 {
		t.Fatalf("artifact projection: %v %#v", err, items)
	}
	if items[0].ArtifactRef == "artifact-id" || !strings.HasPrefix(items[0].ArtifactRef, "artifact:") || items[0].SHA256 == "" {
		t.Fatalf("unexpected artifact projection: %#v", items[0])
	}
}
