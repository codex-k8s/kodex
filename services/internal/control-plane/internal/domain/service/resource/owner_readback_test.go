package resource

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func ownerTestInstructionSpec(stableKey, artifactID string) entity.InstructionSetSpec {
	content := "# Безопасная инструкция\n"
	digest := hashString(content)
	return entity.InstructionSetSpec{StableKey: stableKey, Locale: "ru", CurrentVersion: 1,
		PublishedVersion: 1, Content: content, ContentSHA256: digest, VersionState: "PUBLISHED",
		ValidationSHA256: strings.Repeat("a", 64), ValidationSucceeded: true,
		ValidatedContentVersion: 1, ValidatedContentSHA256: digest,
		ContentArtifactID: artifactID, ContentArtifactVersion: 1,
		Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}
}

func ownerTestProviderPoolSpec(stableKey string, now time.Time) entity.ProviderPoolSpec {
	return entity.ProviderPoolSpec{StableKey: stableKey, Policy: "least_used", PolicyRevision: 1,
		ObservationMaxAge: time.Hour, EligibilitySnapshotSHA256: strings.Repeat("b", 64),
		Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}, Bindings: []entity.ProviderPoolBinding{{
			ProviderConnectionReferenceID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			ProviderConnectionStableKey:   "provider-main", ReferenceVersion: 1,
			ReferenceSHA256: strings.Repeat("c", 64), Weight: 1, Eligible: true, MaskedStatus: "AVAILABLE",
			ObservedLimit: 100, ObservationRevision: 1, ObservedAt: now,
			ObservationExpiresAt: now.Add(time.Hour), ObservationSHA256: strings.Repeat("d", 64),
			WindowDurationSeconds: 3600, ResetsAt: now.Add(time.Hour),
		}}}
}

func ownerTestAgentSpec(stableKey string) entity.AgentSpec {
	return entity.AgentSpec{StableKey: stableKey,
		RoleDefinitionID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", RoleDefinitionVersion: 1,
		RoleDefinitionSHA256: strings.Repeat("a", 64),
		InstructionSetID:     "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", InstructionSetVersion: 1,
		InstructionSetSHA256: strings.Repeat("b", 64),
		ProviderPoolID:       "cccccccc-cccc-4ccc-8ccc-cccccccccccc", ProviderPoolVersion: 1,
		ProviderPoolSHA256: strings.Repeat("c", 64), RuntimeProfileRef: "control-plane://runtime-profile/default",
		OwnerRoleSelector: "developer", OwnerInstructionSelector: "developer-instructions",
		OwnerProviderPoolSelector: "primary-pool",
		RuntimeProfileVersion:     1, RuntimeProfileSHA256: strings.Repeat("d", 64),
		Capabilities: []string{"runtime.execute"}, Enabled: true,
		Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}
}

