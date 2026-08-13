package team

import (
	"context"
	"errors"
	"fmt"
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
	if operation.Intent.Slug == "owner-workspace" ||
		operation.Intent.Slug != providerOperationSlug("owner-workspace", operation.Intent.ProviderCorrelation) {
		t.Fatalf("provider identity is not bound to the durable operation: %q", operation.Intent.Slug)
	}
	provider.recovered = entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", DisplayName: operation.Intent.DisplayName, Slug: operation.Intent.Slug,
		Status: enum.MattermostTeamActive, ProviderSnapshotSHA256: digestValues("snapshot"),
		CreatedAt: time.Now().Add(-time.Second), UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	provider.recovered.ProviderCausalitySHA256 = digestValues("mattermost-team-create-proof-v1",
		operation.Intent.ProviderCorrelation, provider.recovered.ProviderTeamID,
		provider.recovered.CreatedAt.UTC().Format(time.RFC3339Nano))
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

func TestTransientCreateReadbackRemainsAmbiguousWithoutSecondPost(t *testing.T) {
	repository := &fakeRepository{}
	provider := &fakeProvider{createErr: domainmattermost.ErrAmbiguousEffect,
		recoverErr: errors.New("provider readback timeout")}
	service := newTestService(t, repository, provider, &fakeMapping{})
	_, err := service.Create(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	worked, err := service.ProcessRecovery(context.Background())
	if !worked || err != nil || provider.createCalls != 1 || provider.recoveryCalls != 1 ||
		repository.operation.State != enum.TeamOperationAmbiguous {
		t.Fatalf("transient recovery was terminal or repeated POST: worked=%v err=%v create=%d read=%d operation=%#v",
			worked, err, provider.createCalls, provider.recoveryCalls, repository.operation)
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

func TestProjectCreateFenceStopsDifferentCommandBeforeProvider(t *testing.T) {
	repository := &fakeRepository{beginErr: domainrepo.ErrCreateFenceConflict}
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, &fakeMapping{})
	_, err := service.Create(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace",
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrConflict) || provider.createCalls != 0 {
		t.Fatalf("create fence conflict reached provider: err=%v calls=%d", err, provider.createCalls)
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
		ProviderTeamID: "provider-team-one", DisplayName: operation.Intent.DisplayName, Slug: "owner-workspace",
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
	repository.mappingOperation.RecoveryDeadline = time.Now().Add(-time.Minute)
	worked, err := service.ProcessMappingRecovery(context.Background())
	if worked || err != nil || mapping.manageCalls != 1 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationRepairRequired ||
		repository.mappingOperation.FailureCode != "RECOVERY_TIMEOUT" {
		t.Fatalf("expired recovery repeated command: worked=%v err=%v calls=%d operation=%#v",
			worked, err, mapping.manageCalls, repository.mappingOperation)
	}
}

func TestConflictWithTransientOwnerReadbackRemainsAmbiguousUntilDeadline(t *testing.T) {
	team := entity.MattermostTeam{
		ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Status: enum.MattermostTeamActive, ProviderSnapshotSHA256: digestValues("snapshot"),
		CreatedAt: time.Now(), UpdatedAt: time.Now(), ObservedAt: time.Now(),
	}
	repository := &fakeRepository{selectorTeamID: team.ProviderTeamID}
	provider := &fakeProvider{readTeam: team}
	mapping := &fakeMapping{manageErr: domaincontrol.ErrConflict,
		listErrAfter: 2, listErr: domaincontrol.ErrUnavailable}
	service := newTestService(t, repository, provider, mapping)
	_, err := service.Link(context.Background(), testPrincipal, team.Selector,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrUnavailable) ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationAmbiguous ||
		repository.mappingOperation.FailureCode != "OWNER_OUTCOME_UNKNOWN" {
		t.Fatalf("transient owner readback became terminal: err=%v operation=%#v",
			err, repository.mappingOperation)
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
	provider := &fakeProvider{readiness: []entity.MattermostReadinessBinding{{Principal: testPrincipal, ProviderTeamID: "provider-team-one"}}}
	mapping := &fakeMapping{listErr: domaincontrol.ErrUnavailable, listErrAfter: -1}
	service, err := New(&fakeRepository{}, provider, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Check(context.Background()); err != nil {
		t.Fatalf("unbound owner cannot reach first bind path: %v", err)
	}
	if mapping.listCalls != 0 {
		t.Fatalf("first-bind readiness called unavailable owner mapping path: %d", mapping.listCalls)
	}
}

func TestTerminalMutationReplayPrecedesCurrentStateAndProviderChecks(t *testing.T) {
	key := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	selector := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	terminalMapping := entity.WorkspaceMattermostMapping{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Version: 2, Generation: 2, State: "BOUND",
		ProviderTeamID: "provider-team-one", ProviderEffectVersion: 2, ProviderEffectGeneration: 2,
		ProviderObservedAt: time.Now(), UpdatedAt: time.Now(),
	}
	tests := []struct {
		name, action, digest string
		state                string
		invoke               func(*Service) error
	}{
		{name: "create", action: "bind", state: enum.WorkspaceMappingOperationBound,
			digest: mappingRequestDigest(testPrincipal, "bind", "Owner Workspace", "owner-workspace"),
			invoke: func(service *Service) error {
				_, _, err := service.CreateAndBind(context.Background(), testPrincipal, "Owner Workspace", "owner-workspace", key)
				return err
			}},
		{name: "link", action: "bind", state: enum.WorkspaceMappingOperationBound,
			digest: mappingRequestDigest(testPrincipal, "bind", selector),
			invoke: func(service *Service) error {
				_, err := service.Link(context.Background(), testPrincipal, selector, key)
				return err
			}},
		{name: "relink", action: "relink", state: enum.WorkspaceMappingOperationBound,
			digest: mappingRequestDigest(testPrincipal, "relink", selector, "1", "1"),
			invoke: func(service *Service) error {
				_, err := service.Relink(context.Background(), testPrincipal, selector, 1, 1, key)
				return err
			}},
		{name: "unlink", action: "unlink", state: enum.WorkspaceMappingOperationUnlinked,
			digest: mappingRequestDigest(testPrincipal, "unlink", "1", "1"),
			invoke: func(service *Service) error {
				_, err := service.Unlink(context.Background(), testPrincipal, 1, 1, key)
				return err
			}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := terminalMapping
			if test.state == enum.WorkspaceMappingOperationUnlinked {
				result.State = "UNLINKED"
			}
			repository := &fakeRepository{mappingOperation: entity.WorkspaceMappingOperation{
				ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
				Action: test.action, IdempotencyKey: key, RequestSHA256: test.digest,
				State: test.state, Result: result, CreateOperationID: "44444444-4444-4444-8444-444444444444",
			}, operation: entity.MattermostTeamOperation{
				ID: "44444444-4444-4444-8444-444444444444", Principal: testPrincipal,
				State: enum.TeamOperationProviderAccepted,
			}}
			provider, mapping := &fakeProvider{}, &fakeMapping{}
			service := newTestService(t, repository, provider, mapping)
			if err := test.invoke(service); err != nil {
				t.Fatalf("terminal replay failed: %v", err)
			}
			if provider.readCalls != 0 || mapping.listCalls != 0 || mapping.manageCalls != 0 {
				t.Fatalf("terminal replay reached post-state/effect: provider=%d list=%d manage=%d",
					provider.readCalls, mapping.listCalls, mapping.manageCalls)
			}
		})
	}
}

func TestMappingReplayDigestConflictStopsBeforeEffects(t *testing.T) {
	repository := &fakeRepository{mappingOperation: entity.WorkspaceMappingOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal, Action: "bind",
		IdempotencyKey: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", RequestSHA256: digestValues("different"),
		State: enum.WorkspaceMappingOperationBound,
	}}
	provider, mapping := &fakeProvider{}, &fakeMapping{}
	service := newTestService(t, repository, provider, mapping)
	_, err := service.Link(context.Background(), testPrincipal,
		"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrConflict) || provider.readCalls != 0 || mapping.manageCalls != 0 {
		t.Fatalf("mapping digest conflict reached effect: err=%v provider=%d manage=%d",
			err, provider.readCalls, mapping.manageCalls)
	}
}

func TestMappingReplayRemainsReachableForInsertRace(t *testing.T) {
	team := entity.MattermostTeam{ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), ObservedAt: time.Now()}
	result := entity.WorkspaceMattermostMapping{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Version: 1, Generation: 1, State: "BOUND",
		ProviderTeamID: team.ProviderTeamID, ProviderEffectVersion: 1, ProviderEffectGeneration: 1,
		ProviderObservedAt: time.Now(), UpdatedAt: time.Now(),
	}
	repository := &fakeRepository{mappingDisposition: domainrepo.MappingReplay,
		mappingOperation: entity.WorkspaceMappingOperation{
			ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
			Action: "bind", IdempotencyKey: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
			RequestSHA256: digestValues("request"), Team: team,
			State: enum.WorkspaceMappingOperationBound, Result: result,
		}}
	provider, mapping := &fakeProvider{readTeam: team}, &fakeMapping{}
	service := newTestService(t, repository, provider, mapping)
	binding, err := service.beginMapping(context.Background(), testPrincipal, "bind", team, "", 0, 0,
		team.DisplayName, repository.mappingOperation.IdempotencyKey,
		repository.mappingOperation.RequestSHA256, "")
	if err != nil || binding.Mapping.ID != result.ID || mapping.manageCalls != 0 {
		t.Fatalf("transaction-race MappingReplay is unreachable: err=%v binding=%#v manage=%d",
			err, binding, mapping.manageCalls)
	}
}

func TestCurrentGenerationAdmissionAcceptsNewAndRejectsOld(t *testing.T) {
	team := entity.MattermostTeam{ProviderTeamID: "provider-team-new", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("new-snapshot"), ObservedAt: time.Now()}
	mappingState := entity.WorkspaceMattermostMapping{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", Version: 2, Generation: 2, State: "BOUND",
		ProviderTeamID: team.ProviderTeamID, ProviderEffectVersion: 2, ProviderEffectGeneration: 2,
		ProviderObservedAt: time.Now(), UpdatedAt: time.Now(),
	}
	repository := &fakeRepository{mappingOperation: entity.WorkspaceMappingOperation{Result: mappingState}}
	provider := &fakeProvider{readTeam: team, readiness: []entity.MattermostReadinessBinding{{Principal: testPrincipal, ProviderTeamID: team.ProviderTeamID}}}
	service := newTestService(t, repository, provider, &fakeMapping{current: mappingState})
	if _, err := service.RequireBoundTeam(context.Background(), testPrincipal, team.ProviderTeamID); err != nil {
		t.Fatalf("current generation was rejected: %v", err)
	}
	if _, err := service.RequireBoundTeam(context.Background(), testPrincipal, "provider-team-old"); !errors.Is(err, domainerrs.ErrUnauthorized) {
		t.Fatalf("old Team generation was accepted: %v", err)
	}
	if err := service.Check(context.Background()); err != nil {
		t.Fatalf("same joined path readiness failed: %v", err)
	}
}

func TestFreshTeamMembershipFailureStopsManageRetry(t *testing.T) {
	team := entity.MattermostTeam{ProviderTeamID: "provider-team-one", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), ObservedAt: time.Now()}
	repository := &fakeRepository{mappingOperation: entity.WorkspaceMappingOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal, Action: "bind",
		IdempotencyKey: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", Team: team,
		State: enum.WorkspaceMappingOperationAmbiguous, LeaseToken: "lease", Fence: 2,
		RecoveryDeadline: time.Now().Add(time.Minute),
	}}
	provider, mapping := &fakeProvider{readErr: domainmattermost.ErrTeamForbidden}, &fakeMapping{}
	service := newTestService(t, repository, provider, mapping)
	worked, err := service.ProcessMappingRecovery(context.Background())
	if !worked || !errors.Is(err, domainerrs.ErrConflict) || mapping.manageCalls != 0 {
		t.Fatalf("lost membership reached owner effect: worked=%v err=%v manage=%d", worked, err, mapping.manageCalls)
	}
}

func TestTransientFreshTeamReadbackRemainsAmbiguousUntilDeadline(t *testing.T) {
	team := entity.MattermostTeam{ProviderTeamID: "provider-team-one", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), ObservedAt: time.Now()}
	repository := &fakeRepository{mappingOperation: entity.WorkspaceMappingOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal, Action: "bind",
		IdempotencyKey: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", Team: team,
		State: enum.WorkspaceMappingOperationAmbiguous, LeaseToken: "lease", Fence: 2,
		RecoveryDeadline: time.Now().Add(time.Minute),
	}}
	provider, mapping := &fakeProvider{readErr: errors.New("provider readback timeout")}, &fakeMapping{}
	service := newTestService(t, repository, provider, mapping)
	worked, err := service.ProcessMappingRecovery(context.Background())
	if !worked || !errors.Is(err, domainerrs.ErrUnavailable) || mapping.manageCalls != 0 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationAmbiguous {
		t.Fatalf("transient Team readback became terminal: worked=%v err=%v manage=%d operation=%#v",
			worked, err, mapping.manageCalls, repository.mappingOperation)
	}
}

