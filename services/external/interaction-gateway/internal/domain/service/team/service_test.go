package team

import (
	"context"
	"errors"
	"testing"
	"time"

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
	service, err := New(repository, provider, metrics, Config{
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
	service, err := New(repository, provider, &fakeMetrics{}, Config{
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
	service, err := New(repository, provider, &fakeMetrics{}, Config{
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

type fakeRepository struct {
	operation entity.MattermostTeamOperation
	beginErr  error
}

func (*fakeRepository) Check(context.Context) error { return nil }
func (*fakeRepository) ResolveCatalogOffset(context.Context, entity.TeamPrincipal, string, uint32) (uint32, error) {
	return 0, nil
}
func (*fakeRepository) SaveCatalogPage(context.Context, entity.TeamPrincipal, []entity.MattermostTeam, uint32, uint32, bool, time.Duration) ([]entity.MattermostTeam, string, error) {
	return nil, "", nil
}
func (*fakeRepository) ResolveSelector(context.Context, entity.TeamPrincipal, string) (string, error) {
	return "", domainrepo.ErrNotFound
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

type fakeProvider struct {
	createCalls   int
	recoveryCalls int
	createErr     error
	recovered     entity.MattermostTeam
}

func (*fakeProvider) CheckTeamLifecycle(context.Context) error { return nil }
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
func (*fakeProvider) ReadTeam(context.Context, entity.TeamPrincipal, string) (entity.MattermostTeam, error) {
	return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
}

type fakeMetrics struct{}

func (*fakeMetrics) ObserveTeamOperation(string, string)  {}
func (*fakeMetrics) ObserveExternalEffect(string, string) {}