func ownerPromptFixtureResources(
	t *testing.T,
	actorID, organizationID, projectID, scheduleID string,
) map[string]entity.Resource {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	project := entity.Resource{ID: projectID, Name: "Рабочее пространство", OrganizationID: organizationID,
		ProjectID: projectID, OwnerActorID: actorID, Kind: enum.KindProject, State: enum.StateActive, Version: 3,
		Spec: entity.ProjectSpec{Slug: "workspace", Locale: "ru",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}, CreatedAt: now, UpdatedAt: now}
	projectSHA, err := entity.ProjectionSHA256(project)
	if err != nil {
		t.Fatal(err)
	}
	recipe := entity.Resource{ID: "44444444-4444-4444-8444-444444444444", Name: "Runtime recipe",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindRoleImageRecipe, State: enum.StateActive, Version: 4,
		Spec: entity.RoleImageRecipeSpec{Input: entity.RoleImageRecipeInput{
			BaseImageReference: "registry.example/base:1", BaseImageDigest: "sha256:" + digest,
			SourceRef: "git://repository", SourceRevision: "rev1", SourceSHA256: digest,
			ContextRef: "oci://registry.example/context@sha256:" + digest, ContextSHA256: digest,
			BuilderSHA256: digest, FrontendSHA256: digest, ToolchainSHA256: digest,
			Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
		}, Generation: 1, SpecSHA256: digest, PolicyRevision: 1, PolicySHA256: digest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: digest}, CreatedAt: now, UpdatedAt: now}
	recipeSHA, err := entity.ProjectionSHA256(recipe)
	if err != nil {
		t.Fatal(err)
	}
	instruction := entity.Resource{ID: "55555555-5555-4555-8555-555555555555", Name: "Инструкции",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindInstructionSet, State: enum.StateActive, Version: 5,
		Spec:      ownerTestInstructionSpec("developer-instructions", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		CreatedAt: now, UpdatedAt: now}
	instructionSHA, err := entity.ProjectionSHA256(instruction)
	if err != nil {
		t.Fatal(err)
	}
	pool := entity.Resource{ID: "66666666-6666-4666-8666-666666666666", Name: "Основной пул",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindProviderPool, State: enum.StateActive, Version: 6,
		Spec: ownerTestProviderPoolSpec("primary-pool", now), CreatedAt: now, UpdatedAt: now}
	poolSHA, err := entity.ProjectionSHA256(pool)
	if err != nil {
		t.Fatal(err)
	}
	agent := entity.Resource{ID: "77777777-7777-4777-8777-777777777777", Name: "Агент",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindAgent, State: enum.StateActive, Version: 7,
		Spec: entity.AgentSpec{StableKey: "developer-agent",
			RoleDefinitionID: "88888888-8888-4888-8888-888888888888", RoleDefinitionVersion: 1,
			RoleDefinitionSHA256: digest, InstructionSetID: instruction.ID, InstructionSetVersion: instruction.Version,
			InstructionSetSHA256: instructionSHA, ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version,
			ProviderPoolSHA256: poolSHA, RuntimeProfileRef: "control-plane://runtime-profile/" + recipe.ID,
			RuntimeProfileVersion: recipe.Version, RuntimeProfileSHA256: recipeSHA,
			Capabilities: []string{"runtime.execute"}, Enabled: true,
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}, CreatedAt: now, UpdatedAt: now}
	agentSHA, err := entity.ProjectionSHA256(agent)
	if err != nil {
		t.Fatal(err)
	}
	assignment := entity.Resource{ID: "99999999-9999-4999-8999-999999999999", Name: "Назначение",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindAgentAssignment, State: enum.StateActive, Version: 8,
		Spec: entity.AgentAssignmentSpec{AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
			WorkspaceID: projectID, WorkspaceVersion: project.Version, WorkspaceSHA256: projectSHA,
			RootActorID: actorID, AssignmentGeneration: 1}, CreatedAt: now, UpdatedAt: now}
	resources := map[string]entity.Resource{project.ID: project, recipe.ID: recipe, instruction.ID: instruction,
		pool.ID: pool, agent.ID: agent, assignment.ID: assignment}
	if scheduleID != "" {
		resources[scheduleID] = entity.Resource{ID: scheduleID, Name: "Расписание", OrganizationID: organizationID,
			ProjectID: projectID, OwnerActorID: actorID, Kind: enum.KindSchedule, State: enum.StateActive,
			Version: 9, Spec: entity.ScheduleSpec{SessionPolicy: "NEW"}, CreatedAt: now, UpdatedAt: now}
	}
	return resources
}

type ownerReadbackObjects struct {
	projectID, key, digest string
	puts                   int
	mu                     sync.Mutex
	getRaw                 []byte
	putEntered             chan struct{}
	putRelease             <-chan struct{}
}

func (*ownerReadbackObjects) Check(context.Context) error { return nil }

func (objects *ownerReadbackObjects) Get(context.Context, domainobjectstore.Object) ([]byte, error) {
	objects.mu.Lock()
	defer objects.mu.Unlock()
	if len(objects.getRaw) == 0 {
		return nil, errs.ErrNotFound
	}
	return append([]byte(nil), objects.getRaw...), nil
}

func (objects *ownerReadbackObjects) Put(_ context.Context, projectID, key string, content []byte, mediaType, expectedSHA256 string) (domainobjectstore.Object, error) {
	objects.mu.Lock()
	defer objects.mu.Unlock()
	objects.projectID, objects.key, objects.digest = projectID, key, expectedSHA256
	objects.puts++
	entered, release := objects.putEntered, objects.putRelease
	objects.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	objects.mu.Lock()
	return domainobjectstore.Object{Reference: "s3://owner-content/" + key, VersionID: "version-1",
		SHA256: expectedSHA256, Size: uint64(len(content)), MediaType: mediaType}, nil
}

type ownerPromptRepository struct {
	domainrepo.Repository
	mu          sync.Mutex
	preparation domainrepo.SchedulePromptPreparation
	resources   map[string]entity.Resource
}

type ownerPromptTransaction struct {
	domainrepo.Transaction
	domainrepo.ProtectedTransaction
	repository *ownerPromptRepository
}

func (repository *ownerPromptRepository) Transact(
	_ context.Context,
	_ domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return callback(&ownerPromptTransaction{repository: repository})
}

func (*ownerPromptTransaction) CurrentTime(context.Context) (time.Time, error) {
	return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC), nil
}