func TestBoundRoutePreflightStopsOwnerMutation(t *testing.T) {
	team := entity.MattermostTeam{ProviderTeamID: "provider-team-one", Selector: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		DisplayName: "Owner Workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: digestValues("snapshot"), ObservedAt: time.Now()}
	repository := &fakeRepository{selectorTeamID: team.ProviderTeamID}
	provider := &fakeProvider{readTeam: team, routeErr: domainmattermost.ErrTeamConflict}
	mapping := &fakeMapping{}
	service := newTestService(t, repository, provider, mapping)
	_, err := service.Link(context.Background(), testPrincipal, team.Selector,
		"dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	if !errors.Is(err, domainerrs.ErrConflict) || mapping.manageCalls != 0 ||
		repository.mappingOperation.State != enum.WorkspaceMappingOperationRepairRequired {
		t.Fatalf("unmaterializable BOUND route reached owner: err=%v manage=%d operation=%#v",
			err, mapping.manageCalls, repository.mappingOperation)
	}
}

func newTestService(t *testing.T, repository domainrepo.Repository, provider domainmattermost.TeamClient,
	mapping MappingClient,
) *Service {
	t.Helper()
	service, err := New(repository, provider, mapping, fakeSigner{}, &fakeMetrics{}, Config{
		InstanceID: "pod-one", Lease: 10 * time.Second, SelectorTTL: 10 * time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type fakeRepository struct {
	operation          entity.MattermostTeamOperation
	mappingOperation   entity.WorkspaceMappingOperation
	beginErr           error
	mappingDisposition domainrepo.MappingDisposition
	selectorTeamID     string
	generation         uint64
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

func (repository *fakeRepository) BeginCreate(_ context.Context, operation entity.MattermostTeamOperation, _ string, _, recoveryWindow time.Duration) (entity.MattermostTeamOperation, domainrepo.CreateDisposition, error) {
	if repository.beginErr != nil {
		return entity.MattermostTeamOperation{}, 0, repository.beginErr
	}
	operation.Fence, operation.LeaseToken, operation.CreatedAt, operation.UpdatedAt = 1, "lease-one", time.Now(), time.Now()
	operation.RecoveryDeadline = operation.CreatedAt.Add(recoveryWindow)
	repository.operation = operation
	return operation, domainrepo.CreateClaimed, nil
}

func (repository *fakeRepository) GetCreateOperation(_ context.Context, _ entity.TeamPrincipal,
	operationID string,
) (entity.MattermostTeamOperation, error) {
	if repository.operation.ID != operationID {
		return entity.MattermostTeamOperation{}, domainrepo.ErrNotFound
	}
	return repository.operation, nil
}

func (repository *fakeRepository) MarkEffectStarted(_ context.Context, operation entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error) {
	operation.State, operation.EffectStartedAt = enum.TeamOperationEffectPending, time.Now()
	repository.operation = operation
	return operation, nil
}

func (repository *fakeRepository) DeferCreateRecovery(_ context.Context, operation entity.MattermostTeamOperation, code string, retry time.Duration) (entity.MattermostTeamOperation, error) {
	operation.State, operation.FailureCode, operation.RetryNotBefore = enum.TeamOperationAmbiguous, code, time.Now().Add(retry)
	if !operation.RecoveryDeadline.IsZero() && !time.Now().Before(operation.RecoveryDeadline) {
		operation.State, operation.FailureCode = enum.TeamOperationRepairRequired, "RECOVERY_TIMEOUT"
	}
	operation.LeaseToken = ""
	repository.operation = operation
	return operation, nil
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
	if !time.Now().Before(repository.operation.RecoveryDeadline) {
		repository.operation.State, repository.operation.FailureCode = enum.TeamOperationRepairRequired, "RECOVERY_TIMEOUT"
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

func (repository *fakeRepository) BeginMapping(_ context.Context, operation entity.WorkspaceMappingOperation, _ string, _, recoveryWindow time.Duration) (entity.WorkspaceMappingOperation, domainrepo.MappingDisposition, error) {
	if repository.mappingDisposition != 0 {
		return repository.mappingOperation, repository.mappingDisposition, nil
	}
	operation.Fence, operation.LeaseToken = 1, "mapping-lease-one"
	operation.CreatedAt, operation.UpdatedAt = time.Now(), time.Now()
	operation.RecoveryDeadline = operation.CreatedAt.Add(recoveryWindow)
	repository.mappingOperation = operation
	return operation, domainrepo.MappingClaimed, nil
}

func (repository *fakeRepository) GetMappingOperation(_ context.Context, _ entity.TeamPrincipal,
	action, key string,
) (entity.WorkspaceMappingOperation, error) {
	if repository.mappingOperation.IdempotencyKey != key || repository.mappingOperation.Action != action {
		return entity.WorkspaceMappingOperation{}, domainrepo.ErrNotFound
	}
	return repository.mappingOperation, nil
}

func (repository *fakeRepository) PrepareMappingAttempt(_ context.Context,
	operation entity.WorkspaceMappingOperation, team entity.MattermostTeam, _ time.Duration,
) (entity.WorkspaceMappingOperation, error) {
	repository.generation++
	operation.State = enum.WorkspaceMappingOperationPending
	operation.Team = team
	operation.EffectGeneration = repository.generation
	operation.ReceiptID = fmt.Sprintf("%08d-9999-4999-8999-999999999999", repository.generation)
	repository.mappingOperation = operation
	return operation, nil
}

func (repository *fakeRepository) DeferMappingRecovery(_ context.Context, operation entity.WorkspaceMappingOperation, code string, retry time.Duration) (entity.WorkspaceMappingOperation, error) {
	operation.State, operation.FailureCode, operation.RetryNotBefore = enum.WorkspaceMappingOperationAmbiguous, code, time.Now().Add(retry)
	if !operation.RecoveryDeadline.IsZero() && !time.Now().Before(operation.RecoveryDeadline) {
		operation.State, operation.FailureCode = enum.WorkspaceMappingOperationRepairRequired, "RECOVERY_TIMEOUT"
	}
	operation.LeaseToken = ""
	repository.mappingOperation = operation
	return operation, nil
}

func (repository *fakeRepository) MarkMappingTerminal(_ context.Context, operation entity.WorkspaceMappingOperation, mapping entity.WorkspaceMattermostMapping, routes []entity.MattermostRuntimeRoute) error {
	operation.Result = mapping
	operation.State = enum.WorkspaceMappingOperationBound
	if mapping.State == "UNLINKED" {
		operation.State = enum.WorkspaceMappingOperationUnlinked
	}
	operation.LeaseToken = ""
	repository.mappingOperation = operation
	return nil
}

func (repository *fakeRepository) ReconcileRuntimeRoutes(_ context.Context, _ entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping, _ []entity.MattermostRuntimeRoute,
) error {
	repository.mappingOperation.Result = mapping
	return nil
}

func (repository *fakeRepository) ResolveRuntimeRoute(context.Context, string, string) (entity.MattermostRuntimeRoute, error) {
	return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
}

func (repository *fakeRepository) ResolveRuntimeDelivery(context.Context, string, string, string) (entity.MattermostRuntimeRoute, error) {
	return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
}

func (repository *fakeRepository) ListRuntimeRoutes(context.Context) ([]entity.MattermostRuntimeRoute, error) {
	return nil, nil
}

func (repository *fakeRepository) GetRuntimeAdmission(_ context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) (entity.MattermostRuntimeRoute, error) {
	mapping := repository.mappingOperation.Result
	if mapping.ID == "" {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	return entity.MattermostRuntimeRoute{Principal: principal, MappingID: mapping.ID,
		MappingVersion: mapping.Version, MappingGeneration: mapping.Generation,
		MappingDigestSHA256: mappingStateDigest(mapping), ProviderTeamID: providerTeamID}, nil
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
	if !time.Now().Before(repository.mappingOperation.RecoveryDeadline) {
		repository.mappingOperation.State, repository.mappingOperation.FailureCode = enum.WorkspaceMappingOperationRepairRequired, "RECOVERY_TIMEOUT"
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
	recoverErr    error
	recovered     entity.MattermostTeam
	readTeam      entity.MattermostTeam
	readiness     []entity.MattermostReadinessBinding
	readErr       error
	readCalls     int
	routeErr      error
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
	if provider.recoverErr != nil {
		return entity.MattermostTeam{}, provider.recoverErr
	}
	if provider.recovered.ProviderTeamID == "" {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
	}
	return provider.recovered, nil
}

func (provider *fakeProvider) EnsureCreatedTeamOwner(_ context.Context, _ entity.TeamPrincipal,
	_ entity.MattermostTeamCreateIntent, _ string,
) (entity.MattermostTeam, error) {
	return provider.recovered, nil
}

func (provider *fakeProvider) ReadTeam(_ context.Context, _ entity.TeamPrincipal, providerTeamID string) (entity.MattermostTeam, error) {
	provider.readCalls++
	if provider.readErr != nil {
		return entity.MattermostTeam{}, provider.readErr
	}
	if provider.readTeam.ProviderTeamID == "" {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
	}
	if provider.readTeam.ProviderTeamID != providerTeamID {
		return entity.MattermostTeam{}, domainmattermost.ErrTeamNotFound
	}
	return provider.readTeam, nil
}

func (provider *fakeProvider) BuildRuntimeRoutes(_ context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) ([]entity.MattermostRuntimeRoute, error) {
	if provider.routeErr != nil {
		return nil, provider.routeErr
	}
	return []entity.MattermostRuntimeRoute{{TemplateKey: "77777777-7777-4777-8777-777777777777",
		Principal: principal, ProviderTeamID: providerTeamID, ProviderSnapshotSHA256: provider.readTeam.ProviderSnapshotSHA256,
		Boundary: entity.Boundary{OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
			MappingOwnerActorID: principal.ActorID, TeamID: providerTeamID,
			ChatID: "11111111-1111-4111-8111-111111111111", RoleID: "22222222-2222-4222-8222-222222222222",
			ChannelID: "channel-one", Locale: "ru", BotStableKey: "developer"},
		RouteDigestSHA256: digestValues("route")}}, nil
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
	listCalls            int
	listErrAfter         int
	listErr              error
	manageErr            error
}

func (mapping *fakeMapping) ListWorkspaceMattermostMappings(context.Context, domaincontrol.ProviderCredential, string) ([]entity.WorkspaceMattermostMapping, error) {
	mapping.listCalls++
	if mapping.listErrAfter > 0 && mapping.listCalls > mapping.listErrAfter {
		return nil, mapping.listErr
	}
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
	if mapping.manageErr != nil {
		return entity.WorkspaceMattermostMapping{}, mapping.manageErr
	}
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
