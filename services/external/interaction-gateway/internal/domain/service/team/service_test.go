package team

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/team"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

var testPrincipal = entity.TeamPrincipal{
	ActorID:        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	ProjectID:      "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
}

func TestNormalizeCreateIntentIsSemantic(t *testing.T) {
	first, err := normalizeCreateIntent("  Owner   Workspace  ", "Owner__Workspace", "dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := normalizeCreateIntent("Owner Workspace", "owner-workspace", "dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	if first.DisplayName != "Owner Workspace" || first.Slug != "owner-workspace" || first.RequestSHA256 != replay.RequestSHA256 {
		t.Fatalf("semantic normalization mismatch: %#v %#v", first, replay)
	}
	changed, err := normalizeCreateIntent("Another Workspace", "owner-workspace", "dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	if changed.RequestSHA256 == first.RequestSHA256 {
		t.Fatal("different semantic intent reused request digest")
	}
}

func TestAmbiguousCreateRecoveryNeverRepeatsProviderCreate(t *testing.T) {
	repository := &fakeRepository{}
	provider := &fakeProvider{createErr: domainmattermost.ErrAmbiguousEffect}
	metrics := &fakeMetrics{}
	service, err := New(repository, provider, &fakeMapping{}, fakeSigner{}, metrics, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := service.Create(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != enum.TeamOperationAmbiguous || provider.createCalls != 1 {
		t.Fatalf("unexpected ambiguous checkpoint: state=%s calls=%d", operation.State, provider.createCalls)
	}
	provider.recovered = entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", DisplayName: operation.Intent.DisplayName, Slug: operation.Intent.Slug,
		Status: enum.MattermostTeamActive, ProviderSnapshotSHA256: digestValues("snapshot"),
		CreatedAt: time.Now().Add(-time.Second), UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	worked, err := service.ProcessRecovery(context.Background())
	if err != nil || !worked {
		t.Fatalf("recovery failed: worked=%v err=%v", worked, err)
	}
	if provider.createCalls != 1 || provider.recoveryCalls != 1 {
		t.Fatalf("provider effect repeated: create=%d recovery=%d", provider.createCalls, provider.recoveryCalls)
	}
	if repository.operation.State != enum.TeamOperationProviderAccepted || repository.operation.ProviderGeneration != 1 {
		t.Fatalf("provider checkpoint was not accepted: %#v", repository.operation)
	}
}

func TestSemanticIdempotencyConflictStopsBeforeProvider(t *testing.T) {
	repository := &fakeRepository{beginErr: domainrepo.ErrIdempotencyConflict}
	provider := &fakeProvider{}
	service, err := New(repository, provider, &fakeMapping{}, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrConflict) || provider.createCalls != 0 {
		t.Fatalf("semantic conflict reached provider: err=%v calls=%d", err, provider.createCalls)
	}
}

func TestAmbiguousRecoveryRejectsPreexistingMatchingTeam(t *testing.T) {
	repository := &fakeRepository{}
	provider := &fakeProvider{createErr: domainmattermost.ErrAmbiguousEffect}
	service, err := New(repository, provider, &fakeMapping{}, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := service.Create(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	provider.recovered = entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", DisplayName: operation.Intent.DisplayName, Slug: operation.Intent.Slug,
		Status: enum.MattermostTeamActive, ProviderSnapshotSHA256: digestValues("snapshot"),
		CreatedAt: operation.EffectStartedAt.Add(-time.Minute), UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	worked, err := service.ProcessRecovery(context.Background())
	if err != nil || !worked {
		t.Fatalf("recovery failed: worked=%v err=%v", worked, err)
	}
	if provider.createCalls != 1 || repository.operation.State != enum.TeamOperationRepairRequired ||
		repository.operation.FailureCode != "PROVIDER_READBACK_MISMATCH" {
		t.Fatalf("preexisting Team was adopted: calls=%d operation=%#v", provider.createCalls, repository.operation)
	}
}

func TestAmbiguousMappingRecoveryUsesOwnerReadbackWithoutRepeatingCommand(t *testing.T) {
	team := entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Slug: "owner-workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	repository := &fakeRepository{selectorTeamID: team.ProviderTeamID}
	provider := &fakeProvider{readTeam: team}
	mapping := &fakeMapping{ambiguousAfterCommit: true}
	service, err := New(repository, provider, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Link(context.Background(), testPrincipal, team.Selector,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrUnavailable) || mapping.manageCalls != 1 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationAmbiguous {
		t.Fatalf("ambiguous mapping checkpoint mismatch: err=%v calls=%d state=%s", err,
			mapping.manageCalls, repository.mappingOperation.State)
	}
	worked, err := service.ProcessMappingRecovery(context.Background())
	if err != nil || !worked || mapping.manageCalls != 1 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationBound {
		t.Fatalf("mapping recovery repeated command: worked=%v err=%v calls=%d state=%s",
			worked, err, mapping.manageCalls, repository.mappingOperation.State)
	}
}

func TestAmbiguousMappingRecoveryRetriesWithFreshReceiptAfterProvenPredecessor(t *testing.T) {
	team := entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Slug: "owner-workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	repository := &fakeRepository{selectorTeamID: team.ProviderTeamID}
	mapping := &fakeMapping{failBeforeCommit: true}
	service, err := New(repository, &fakeProvider{readTeam: team}, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Link(context.Background(), testPrincipal, team.Selector,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrUnavailable) || mapping.manageCalls != 1 {
		t.Fatalf("first owner outcome is not ambiguous: err=%v calls=%d", err, mapping.manageCalls)
	}
	firstGeneration := repository.mappingOperation.EffectGeneration
	firstReceiptID := repository.mappingOperation.ReceiptID
	worked, err := service.ProcessMappingRecovery(context.Background())
	if err != nil || !worked || mapping.manageCalls != 2 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationBound {
		t.Fatalf("safe retry failed: worked=%v err=%v calls=%d operation=%#v",
			worked, err, mapping.manageCalls, repository.mappingOperation)
	}
	if repository.mappingOperation.EffectGeneration <= firstGeneration ||
		repository.mappingOperation.ReceiptID == firstReceiptID || len(mapping.receipts) != 2 ||
		mapping.receipts[1].EffectGeneration <= mapping.receipts[0].EffectGeneration ||
		mapping.receipts[1].ReceiptID == mapping.receipts[0].ReceiptID {
		t.Fatalf("ambiguous retry reused provider proof: first=%#v second=%#v operation=%#v",
			mapping.receipts[0], mapping.receipts[1], repository.mappingOperation)
	}
}

func TestAmbiguousMappingRecoveryWindowExpiresWithoutRepeatingCommand(t *testing.T) {
	team := entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Slug: "owner-workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	repository := &fakeRepository{selectorTeamID: team.ProviderTeamID}
	mapping := &fakeMapping{failBeforeCommit: true}
	service, err := New(repository, &fakeProvider{readTeam: team}, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Link(context.Background(), testPrincipal, team.Selector,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrUnavailable) {
		t.Fatalf("first owner outcome is not ambiguous: %v", err)
	}
	repository.mappingOperation.CreatedAt = time.Now().Add(-2 * time.Minute)
	worked, err := service.ProcessMappingRecovery(context.Background())
	if !worked || !errors.Is(err, domainerrs.ErrConflict) || mapping.manageCalls != 1 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationRepairRequired ||
		repository.mappingOperation.FailureCode != "RECOVERY_TIMEOUT" {
		t.Fatalf("expired recovery repeated command: worked=%v err=%v calls=%d operation=%#v",
			worked, err, mapping.manageCalls, repository.mappingOperation)
	}
}

func TestRequireBoundTeamRejectsUnlinkedOwnerState(t *testing.T) {
	team := entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), ObservedAt: time.Now(),
	}
	mapping := &fakeMapping{current: entity.WorkspaceMattermostMapping{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Version: 2, Generation: 2, State: "UNLINKED",
		ProviderTeamID: team.ProviderTeamID, ProviderEffectVersion: 2, ProviderEffectGeneration: 2,
		ProviderObservedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	service, err := New(&fakeRepository{}, &fakeProvider{readTeam: team}, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RequireBoundTeam(context.Background(), testPrincipal, team.ProviderTeamID)
	if !errors.Is(err, domainerrs.ErrUnauthorized) {
		t.Fatalf("unlinked owner mapping passed the working-path gate: %v", err)
	}
}

func TestReadinessKeepsFirstBindPathAvailableWithoutMapping(t *testing.T) {
	provider := &fakeProvider{readiness: []entity.MattermostReadinessBinding{{Principal: testPrincipal}}}
	service, err := New(&fakeRepository{}, provider, &fakeMapping{}, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Check(context.Background()); err != nil {
		t.Fatalf("unbound owner cannot reach first bind path: %v", err)
	}
}

type fakeRepository struct {
	operation        entity.MattermostTeamOperation
	mappingOperation entity.WorkspaceMappingOperation
	beginErr         error
	selectorTeamID   string
	generation       uint64
}

func (*fakeRepository) Check(context.Context) error { return nil }
func (*fakeRepository) ResolveCatalogOffset(context.Context, entity.TeamPrincipal, string, uint32) (uint32, error) {
	return 0, nil
}

func (*fakeRepository) SaveCatalogPage(context.Context, entity.TeamPrincipal, []entity.MattermostTeam, uint32, uint32, bool, time.Duration) ([]entity.MattermostTeam, string, error) {
	return nil, "", nil
}

func (repository *fakeRepository) ResolveSelector(context.Context, entity.TeamPrincipal, string) (string, error) {
	if repository.selectorTeamID == "" {
		return "", domainrepo.ErrNotFound
	}
	return repository.selectorTeamID, nil
}

func (*fakeRepository) RefreshSelector(_ context.Context, _ entity.TeamPrincipal, team entity.MattermostTeam, _ time.Duration) (entity.MattermostTeam, error) {
	return team, nil
}

func (repository *fakeRepository) BeginCreate(_ context.Context, operation entity.MattermostTeamOperation, _ string, _ time.Duration) (entity.MattermostTeamOperation, domainrepo.CreateDisposition, error) {
	if repository.beginErr != nil {
		return entity.MattermostTeamOperation{}, 0, repository.beginErr
	}
	operation.Fence, operation.LeaseToken, operation.CreatedAt, operation.UpdatedAt = 1, "lease-one", time.Now(), time.Now()
	repository.operation = operation
	return operation, domainrepo.CreateClaimed, nil
}

func (repository *fakeRepository) MarkEffectStarted(_ context.Context, operation entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error) {
	operation.State, operation.EffectStartedAt = enum.TeamOperationEffectPending, time.Now()
	repository.operation = operation
	return operation, nil
}

func (repository *fakeRepository) MarkAmbiguous(_ context.Context, operation entity.MattermostTeamOperation, code string, retry time.Time) error {
	operation.State, operation.FailureCode, operation.RetryNotBefore = enum.TeamOperationAmbiguous, code, retry
	operation.LeaseToken = ""
	repository.operation = operation
	return nil
}

func (repository *fakeRepository) MarkRepairRequired(_ context.Context, operation entity.MattermostTeamOperation, code string) error {
	operation.State, operation.FailureCode = enum.TeamOperationRepairRequired, code
	repository.operation = operation
	return nil
}

func (repository *fakeRepository) AcceptProvider(_ context.Context, operation entity.MattermostTeamOperation,
	team entity.MattermostTeam, receipt string, _ time.Duration,
) (entity.MattermostTeamOperation, error) {
	operation.State, operation.Team = enum.TeamOperationProviderAccepted, team
	operation.Team.Selector = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	operation.ProviderReceiptSHA256, operation.ProviderGeneration = receipt, 1
	operation.LeaseToken = ""
	repository.operation = operation
	return operation, nil
}

func (repository *fakeRepository) ClaimRecovery(context.Context, string, time.Duration) (entity.MattermostTeamOperation, bool, error) {
	if repository.operation.State != enum.TeamOperationAmbiguous {
		return entity.MattermostTeamOperation{}, false, nil
	}
	operation := repository.operation
	operation.Fence++
	operation.LeaseToken = "lease-two"
	return operation, true, nil
}

func (repository *fakeRepository) AdvanceProviderGeneration(context.Context, entity.TeamPrincipal) (uint64, error) {
	repository.generation++
	return repository.generation, nil
}

func (repository *fakeRepository) BeginMapping(_ context.Context, operation entity.WorkspaceMappingOperation, _ string, _ time.Duration) (entity.WorkspaceMappingOperation, domainrepo.MappingDisposition, error) {
	repository.generation++
	operation.EffectGeneration, operation.ReceiptID = repository.generation, "99999999-9999-4999-8999-999999999999"
	operation.Fence, operation.LeaseToken = 1, "mapping-lease-one"
	operation.CreatedAt, operation.UpdatedAt = time.Now(), time.Now()
	repository.mappingOperation = operation
	return operation, domainrepo.MappingClaimed, nil
}

func (repository *fakeRepository) RefreshMappingReceipt(_ context.Context,
	operation entity.WorkspaceMappingOperation,
) (entity.WorkspaceMappingOperation, error) {
	repository.generation++
	operation.State = enum.WorkspaceMappingOperationPending
	operation.EffectGeneration = repository.generation
	operation.ReceiptID = "88888888-8888-4888-8888-888888888888"
	repository.mappingOperation = operation
	return operation, nil
}

func (repository *fakeRepository) MarkMappingAmbiguous(_ context.Context, operation entity.WorkspaceMappingOperation, code string, retry time.Time) error {
	operation.State, operation.FailureCode, operation.RetryNotBefore = enum.WorkspaceMappingOperationAmbiguous, code, retry
	operation.LeaseToken = ""
	repository.mappingOperation = operation
	return nil
}

func (repository *fakeRepository) MarkMappingTerminal(_ context.Context, operation entity.WorkspaceMappingOperation, mapping entity.WorkspaceMattermostMapping) error {
	operation.Result = mapping
	operation.State = enum.WorkspaceMappingOperationBound
	if mapping.State == "UNLINKED" {
		operation.State = enum.WorkspaceMappingOperationUnlinked
	}
	operation.LeaseToken = ""
	repository.mappingOperation = operation
	return nil
}

func (repository *fakeRepository) MarkMappingRepairRequired(_ context.Context, operation entity.WorkspaceMappingOperation, code string) error {
	operation.State, operation.FailureCode = enum.WorkspaceMappingOperationRepairRequired, code
	operation.LeaseToken = ""
	repository.mappingOperation = operation
	return nil
}

func (repository *fakeRepository) ClaimMappingRecovery(context.Context, string, time.Duration) (entity.WorkspaceMappingOperation, bool, error) {
	if repository.mappingOperation.State != enum.WorkspaceMappingOperationAmbiguous {
		return entity.WorkspaceMappingOperation{}, false, nil
	}
	operation := repository.mappingOperation
	operation.Fence++
	operation.LeaseToken = "mapping-lease-two"
	return operation, true, nil
}

type fakeProvider struct {
	createCalls   int
	recoveryCalls int
	createErr     error
	recovered     entity.MattermostTeam
	readTeam      entity.MattermostTeam
	readiness     []entity.MattermostReadinessBinding
}

func (*fakeProvider) CheckTeamLifecycle(context.Context) error { return nil }
func (provider *fakeProvider) TeamReadinessBindings() []entity.MattermostReadinessBinding {
	return provider.readiness
}

func (*fakeProvider) ReadOwner(_ context.Context, principal entity.TeamPrincipal) (entity.MattermostOwnerObservation, error) {
	return entity.MattermostOwnerObservation{
		ProviderObjectRef: "owner-user-one",
		SnapshotSHA256:    digestValues(principal.ActorID, "owner-user-one"),
	}, nil
}

func (*fakeProvider) ListTeams(context.Context, entity.TeamPrincipal, uint32, uint32) ([]entity.MattermostTeam, bool, error) {
	return nil, false, nil
}

func (provider *fakeProvider) CreateTeam(_ context.Context, _ entity.TeamPrincipal, intent entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error) {
	provider.createCalls++
	return entity.MattermostTeam{DisplayName: intent.DisplayName, Slug: intent.Slug}, provider.createErr
}

func (provider *fakeProvider) RecoverCreatedTeam(context.Context, entity.TeamPrincipal, entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error) {
	provider.recoveryCalls++
	if provider.recovered.ProviderTeamID == "" {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
	}
	return provider.recovered, nil
}

func (provider *fakeProvider) ReadTeam(context.Context, entity.TeamPrincipal, string) (entity.MattermostTeam, error) {
	if provider.readTeam.ProviderTeamID == "" {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
	}
	return provider.readTeam, nil
}

type fakeMetrics struct{}

func (*fakeMetrics) ObserveTeamOperation(string, string)  {}
func (*fakeMetrics) ObserveExternalEffect(string, string) {}

type fakeMapping struct {
	current              entity.WorkspaceMattermostMapping
	manageCalls          int
	ambiguousAfterCommit bool
	failBeforeCommit     bool
	receipts             []domaincontrol.ProviderEffectReceipt
}

func (mapping *fakeMapping) ListWorkspaceMattermostMappings(context.Context, domaincontrol.ProviderCredential, string) ([]entity.WorkspaceMattermostMapping, error) {
	if mapping.current.ID == "" {
		return nil, nil
	}
	return []entity.WorkspaceMattermostMapping{mapping.current}, nil
}

func (mapping *fakeMapping) GetWorkspaceMattermostMapping(context.Context, domaincontrol.ProviderCredential, string) (entity.WorkspaceMattermostMapping, error) {
	if mapping.current.ID == "" {
		return entity.WorkspaceMattermostMapping{}, domaincontrol.ErrNotFound
	}
	return mapping.current, nil
}

func (mapping *fakeMapping) ManageWorkspaceMattermostMapping(_ context.Context, input domaincontrol.ManageWorkspaceMappingInput) (entity.WorkspaceMattermostMapping, error) {
	mapping.manageCalls++
	mapping.receipts = append(mapping.receipts, input.Credential.Receipt)
	if mapping.failBeforeCommit {
		mapping.failBeforeCommit = false
		return entity.WorkspaceMattermostMapping{}, domaincontrol.ErrUnavailable
	}
	mapping.current = entity.WorkspaceMattermostMapping{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Version: 1, Generation: 1, State: "BOUND",
		ProviderTeamID:           input.Credential.Receipt.ProviderTeamRef,
		ProviderEffectVersion:    input.Credential.Receipt.EffectVersion,
		ProviderEffectGeneration: input.Credential.Receipt.EffectGeneration,
		ProviderObservedAt:       time.Now(), UpdatedAt: time.Now(),
	}
	if mapping.ambiguousAfterCommit {
		mapping.ambiguousAfterCommit = false
		return entity.WorkspaceMattermostMapping{}, domaincontrol.ErrUnavailable
	}
	return mapping.current, nil
}

type fakeSigner struct{}

func (fakeSigner) Sign(receipt domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error) {
	return domaincontrol.ProviderCredential{CompactJWS: "signed", Receipt: receipt}, nil
}