func (tx *ownerPromptTransaction) GetForUpdate(_ context.Context, organizationID, projectID, id string) (entity.Resource, error) {
	resource, ok := tx.repository.resources[id]
	if !ok || resource.OrganizationID != organizationID || resource.ProjectID != projectID {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func (tx *ownerPromptTransaction) ListSnapshotResources(_ context.Context, organizationID, projectID string) ([]entity.Resource, error) {
	result := make([]entity.Resource, 0, len(tx.repository.resources))
	for _, resource := range tx.repository.resources {
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID {
			result = append(result, resource)
		}
	}
	return result, nil
}

func (*ownerPromptTransaction) HasOpenScheduleOccurrence(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (tx *ownerPromptTransaction) GetByStableKeyForUpdate(
	_ context.Context,
	organizationID, projectID string,
	kind enum.Kind,
	stableKey string,
) (entity.Resource, error) {
	for _, resource := range tx.repository.resources {
		if resource.OrganizationID != organizationID || resource.ProjectID != projectID || resource.Kind != kind {
			continue
		}
		key := ""
		switch spec := resource.Spec.(type) {
		case entity.AgentSpec:
			key = spec.StableKey
		case entity.InstructionSetSpec:
			key = spec.StableKey
		case entity.ProviderPoolSpec:
			key = spec.StableKey
		case entity.ChatSpec:
			key = spec.StableKey
		}
		if key == stableKey {
			return resource, nil
		}
	}
	return entity.Resource{}, errs.ErrNotFound
}

func (tx *ownerPromptTransaction) GetSchedulePromptPreparation(
	context.Context,
	string,
) (domainrepo.SchedulePromptPreparation, error) {
	if tx.repository.preparation.KeyHash == "" {
		return domainrepo.SchedulePromptPreparation{}, errs.ErrNotFound
	}
	return tx.repository.preparation, nil
}

func (tx *ownerPromptTransaction) ReserveSchedulePromptPreparation(
	_ context.Context,
	requested domainrepo.SchedulePromptPreparation,
) (domainrepo.SchedulePromptPreparation, bool, error) {
	current := tx.repository.preparation
	if current.KeyHash == "" {
		tx.repository.preparation = requested
		return requested, true, nil
	}
	if current.State == "READY" || current.State == "CONSUMED" {
		return current, false, nil
	}
	if current.State == "WRITING" && current.LeaseExpiresAt.After(requested.CreatedAt) {
		return current, false, errs.ErrUnavailable
	}
	current.State = "WRITING"
	current.Generation++
	current.LeaseExpiresAt = requested.LeaseExpiresAt
	tx.repository.preparation = current
	return current, true, nil
}

func (tx *ownerPromptTransaction) CompleteSchedulePromptPreparation(
	_ context.Context,
	preparation domainrepo.SchedulePromptPreparation,
) error {
	preparation.State = "READY"
	tx.repository.preparation = preparation
	return nil
}

func (tx *ownerPromptTransaction) MarkSchedulePromptPreparationAmbiguous(
	_ context.Context,
	preparation domainrepo.SchedulePromptPreparation,
) error {
	preparation.State = "AMBIGUOUS"
	tx.repository.preparation = preparation
	return nil
}

func (*ownerPromptTransaction) ConsumeSchedulePromptPreparation(
	context.Context, string, string, uint64, string, uint64, time.Time,
) error {
	return nil
}

type ownerReadbackRepository struct {
	domainrepo.Repository
	resources map[string]entity.Resource
}

type ownerReadbackTransaction struct {
	domainrepo.Transaction
	resources map[string]entity.Resource
}

type runActionTransaction struct {
	domainrepo.Transaction
	activeChildren bool
	candidates     []entity.Resource
	attempt        domainrepo.TurnAttempt
	attemptErr     error
	lease          domainrepo.TurnLease
	leaseErr       error
}

type incidentOwnerReadRepository struct {
	domainrepo.Repository
	tx *incidentOwnerReadTransaction
}

type incidentOwnerReadTransaction struct {
	domainrepo.Transaction
	domainrepo.ProtectedTransaction
	domainrepo.OwnerReadTransaction
	incident  domainrepo.RuntimeIncident
	execution domainrepo.RuntimeExecution
	process   entity.Resource
	project   entity.Resource
	history   []domainrepo.RuntimeIncidentHistory
}

type snapshotFenceTransaction struct {
	domainrepo.Transaction
	domainrepo.OwnerReadTransaction
	fence string
}

func (tx *snapshotFenceTransaction) OwnerSnapshotFence(context.Context) (string, error) {
	return tx.fence, nil
}

func (repository *incidentOwnerReadRepository) Transact(
	_ context.Context,
	_ domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	return callback(repository.tx)
}

func (tx *incidentOwnerReadTransaction) GetRuntimeIncidentForUpdate(context.Context, string) (domainrepo.RuntimeIncident, error) {
	return tx.incident, nil
}

func (tx *incidentOwnerReadTransaction) GetRuntimeExecutionForUpdate(context.Context, string) (domainrepo.RuntimeExecution, error) {
	return tx.execution, nil
}

func (tx *incidentOwnerReadTransaction) GetForUpdate(_ context.Context, _, _, id string) (entity.Resource, error) {
	if id == tx.process.ID {
		return tx.process, nil
	}
	if id == tx.project.ID {
		return tx.project, nil
	}
	return entity.Resource{}, errs.ErrNotFound
}

func (tx *incidentOwnerReadTransaction) ListRuntimeIncidentHistorySnapshot(
	context.Context, string, uint64, int,
) ([]domainrepo.RuntimeIncidentHistory, error) {
	return append([]domainrepo.RuntimeIncidentHistory(nil), tx.history...), nil
}

func (tx *runActionTransaction) HasActiveChildProcesses(context.Context, string, string, string) (bool, error) {
	return tx.activeChildren, nil
}

func (tx *runActionTransaction) ActiveProcessTurnCandidates(context.Context, string, string, string) ([]entity.Resource, error) {
	return tx.candidates, nil
}

func (tx *runActionTransaction) GetTurnAttemptForUpdate(context.Context, string, uint32) (domainrepo.TurnAttempt, error) {
	return tx.attempt, tx.attemptErr
}

func (tx *runActionTransaction) GetTurnLeaseForUpdate(context.Context, string) (domainrepo.TurnLease, error) {
	return tx.lease, tx.leaseErr
}

func (tx *ownerReadbackTransaction) Get(_ context.Context, organizationID, projectID, id string) (entity.Resource, error) {
	resource, ok := tx.resources[id]
	if !ok || resource.OrganizationID != organizationID || resource.ProjectID != projectID {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func (repository *ownerReadbackRepository) Transact(
	_ context.Context,
	_ domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	return callback(&ownerReadbackTransaction{resources: repository.resources})
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
	instruction := entity.Resource{ID: "88888888-8888-4888-8888-888888888888", Name: "Инструкции",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindInstructionSet, State: enum.StateActive, Version: 5,
		Spec:      ownerTestInstructionSpec("developer-instructions", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		CreatedAt: now, UpdatedAt: now}
	instructionSHA, err := entity.ProjectionSHA256(instruction)
	if err != nil {
		t.Fatal(err)
	}
	pool := entity.Resource{ID: "99999999-9999-4999-8999-999999999999", Name: "Основной пул",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindProviderPool, State: enum.StateActive, Version: 6,
		Spec: ownerTestProviderPoolSpec("primary-pool", now), CreatedAt: now, UpdatedAt: now}
	poolSHA, err := entity.ProjectionSHA256(pool)
	if err != nil {
		t.Fatal(err)
	}
	agent := entity.Resource{ID: "66666666-6666-4666-8666-666666666666", Name: "Агент",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindAgent, State: enum.StateActive, Version: 2,
		Spec: entity.AgentSpec{StableKey: "developer-agent", RoleDefinitionID: roleID,
			RoleDefinitionVersion: role.Version, RoleDefinitionSHA256: roleSHA,
			InstructionSetID: instruction.ID, InstructionSetVersion: instruction.Version,
			InstructionSetSHA256: instructionSHA, ProviderPoolID: pool.ID,
			ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
			OwnerRoleSelector: "developer", OwnerInstructionSelector: "developer-instructions",
			OwnerProviderPoolSelector: "primary-pool",
			RuntimeProfileRef:         "control-plane://runtime-profile/" + recipeID,
			RuntimeProfileVersion:     recipe.Version, RuntimeProfileSHA256: recipeSHA,
			BotUsername: "mattercodex-bot", BotMaskedStatus: "AVAILABLE", BotProviderGeneration: 7}}
	repository := &ownerReadbackRepository{resources: map[string]entity.Resource{
		agent.ID: agent, roleID: role, recipeID: recipe, instruction.ID: instruction, pool.ID: pool,
	}}
	service := &Service{repository: repository}
	principal := value.Principal{ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID}
	projection, err := service.AgentOwnerProjection(context.Background(), principal, agent)
	if err != nil || projection.RuntimeSelection.Status != OwnerProjectionPresent ||
		projection.RuntimeSelection.SelectionKey != "developer" || projection.BotIdentity.Status != "BOUND" ||
		projection.BotIdentity.Username != "mattercodex-bot" || projection.BotIdentity.ProviderGeneration != 7 ||
		projection.InstructionSelection.StableSelector != "developer-instructions" ||
		projection.InstructionSelection.Status != OwnerProjectionPresent ||
		projection.ProviderPoolSelection.StableSelector != "primary-pool" ||
		projection.ProviderPoolSelection.Status != OwnerProjectionPresent {
		t.Fatalf("current selection: %#v %v", projection.RuntimeSelection, err)
	}
	foreign := principal
	foreign.ActorID = "77777777-7777-4777-8777-777777777777"
	if _, err := service.AgentOwnerProjection(context.Background(), foreign, agent); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign owner was not hidden: %v", err)
	}
	delete(repository.resources, roleID)
	delete(repository.resources, instruction.ID)
	delete(repository.resources, pool.ID)
	missing, err := service.AgentOwnerProjection(context.Background(), principal, agent)
	if err != nil || missing.RuntimeSelection.SelectionKey != "developer" ||
		missing.InstructionSelection.StableSelector != "developer-instructions" ||
		missing.ProviderPoolSelection.StableSelector != "primary-pool" {
		t.Fatalf("stable selections were lost with hidden dependencies: %#v %v", missing, err)
	}
	repository.resources[roleID], repository.resources[instruction.ID], repository.resources[pool.ID] = role, instruction, pool
	recipe.Version++
	repository.resources[recipeID] = recipe
	projection, err = service.AgentOwnerProjection(context.Background(), principal, agent)
	if err != nil || projection.RuntimeSelection.Status != OwnerProjectionStale {
		t.Fatalf("stale selection: %#v %v", projection.RuntimeSelection, err)
	}
	newerAgent := agent
	newerAgent.Version++
	repository.resources[agent.ID] = newerAgent
	if _, err := service.AgentOwnerProjection(context.Background(), principal, agent); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("mixed Agent snapshot was accepted: %v", err)
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

func TestOwnerScheduleProjectionRoundTripsStableSelectionsAndPrompt(t *testing.T) {
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	resourceWithDigest := func(id, name string, kind enum.Kind, version uint64, spec entity.Spec) (entity.Resource, string) {
		item := entity.Resource{ID: id, Name: name, OrganizationID: organizationID, ProjectID: projectID,
			OwnerActorID: actorID, Kind: kind, State: enum.StateActive, Version: version, Spec: spec,
			CreatedAt: now, UpdatedAt: now}
		digest, err := entity.ProjectionSHA256(item)
		if err != nil {
			t.Fatal(err)
		}
		return item, digest
	}
	agent, agentSHA := resourceWithDigest("44444444-4444-4444-8444-444444444444", "Агент", enum.KindAgent, 2,
		ownerTestAgentSpec("developer-agent"))
	instruction, instructionSHA := resourceWithDigest("55555555-5555-4555-8555-555555555555", "Инструкции",
		enum.KindInstructionSet, 3, ownerTestInstructionSpec("developer-instructions", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	pool, poolSHA := resourceWithDigest("66666666-6666-4666-8666-666666666666", "Пул",
		enum.KindProviderPool, 4, ownerTestProviderPoolSpec("primary-pool", now))
	room, roomSHA := resourceWithDigest("77777777-7777-4777-8777-777777777777", "Комната",
		enum.KindChat, 5, entity.ChatSpec{StableKey: "owner-room", RoomType: "USER",
			ExternalChannelRef: "mattermost://channel/owner-room", Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}})
	artifact := entity.Resource{ID: "88888888-8888-4888-8888-888888888888", Name: "Безопасный prompt",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindArtifact, State: enum.StateActive, Version: 6, CreatedAt: now, UpdatedAt: now,
		Spec: entity.ArtifactSpec{Direction: "INPUT", MediaType: "text/markdown",
			SHA256: strings.Repeat("f", 64), ScanStatus: "CLEAN", SizeBytes: 12,
			StorageRef: "s3://private/prompt?versionId=v1"}}
	schedule := entity.Resource{ID: "99999999-9999-4999-8999-999999999999", Name: "Проверка",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindSchedule, State: enum.StateActive, Version: 7, CreatedAt: now, UpdatedAt: now,
		Spec: entity.ScheduleSpec{OwnerPresetKey: "daily", OwnerPresetRevision: 1,
			OwnerPresetSHA256: strings.Repeat("a", 64), OwnerDefaultsRevision: 1,
			OwnerDefaultsSHA256: strings.Repeat("b", 64), AgentID: agent.ID, AgentVersion: agent.Version,
			AgentSHA256: agentSHA, InstructionSetID: instruction.ID, InstructionSetVersion: instruction.Version,
			InstructionSetSHA256: instructionSHA, ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version,
			ProviderPoolSHA256: poolSHA, RoomID: room.ID, PromptArtifactID: artifact.ID,
			PromptArtifactVersion: artifact.Version, PromptSHA256: strings.Repeat("f", 64),
			PromptIntentKind: "SELECTOR", PromptDisplay: artifact.Name, OwnerAgentSelector: "developer-agent",
			OwnerInstructionSelector: "developer-instructions", OwnerProviderPoolSelector: "primary-pool",
			OwnerRoomSelector: "owner-room", OwnerRoomVersion: room.Version, OwnerRoomSHA256: roomSHA,
			OwnerPromptSelector: artifact.Name, AdvancedOverrides: []string{"maximum_attempts"},
			MaximumAttempts: 5}}
	tx := &ownerReadbackTransaction{resources: map[string]entity.Resource{agent.ID: agent, instruction.ID: instruction,
		pool.ID: pool, room.ID: room, artifact.ID: artifact}}
	service := &Service{}
	principal := value.Principal{ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID}
	projection, err := service.scheduleOwnerProjectionFromTx(context.Background(), tx, principal, schedule)
	if err != nil || projection.AgentSelection.StableSelector != "developer-agent" ||
		projection.InstructionSelection.StableSelector != "developer-instructions" ||
		projection.ProviderPoolSelection.StableSelector != "primary-pool" ||
		projection.RoomSelection.StableSelector != "owner-room" ||
		projection.Prompt.ArtifactSelector != artifact.Name || projection.Prompt.Status != OwnerProjectionPresent ||
		projection.MaximumAttempts != 5 || strings.Join(projection.AdvancedOverrides, ",") != "maximum_attempts" {
		t.Fatalf("selector round-trip: %#v %v", projection, err)
	}
	inline := schedule
	inlineSpec := inline.Spec.(entity.ScheduleSpec)
	inlineSpec.PromptIntentKind, inlineSpec.OwnerPromptSelector, inlineSpec.PromptDisplay = "INLINE", "", "Встроенный prompt"
	inline.Spec = inlineSpec
	objects := &ownerReadbackObjects{getRaw: []byte("# Встроенный prompt\n")}
	service.instructionObjects = objects
	projection, err = service.scheduleOwnerProjectionFromTx(context.Background(), tx, principal, inline)
	if err == nil {
		projection, err = service.hydrateOwnerSchedulePrompt(context.Background(), projection)
	}
	if err != nil || projection.Prompt.InlineMarkdown != "# Встроенный prompt\n" ||
		projection.Prompt.Object.Reference != "" {
		t.Fatalf("inline round-trip: %#v %v", projection.Prompt, err)
	}
}

func TestOwnerScheduleInlinePromptUsesContentAddressedVersionPinnedObject(t *testing.T) {
	objects := &ownerReadbackObjects{}
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	markdown := "# Проверка\n"
	idempotencyKey := "owner-inline-test"
	requestSHA256 := strings.Repeat("a", 64)
	repository := &ownerPromptRepository{preparation: domainrepo.SchedulePromptPreparation{
		OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, KeyHash: hashString(idempotencyKey),
		RequestSHA256: requestSHA256, SemanticSHA256: strings.Repeat("b", 64), Action: "create",
		ObjectKey: "schedule-prompts/" + hashString(markdown) + ".md", State: "AMBIGUOUS", Generation: 1,
	}, resources: ownerPromptFixtureResources(t, actorID, organizationID, projectID, "")}
	service := &Service{instructionObjects: objects, repository: repository}
	input := ownerSchedulePromptPreparationInput{
		Principal: value.Principal{ActorID: repository.preparation.OwnerActorID,
			OrganizationID: repository.preparation.OrganizationID, ProjectID: projectID},
		IdempotencyKey: idempotencyKey, RequestSHA256: requestSHA256, Action: "create",
		AgentStableKey: "developer-agent", InstructionSetStableKey: "developer-instructions",
		ProviderPoolStableKey: "primary-pool", SessionPolicy: "NEW"}
	semantic, err := service.validateOwnerSchedulePromptPreparation(context.Background(),
		&ownerPromptTransaction{repository: repository}, input)
	if err != nil {
		t.Fatal(err)
	}
	repository.preparation.SemanticSHA256 = semantic
	prompt, err := service.prepareOwnerSchedulePrompt(context.Background(), input,
		OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown})
	if err != nil || prompt.Object.VersionID == "" || prompt.Object.SHA256 == "" ||
		objects.projectID != projectID || objects.key != "schedule-prompts/"+prompt.Object.SHA256+".md" ||
		objects.digest != prompt.Object.SHA256 {
		t.Fatalf("inline prompt materialization: %#v %#v %v", prompt, objects, err)
	}
	replayed, err := service.prepareOwnerSchedulePrompt(context.Background(), input,
		OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown})
	if err != nil || replayed.Object.VersionID != prompt.Object.VersionID || objects.puts != 1 {
		t.Fatalf("durable replay created another object version: %#v puts=%d err=%v", replayed, objects.puts, err)
	}
}

func TestOwnerSchedulePromptPreparationHasSingleConcurrentWinnerAndBoundedRecovery(t *testing.T) {
	if ownerSchedulePromptRPCTimeout >= ownerSchedulePromptLease {
		t.Fatal("object RPC budget must end before durable preparation lease")
	}
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	markdown := "# Конкурентная проверка\n"
	idempotencyKey, requestSHA256 := "owner-inline-concurrent", strings.Repeat("c", 64)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	objects := &ownerReadbackObjects{putEntered: entered, putRelease: release}
	repository := &ownerPromptRepository{preparation: domainrepo.SchedulePromptPreparation{
		OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, KeyHash: hashString(idempotencyKey),
		RequestSHA256: requestSHA256, SemanticSHA256: strings.Repeat("d", 64), Action: "update",
		TargetID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ExpectedVersion: 9,
		ObjectKey: "schedule-prompts/" + hashString(markdown) + ".md", State: "AMBIGUOUS", Generation: 1,
	}}
	repository.resources = ownerPromptFixtureResources(t, actorID, organizationID, projectID, repository.preparation.TargetID)
	service := &Service{instructionObjects: objects, repository: repository}
	input := ownerSchedulePromptPreparationInput{Principal: value.Principal{ActorID: repository.preparation.OwnerActorID,
		OrganizationID: repository.preparation.OrganizationID, ProjectID: projectID}, IdempotencyKey: idempotencyKey,
		RequestSHA256: requestSHA256, Action: "update", ScheduleID: repository.preparation.TargetID, ExpectedVersion: 9,
		AgentStableKey: "developer-agent", InstructionSetStableKey: "developer-instructions",
		ProviderPoolStableKey: "primary-pool", SessionPolicy: "NEW"}
	semantic, err := service.validateOwnerSchedulePromptPreparation(context.Background(),
		&ownerPromptTransaction{repository: repository}, input)
	if err != nil {
		t.Fatal(err)
	}
	repository.preparation.SemanticSHA256 = semantic
	staleResources := make(map[string]entity.Resource, len(repository.resources))
	for id, item := range repository.resources {
		staleResources[id] = item
	}
	stalePool := staleResources["66666666-6666-4666-8666-666666666666"]
	stalePool.Version++
	staleResources[stalePool.ID] = stalePool
	staleObjects := &ownerReadbackObjects{}
	staleRepository := &ownerPromptRepository{preparation: repository.preparation, resources: staleResources}
	if _, err := (&Service{instructionObjects: staleObjects, repository: staleRepository}).prepareOwnerSchedulePrompt(
		context.Background(), input, OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown}); err == nil || staleObjects.puts != 0 {
		t.Fatalf("stale recovery left object effect: puts=%d err=%v", staleObjects.puts, err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := service.prepareOwnerSchedulePrompt(context.Background(), input,
			OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown})
		result <- err
	}()
	<-entered
	if _, err := service.prepareOwnerSchedulePrompt(context.Background(), input,
		OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("concurrent loser was not closed: %v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("recovery winner failed: %v", err)
	}
	if objects.puts != 1 || repository.preparation.State != "READY" {
		t.Fatalf("unexpected external effects: puts=%d state=%s", objects.puts, repository.preparation.State)
	}
	invalidObjects := &ownerReadbackObjects{}
	invalidService := &Service{instructionObjects: invalidObjects, repository: &ownerPromptRepository{}}
	if _, err := invalidService.prepareOwnerSchedulePrompt(context.Background(), input,
		OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: markdown}); err == nil || invalidObjects.puts != 0 {
		t.Fatalf("failed preflight left storage effect: puts=%d err=%v", invalidObjects.puts, err)
	}
}

func TestOwnerNextActionsMatrices(t *testing.T) {
	now := time.Now().UTC()
	if got := strings.Join((runActionDecision{Cancel: true}).actions(), ","); got != "CANCEL" {
		t.Fatalf("running actions: %s", got)
	}
	if got := strings.Join((runActionDecision{Retry: true}).actions(), ","); got != "RETRY" {
		t.Fatalf("failed actions: %s", got)
	}
	if got := (runActionDecision{}).actions(); len(got) != 0 {
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

func TestRunActionsUseTheSameLockedCommandPredicate(t *testing.T) {
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	processID := "44444444-4444-4444-8444-444444444444"
	turnID := "55555555-5555-4555-8555-555555555555"
	sessionID := "66666666-6666-4666-8666-666666666666"
	revisionID := "77777777-7777-4777-8777-777777777777"
	inputSHA := strings.Repeat("a", 64)
	turn := entity.Resource{ID: turnID, OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, Kind: enum.KindTurn, State: enum.StateRunning, Version: 8,
		Spec: entity.TurnSpec{SessionID: sessionID, ProcessRunID: processID, Attempt: 2,
			RuntimeRevisionID: revisionID, EffectiveInputSHA256: inputSHA}}
	process := entity.Resource{ID: processID, OrganizationID: organizationID, ProjectID: projectID,
		OwnerActorID: actorID, Kind: enum.KindProcessRun, State: enum.StateRunning, Version: 9,
		Spec: entity.ProcessRunSpec{CurrentSessionID: sessionID, CurrentSessionVersion: 3,
			CurrentTurnID: turnID, CurrentTurnVersion: turn.Version, CurrentAttempt: 2,
			CurrentRuntimeRevisionID: revisionID, CurrentRuntimeRevisionVersion: 4,
			CurrentInputSHA256: inputSHA}}
	graph := lockedOwnerGraph{Process: process, Turn: turn}
	principal := value.Principal{ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID}
	tx := &runActionTransaction{candidates: []entity.Resource{turn},
		attempt: domainrepo.TurnAttempt{TurnID: turnID, Attempt: 2, InputSHA256: inputSHA,
			AuthorityGeneration: 5}, leaseErr: errs.ErrNotFound}
	service := &Service{}
	decision, err := service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || strings.Join(decision.actions(), ",") != "CANCEL" {
		t.Fatalf("running command decision: %#v %v", decision, err)
	}
	tx.activeChildren = true
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("active child was ignored: %#v %v", decision, err)
	}
	tx.activeChildren = false
	finished := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	tx.attempt.FinishedAt, tx.attempt.State = finished, "FAILED"
	graph.Turn.State = enum.StateFailed
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || strings.Join(decision.actions(), ",") != "RETRY" {
		t.Fatalf("runtime-absent failed command decision: %#v %v", decision, err)
	}
	graph.Turn.State = enum.StateExpired
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("expired predecessor exposed an action: %#v %v", decision, err)
	}
	graph.Turn.State = enum.StateFailed
	graph.Runtime = &RuntimeExecution{State: "FAILED"}
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("terminal runtime exposed an action: %#v %v", decision, err)
	}
	graph.Runtime.State = "RUNNING"
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("live runtime exposed an action: %#v %v", decision, err)
	}
	graph.Runtime = nil
	graph.Process.State = enum.StateExpired
	graph.Turn.State = enum.StateFailed
	tx.attempt.State, tx.attempt.FinishedAt = string(enum.StateFailed), time.Now().UTC()
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("terminal process exposed retry: %#v %v", decision, err)
	}
	graph.Process.State = enum.StateRunning
	graph.Runtime = nil
	graph.Turn.State = enum.StateRunning
	tx.attempt.FinishedAt = time.Time{}
	tx.leaseErr = nil
	tx.lease = domainrepo.TurnLease{TurnID: turnID, Attempt: 2, Fence: turn.Version + 1,
		AuthorityGeneration: tx.attempt.AuthorityGeneration}
	decision, err = service.decideRunActionsLocked(context.Background(), tx, principal, graph)
	if err != nil || len(decision.actions()) != 0 {
		t.Fatalf("stale lease fence exposed cancel: %#v %v", decision, err)
	}
}

func TestRunDisplayUsesOnlyPinnedOwnerSafeMetadata(t *testing.T) {
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	agent := entity.Resource{ID: "44444444-4444-4444-8444-444444444444", Name: "Рабочий агент",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindAgent, State: enum.StateActive, Version: 2,
		Spec: ownerTestAgentSpec("worker-agent"), CreatedAt: now, UpdatedAt: now}
	role := entity.Resource{ID: "55555555-5555-4555-8555-555555555555", Name: "Разработчик",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindRoleDefinition, State: enum.StateActive, Version: 3,
		Spec: entity.RoleDefinitionSpec{StableKey: "developer", Capabilities: []string{"runtime.execute"},
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}, CreatedAt: now, UpdatedAt: now}
	pool := entity.Resource{ID: "66666666-6666-4666-8666-666666666666", Name: "Основной пул",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindProviderPool, State: enum.StateActive, Version: 4,
		Spec: ownerTestProviderPoolSpec("primary-pool", now), CreatedAt: now, UpdatedAt: now}
	agentSHA, err := entity.ProjectionSHA256(agent)
	if err != nil {
		t.Fatal(err)
	}
	roleSHA, err := entity.ProjectionSHA256(role)
	if err != nil {
		t.Fatal(err)
	}
	poolSHA, err := entity.ProjectionSHA256(pool)
	if err != nil {
		t.Fatal(err)
	}
	revision := entity.Resource{ID: "77777777-7777-4777-8777-777777777777",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindRuntimeRevision, State: enum.StateActive, Version: 5,
		Spec: entity.RuntimeRevisionSpec{CodexModel: "gpt-owner-safe", AgentID: agent.ID,
			AgentVersion: agent.Version, AgentSHA256: agentSHA, RoleDefinitionID: role.ID,
			RoleDefinitionVersion: role.Version, RoleDefinitionSHA256: roleSHA,
			ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA}}
	process := entity.Resource{ID: "88888888-8888-4888-8888-888888888888", Name: "Анализ репозитория",
		OrganizationID: organizationID, ProjectID: projectID, OwnerActorID: actorID,
		Kind: enum.KindProcessRun, State: enum.StateRunning, Version: 6, CreatedAt: now, UpdatedAt: now.Add(time.Minute),
		Spec: entity.ProcessRunSpec{RootInitiatorActorID: actorID, RootTriggerRef: "owner:manual", RootAttempt: 1}}
	repository := &ownerReadbackTransaction{resources: map[string]entity.Resource{
		projectID: {ID: projectID, Name: "Рабочее пространство", OrganizationID: organizationID,
			ProjectID: projectID, OwnerActorID: actorID},
		agent.ID: agent, role.ID: role, pool.ID: pool,
	}}
	projection, err := (&Service{}).runOwnerProjectionFromTx(context.Background(), repository,
		value.Principal{ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID},
		RunDetailResult{ProcessRun: process, RuntimeRevision: revision,
			Runtime: &RuntimeExecution{State: "RUNNING"}}, runActionDecision{Cancel: true})
	if err != nil || projection.Workspace.Value != "Рабочее пространство" ||
		projection.Trigger.Value != "Владелец" || projection.Initiator.Value != "Владелец" ||
		projection.Agent.Value != agent.Name || projection.Role.Value != role.Name ||
		projection.Model.Value != "gpt-owner-safe" || projection.Provider.Value != "Основной пул · AVAILABLE" ||
		strings.Join(projection.NextActions, ",") != "CANCEL" {
		t.Fatalf("safe run projection: %#v %v", projection, err)
	}
	for _, display := range []OwnerDisplayValue{projection.Workspace, projection.Trigger, projection.Initiator,
		projection.Agent, projection.Role, projection.Model, projection.Provider} {
		if strings.Contains(display.Value, actorID) || strings.Contains(display.Value, pool.ID) ||
			strings.Contains(display.Value, "provider-main") {
			t.Fatalf("private/internal reference leaked: %#v", display)
		}
	}
	lineage, err := RunLineageOwnerProjections(RunLineageResult{Processes: []domainrepo.RunGraphNode{{
		NodeType: "PROCESS", ID: process.ID, State: string(enum.StateRunning), Version: process.Version,
		DisplayName: process.Name, OccurredAt: now, UpdatedAt: now,
	}}})
	if err != nil || len(lineage) != 1 {
		t.Fatalf("safe lineage projection: %#v %v", lineage, err)
	}
	for _, display := range []OwnerDisplayValue{lineage[0].Agent, lineage[0].Role,
		lineage[0].Model, lineage[0].Provider} {
		if display.Status != OwnerProjectionUnavailable || display.Value != "" {
			t.Fatalf("lineage absence is not typed: %#v", display)
		}
	}
}

func TestRunLineageContinuationIsTamperAndSnapshotBound(t *testing.T) {
	service := &Service{leaseSigningKey: []byte(strings.Repeat("r", 32))}
	processID := "11111111-1111-4111-8111-111111111111"
	nodeID := "22222222-2222-4222-8222-222222222222"
	token, err := service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "LINEAGE",
		ProcessRunID: processID, ProcessVersion: 7, AfterType: "ATTEMPT", AfterID: nodeID})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := service.decodeRunSnapshotCursor(token)
	if err != nil || cursor.ProcessVersion != 7 || cursor.AfterType != "ATTEMPT" || cursor.AfterID != nodeID {
		t.Fatalf("lineage continuation round-trip: %#v %v", cursor, err)
	}
	tampered := token[:len(token)-1] + map[bool]string{true: "1", false: "0"}[token[len(token)-1] == '0']
	if _, err := service.decodeRunSnapshotCursor(tampered); err == nil {
		t.Fatal("tampered lineage continuation was accepted")
	}
	other := &Service{leaseSigningKey: []byte(strings.Repeat("s", 32))}
	if _, err := other.decodeRunSnapshotCursor(token); err == nil {
		t.Fatal("lineage continuation was accepted under another signing snapshot")
	}
	if cursor.ProcessVersion == 8 {
		t.Fatal("lineage continuation lost exact process version")
	}
}

func TestOwnerListCursorRejectsAnotherSnapshotAndKind(t *testing.T) {
	service := &Service{leaseSigningKey: []byte(strings.Repeat("l", 32))}
	afterID := "11111111-1111-4111-8111-111111111111"
	snapshot := hashString("100:110:")
	token, err := service.encodeRunSnapshotCursor(runSnapshotCursor{Kind: "AGENT_LIST",
		AfterID: afterID, SnapshotSHA256: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := service.decodeOwnerListCursor(token, "AGENT_LIST")
	if err != nil || cursor.AfterID != afterID || cursor.SnapshotSHA256 != snapshot {
		t.Fatalf("owner list cursor round-trip: %#v %v", cursor, err)
	}
	if _, err := service.decodeOwnerListCursor(token, "RUN_LIST"); err == nil {
		t.Fatal("owner list cursor was accepted for another projection kind")
	}
	if _, err := ownerListSnapshot(context.Background(),
		&snapshotFenceTransaction{fence: "100:110:"}, snapshot); err != nil {
		t.Fatalf("same owner snapshot was rejected: %v", err)
	}
	if _, err := ownerListSnapshot(context.Background(),
		&snapshotFenceTransaction{fence: "100:111:"}, snapshot); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("changed owner snapshot was accepted: %v", err)
	}
}

func TestRuntimeIncidentHistoryPreservesExactExecutionFence(t *testing.T) {
	actorID := "11111111-1111-4111-8111-111111111111"
	organizationID := "22222222-2222-4222-8222-222222222222"
	projectID := "33333333-3333-4333-8333-333333333333"
	processID := "44444444-4444-4444-8444-444444444444"
	turnID := "55555555-5555-4555-8555-555555555555"
	executionID := "66666666-6666-4666-8666-666666666666"
	incidentID := "77777777-7777-4777-8777-777777777777"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	tx := &incidentOwnerReadTransaction{
		incident: domainrepo.RuntimeIncident{ID: incidentID, OrganizationID: organizationID,
			ProjectID: projectID, ExecutionID: executionID, ExecutionFence: 47,
			Kind: "HEARTBEAT_MISSED", State: "ACKNOWLEDGED", Version: 9, OccurredAt: now, UpdatedAt: now},
		execution: domainrepo.RuntimeExecution{ID: executionID, ProcessID: processID, TurnID: turnID,
			Attempt: 2, Fence: 47, State: "FAILED"},
		process: entity.Resource{ID: processID, Name: "Сбой выполнения", OrganizationID: organizationID,
			ProjectID: projectID, OwnerActorID: actorID, Kind: enum.KindProcessRun, State: enum.StateFailed,
			Spec: entity.ProcessRunSpec{CurrentTurnID: turnID, CurrentAttempt: 2}},
		project: entity.Resource{ID: projectID, Name: "Рабочее пространство", OrganizationID: organizationID,
			ProjectID: projectID, OwnerActorID: actorID, Kind: enum.KindProject, State: enum.StateActive},
		history: []domainrepo.RuntimeIncidentHistory{
			{IncidentID: incidentID, Version: 9, ExecutionFence: 47, State: "ACKNOWLEDGED", Action: "ACKNOWLEDGE", OccurredAt: now},
			{IncidentID: incidentID, Version: 8, ExecutionFence: 46, State: "OPEN", Action: "DETECTED", OccurredAt: now.Add(-time.Minute)},
		},
	}
	service := &Service{repository: &incidentOwnerReadRepository{tx: tx}}
	page, err := service.ListRuntimeIncidentOwnerHistory(context.Background(),
		ownerPaginationPrincipal(actorID, organizationID, projectID, permissionRuntimeIncidentRead), incidentID, 0, 10)
	if err != nil || page.Current.ExecutionFence != 47 || len(page.Entries) != 2 ||
		page.Entries[0].ExecutionFence != 47 || page.Entries[1].ExecutionFence != 46 {
		t.Fatalf("incident fence history: %#v %v", page, err)
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
		page.Changes[0].Kind != "CHANGED" || page.Changes[0].Path != "/content/lines/1" ||
		page.Changes[0].Display != "REDACTED" || page.Changes[0].Before != "[REDACTED]" ||
		page.Changes[0].After != "[REDACTED]" {
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
	changes := boundedLineChanges("api_key: secret\nsession: opaque\nemail: owner@example.test\nold_name: value\nremoved",
		"api_key: changed\nsession: new-opaque\nemail: other@example.test\nnew_name: value\n")
	if len(changes) != 5 {
		t.Fatalf("unknown/PII change set was not preserved: %#v", changes)
	}
	for _, change := range changes {
		if change.Display != "REDACTED" || change.Before != "[REDACTED]" || change.After != "[REDACTED]" {
			t.Fatalf("default-redact bypass: %#v", change)
		}
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
