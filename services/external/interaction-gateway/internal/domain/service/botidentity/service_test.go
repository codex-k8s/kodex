package botidentity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domaincredential "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/credential"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/botidentity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

var testPrincipal = entity.TeamPrincipal{
	ActorID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	ProjectID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
}

const (
	testAgentRef = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	testKey      = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

func TestCreateAmbiguousEffectDefersWithoutBlindRetry(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	repository.begin = func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
		operation.Fence, operation.LeaseToken = 1, "lease"
		return operation, domainrepo.Claimed, nil
	}
	repository.markEffect = func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
		operation.EffectStartedAt = time.Now().UTC()
		return operation, nil
	}
	repository.deferRecovery = func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
		operation.State = enum.AgentBotOperationAmbiguous
		return operation, nil
	}
	provider := &fakeProvider{createErr: domainmattermost.ErrBotAmbiguousEffect}
	service := newTestService(t, repository, provider, &fakeCredentials{}, ownerWithoutBot(), teamSource())
	operation, _, err := service.CreateAndBind(context.Background(), testPrincipal, testAgentRef, 7,
		"agent-primary", "Agent primary", testKey)
	if !errors.Is(err, domainerrs.ErrAmbiguousEffect) || operation.State != enum.AgentBotOperationAmbiguous {
		t.Fatalf("ambiguous effect was not durably deferred: %#v %v", operation, err)
	}
	if provider.createCalls != 1 {
		t.Fatalf("provider create was repeated: %d", provider.createCalls)
	}
}

func TestTerminalReplayDoesNotRepeatProviderEffect(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{begin: func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
		operation.State = enum.AgentBotOperationBound
		operation.Result = entity.AgentMattermostBotBinding{AgentRef: testAgentRef, AgentVersion: 8}
		return operation, domainrepo.Replay, nil
	}}
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, &fakeCredentials{}, ownerWithoutBot(), teamSource())
	operation, result, err := service.CreateAndBind(context.Background(), testPrincipal, testAgentRef, 7,
		"agent-primary", "Agent primary", testKey)
	if err != nil || operation.State != enum.AgentBotOperationBound || result.AgentVersion != 8 {
		t.Fatalf("terminal replay mismatch: %#v %#v %v", operation, result, err)
	}
	if provider.createCalls != 0 {
		t.Fatalf("replay repeated provider effect: %d", provider.createCalls)
	}
}

func TestRevokeClosesGenerationBeforeProviderEffect(t *testing.T) {
	t.Parallel()
	closed := false
	identity := testIdentity(enum.AgentBotIdentityAvailable, 4)
	binding := entity.AgentMattermostBotBinding{
		AgentRef: testAgentRef, AgentVersion: 7, Identity: identity,
		ReceiptSHA256: hexDigest("a"),
	}
	repository := &fakeRepository{binding: binding}
	repository.begin = func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
		operation.Fence, operation.LeaseToken = 1, "lease"
		return operation, domainrepo.Claimed, nil
	}
	repository.closeGeneration = func(generation uint64) error {
		if generation != 4 {
			t.Fatalf("unexpected closed generation: %d", generation)
		}
		closed = true
		return nil
	}
	repository.markEffect = func(operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
		if !closed {
			t.Fatal("provider checkpoint started before generation closure")
		}
		return operation, nil
	}
	repository.accept = func(operation entity.AgentMattermostBotOperation, value entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error) {
		value.ProviderGeneration = 5
		operation.Identity, operation.State = value, enum.AgentBotOperationProviderAccepted
		return operation, nil
	}
	repository.finish = func(operation entity.AgentMattermostBotOperation, result entity.AgentMattermostBotBinding) error {
		if result.Identity.ProviderGeneration != 5 || result.Identity.Status != enum.AgentBotIdentityRevoked {
			t.Fatalf("unexpected terminal revoke binding: %#v", result)
		}
		return nil
	}
	provider := &fakeProvider{}
	provider.revoke = func(value entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, error) {
		if !closed {
			t.Fatal("provider revoke ran before stale generation closure")
		}
		value.Status, value.ProviderVersion, value.ProviderSnapshotSHA256 = enum.AgentBotIdentityRevoked, 2, hexDigest("b")
		provider.readIdentity = value
		return value, nil
	}
	credentials := &fakeCredentials{revoke: func(bindingID string, version uint64) error {
		if bindingID != identity.CredentialBindingID || version != identity.CredentialSecretVersion {
			t.Fatal("wrong credential version revoked")
		}
		return nil
	}}
	owner := ownerWithoutBot()
	owner.value.BotIdentityRef, owner.value.BotProviderGeneration = identity.ProviderObjectRef, 4
	owner.value.BotMaskedStatus = string(enum.AgentBotIdentityAvailable)
	owner.manage = func(input domaincontrol.ManageAgentMattermostBotIdentityInput) domaincontrol.AgentMattermostBotOwner {
		receiptSHA, err := internalrpcauth.CanonicalJSONSHA256(input.Credential.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		return domaincontrol.AgentMattermostBotOwner{
			AgentRef: testAgentRef, AgentStableKey: "agent-primary",
			AgentVersion: 8, BotIdentityRef: input.Credential.Receipt.ProviderObjectRef, BotProviderGeneration: 5,
			BotMaskedStatus: string(enum.AgentBotIdentityRevoked),
			BotReceiptID:    input.Credential.Receipt.ReceiptID, BotReceiptVersion: input.Credential.Receipt.ReceiptRevision,
			BotReceiptSHA256: receiptSHA,
		}
	}
	service := newTestService(t, repository, provider, credentials, owner, teamSource())
	operation, result, err := service.Revoke(context.Background(), testPrincipal, testAgentRef, 7, 4, testKey)
	if err != nil || operation.State != enum.AgentBotOperationRevoked || result.Identity.ProviderGeneration != 5 {
		t.Fatalf("revoke outcome mismatch: %#v %#v %v", operation, result, err)
	}
}

func TestStaleGenerationIsRejectedBeforeCredentialRead(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{admitErr: domainrepo.ErrNotFound}
	credentials := &fakeCredentials{}
	service := newTestService(t, repository, &fakeProvider{}, credentials, ownerWithoutBot(), teamSource())
	_, _, err := service.ReadCurrentRuntimeBotToken(context.Background(), testPrincipal,
		"agent-primary", "provider-user", 3)
	if !errors.Is(err, domainerrs.ErrUnauthorized) || credentials.readCalls != 0 {
		t.Fatalf("stale generation was not closed before credential read: %v calls=%d", err, credentials.readCalls)
	}
}

func TestRecoveredCredentialResolvesExactProviderToken(t *testing.T) {
	t.Parallel()
	identity := testIdentity(enum.AgentBotIdentityAvailable, 0)
	identity.ProviderTokenID = ""
	provider := &fakeProvider{resolveToken: func(value entity.AgentMattermostBotIdentity,
		correlation string,
	) (string, bool, error) {
		if value.ProviderUserID != identity.ProviderUserID || correlation != "operation-correlation" {
			t.Fatalf("unexpected provider token lookup: %#v %q", value, correlation)
		}
		return "provider-token-recovered", true, nil
	}}
	credentials := &fakeCredentials{recover: domaincredential.Materialized{
		SecretRef: "secret/data/agent-bot", Version: 3, ContentSHA256: hexDigest("4"),
	}}
	service := newTestService(t, &fakeRepository{}, provider, credentials, ownerWithoutBot(), teamSource())
	operation := entity.AgentMattermostBotOperation{
		ID:     "33333333-3333-4333-8333-333333333333",
		Intent: entity.AgentMattermostBotCreateIntent{ProviderCorrelation: "operation-correlation"},
	}

	recovered, err := service.ensureCredential(context.Background(), operation, identity)
	if err != nil || recovered.ProviderTokenID != "provider-token-recovered" ||
		recovered.CredentialSecretVersion != 3 || recovered.CredentialSHA256 != hexDigest("4") {
		t.Fatalf("recovered credential lost provider token lineage: %#v %v", recovered, err)
	}
}

func TestRecoveryDeadlineClassificationNeverUsesProcessClock(t *testing.T) {
	t.Parallel()
	operation := entity.AgentMattermostBotOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
		Action: enum.AgentBotActionCreateAndBind, AgentRef: testAgentRef,
		State: enum.AgentBotOperationEffectPending, RecoveryDeadline: time.Unix(1, 0).UTC(),
	}
	repository := &fakeRepository{claim: domainrepo.RecoveryClaim{Operation: operation, Found: true}}
	provider := &fakeProvider{recoverErr: domainmattermost.ErrBotNotFound}
	service := newTestService(t, repository, provider, &fakeCredentials{}, ownerWithoutBot(), teamSource())
	worked, err := service.ProcessRecovery(context.Background())
	if !worked || err == nil {
		t.Fatalf("recovery provider readback outcome was not returned: worked=%v err=%v", worked, err)
	}
	if repository.repairCalls != 0 {
		t.Fatalf("process clock classified DB-owned deadline: repair calls=%d", repository.repairCalls)
	}
}

func TestRecoveryPublishesDurableRepairBacklogWithoutClaimedWork(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{backlog: domainrepo.RepairBacklog{RecoveryTimeout: 2, Other: 1}}
	service := newTestService(t, repository, &fakeProvider{}, &fakeCredentials{}, ownerWithoutBot(), teamSource())
	metrics := &recordingMetrics{gauges: make(map[string]float64)}
	service.metrics = metrics
	worked, err := service.ProcessRecovery(context.Background())
	if err != nil || worked || metrics.gauges["recovery_timeout"] != 2 || metrics.gauges["other"] != 1 {
		t.Fatalf("durable repair backlog was not exported: worked=%v gauges=%v err=%v", worked, metrics.gauges, err)
	}
}

func TestAgentReadinessUsesFullRuntimeProofAndBoundedFailures(t *testing.T) {
	t.Parallel()
	identity := testIdentity(enum.AgentBotIdentityAvailable, 4)
	repository := &fakeRepository{binding: entity.AgentMattermostBotBinding{
		AgentRef: testAgentRef, AgentVersion: 7, Identity: identity,
	}, admit: identity}
	provider := &fakeProvider{readIdentity: identity}
	owner := ownerWithoutBot()
	mappingProofRef, err := agentBotMappingProofRef(teamSource().binding.Mapping)
	if err != nil {
		t.Fatal(err)
	}
	owner.value.BotIdentityRef = identity.ProviderObjectRef
	owner.value.BotUsername = identity.Username
	owner.value.BotProviderRevision = identity.ProviderVersion
	owner.value.BotProviderGeneration = identity.ProviderGeneration
	owner.value.BotProviderTeamRef = mappingProofRef
	owner.value.BotMaskedStatus = string(identity.Status)
	owner.value.BotReceiptID = "77777777-7777-4777-8777-777777777777"
	owner.value.BotReceiptVersion = identity.ProviderGeneration
	owner.value.BotReceiptSHA256 = hexDigest("7")
	service := newTestService(t, repository, provider, &fakeCredentials{}, owner, teamSource())
	ready := service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if !ready.Ready || !ready.PostgresReady || !ready.ControlPlaneReady || !ready.MattermostReady ||
		!ready.IdentityGenerationReady || provider.verifyCalls != 1 || provider.permissionCalls != 1 ||
		owner.getCalls != 2 || owner.manageCalls != 0 || owner.readinessCalls != 1 {
		t.Fatalf("full non-mutating runtime proof was not used by readiness: %#v verify=%d permission=%d get=%d manage=%d readiness=%d",
			ready, provider.verifyCalls, provider.permissionCalls, owner.getCalls, owner.manageCalls, owner.readinessCalls)
	}

	provider.verifyErr = domainmattermost.ErrBotForbidden
	failed := service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.MattermostReady || failed.FailureCode != "MATTERMOST_RUNTIME_NOT_READY" {
		t.Fatalf("revoked/invalid runtime token did not fail closed: %#v", failed)
	}

	provider.verifyErr = nil
	provider.permissionErr = domainmattermost.ErrBotForbidden
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.FailureCode != "MATTERMOST_PERMISSION_NOT_READY" {
		t.Fatalf("incomplete create/Team/token/revoke permissions did not fail closed: %#v", failed)
	}

	provider.permissionErr = nil
	provider.readErr = domainmattermost.ErrBotConflict
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.FailureCode != "MATTERMOST_IDENTITY_NOT_READY" {
		t.Fatalf("foreign owner or missing Team membership did not fail closed: %#v", failed)
	}

	provider.readErr = nil
	owner.value.BotIdentityRef = "88888888-8888-4888-8888-888888888888"
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.FailureCode != "OWNER_PREDECESSOR_NOT_READY" {
		t.Fatalf("control-plane owner mismatch did not fail closed: %#v", failed)
	}

	owner.value.BotIdentityRef = identity.ProviderObjectRef
	service.receipts = manageFailingSigner{}
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.ControlPlaneReady || failed.FailureCode != "CONTROL_PLANE_MANAGE_PROFILE_NOT_READY" {
		t.Fatalf("broken generated owner manage receipt profile did not fail closed: %#v", failed)
	}

	service.receipts = fakeSigner{}
	owner.readinessErr = errors.New("manage application profile is unavailable")
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.ControlPlaneReady || failed.FailureCode != "CONTROL_PLANE_MANAGE_PROFILE_NOT_READY" {
		t.Fatalf("broken generated owner Manage RPC did not fail closed: %#v", failed)
	}

	owner.readinessErr = nil
	service.receipts = failingSigner{}
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.ControlPlaneReady || failed.FailureCode != "CONTROL_PLANE_READBACK_NOT_READY" {
		t.Fatalf("broken signer/trust readback did not fail closed: %#v", failed)
	}

	service.receipts = fakeSigner{}
	repository.admitErr = domainrepo.ErrGenerationConflict
	failed = service.CheckAgent(context.Background(), testPrincipal, testAgentRef)
	if failed.Ready || failed.FailureCode != "IDENTITY_GENERATION_NOT_READY" {
		t.Fatalf("stale generation did not fail closed: %#v", failed)
	}
}

func TestCreateChecksProviderPermissionProfileBeforeEffect(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{permissionErr: domainmattermost.ErrBotForbidden}
	owner := ownerWithoutBot()
	service := newTestService(t, &fakeRepository{}, provider, &fakeCredentials{}, owner, teamSource())
	_, _, err := service.CreateAndBind(context.Background(), testPrincipal, testAgentRef, 7,
		"agent-primary", "Agent primary", testKey)
	if !errors.Is(err, domainerrs.ErrUnavailable) || provider.createCalls != 0 ||
		provider.permissionCalls != 1 || owner.getCalls != 0 {
		t.Fatalf("provider effect crossed incomplete permission predecessor: err=%v create=%d permission=%d owner=%d",
			err, provider.createCalls, provider.permissionCalls, owner.getCalls)
	}
}

func TestBindRebindRevokeAndRecoveryRejectFreshForeignOwnerBeforeOwnerEffect(t *testing.T) {
	t.Parallel()
	for _, action := range []string{enum.AgentBotActionBind, enum.AgentBotActionRebind, enum.AgentBotActionRevoke} {
		action := action
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			identity := testIdentity(enum.AgentBotIdentityAvailable, 5)
			if action == enum.AgentBotActionRevoke {
				identity.Status = enum.AgentBotIdentityRevoked
			}
			operation := entity.AgentMattermostBotOperation{
				ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
				Action: action, AgentRef: testAgentRef, ExpectedAgentVersion: 7,
				PredecessorGeneration: 4, Identity: identity,
			}
			expected := ownerWithoutBot()
			if action != enum.AgentBotActionBind {
				expected.value.BotIdentityRef = "77777777-7777-4777-8777-777777777777"
				expected.value.BotProviderGeneration = 4
				expected.value.BotMaskedStatus = string(enum.AgentBotIdentityAvailable)
			}
			current := *expected
			current.value.BotIdentityRef = "88888888-8888-4888-8888-888888888888"
			repository := &fakeRepository{}
			provider := &fakeProvider{readIdentity: identity}
			service := newTestService(t, repository, provider, &fakeCredentials{}, &current, teamSource())
			_, _, err := service.applyOwner(context.Background(), operation, expected.value)
			if !errors.Is(err, domainerrs.ErrConflict) || current.manageCalls != 0 || repository.repairCalls != 1 {
				t.Fatalf("foreign owner reached %s owner effect: manage=%d repair=%d err=%v",
					action, current.manageCalls, repository.repairCalls, err)
			}
		})
	}
}

func TestOwnerTransferRaceAfterEffectBecomesRepairWithoutTerminalSuccess(t *testing.T) {
	t.Parallel()
	identity := testIdentity(enum.AgentBotIdentityAvailable, 5)
	operation := entity.AgentMattermostBotOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
		Action: enum.AgentBotActionBind, IdempotencyKey: testKey, AgentRef: testAgentRef,
		ExpectedAgentVersion: 7, Identity: identity,
	}
	repository := &fakeRepository{}
	provider := &fakeProvider{readIdentity: identity}
	provider.read = func(call int) (entity.AgentMattermostBotIdentity, error) {
		if call == 1 {
			return identity, nil
		}
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotConflict
	}
	owner := ownerWithoutBot()
	owner.manage = func(input domaincontrol.ManageAgentMattermostBotIdentityInput) domaincontrol.AgentMattermostBotOwner {
		receiptSHA, err := internalrpcauth.CanonicalJSONSHA256(input.Credential.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		return domaincontrol.AgentMattermostBotOwner{
			AgentRef: testAgentRef, AgentStableKey: identity.AgentStableKey, AgentVersion: 8,
			BotIdentityRef: identity.ProviderObjectRef, BotProviderGeneration: identity.ProviderGeneration,
			BotMaskedStatus: string(identity.Status), BotReceiptID: input.Credential.Receipt.ReceiptID,
			BotReceiptVersion: identity.ProviderGeneration, BotReceiptSHA256: receiptSHA,
		}
	}
	service := newTestService(t, repository, provider, &fakeCredentials{}, owner, teamSource())
	_, _, err := service.applyOwner(context.Background(), operation, owner.value)
	if !errors.Is(err, domainerrs.ErrConflict) || owner.manageCalls != 1 || repository.repairCalls != 1 {
		t.Fatalf("owner transfer race became success: manage=%d repair=%d err=%v",
			owner.manageCalls, repository.repairCalls, err)
	}
}

func TestProviderAcceptedRecoveryFinishesExactOwnerReadback(t *testing.T) {
	t.Parallel()
	identity := testIdentity(enum.AgentBotIdentityRevoked, 5)
	operation := entity.AgentMattermostBotOperation{
		ID: "33333333-3333-4333-8333-333333333333", Principal: testPrincipal,
		Action: enum.AgentBotActionRevoke, IdempotencyKey: testKey, AgentRef: testAgentRef,
		ExpectedAgentVersion: 7, PredecessorGeneration: 4, State: enum.AgentBotOperationProviderAccepted,
		Identity: identity, Fence: 3, LeaseToken: "lease",
	}
	finished := false
	repository := &fakeRepository{finish: func(got entity.AgentMattermostBotOperation,
		binding entity.AgentMattermostBotBinding,
	) error {
		finished = true
		if got.ReceiptID != "44444444-4444-4444-8444-444444444444" ||
			binding.AgentVersion != 8 || binding.Identity.ProviderGeneration != 5 {
			t.Fatalf("unexpected recovered terminal checkpoint: %#v %#v", got, binding)
		}
		return nil
	}}
	owner := ownerWithoutBot()
	owner.value = domaincontrol.AgentMattermostBotOwner{
		AgentRef: testAgentRef, AgentStableKey: identity.AgentStableKey, AgentVersion: 8,
		BotIdentityRef: identity.ProviderObjectRef, BotProviderGeneration: identity.ProviderGeneration,
		BotMaskedStatus: string(identity.Status), BotReceiptID: "44444444-4444-4444-8444-444444444444",
		BotReceiptVersion: identity.ProviderGeneration, BotReceiptSHA256: hexDigest("5"),
	}
	owner.manage = func(domaincontrol.ManageAgentMattermostBotIdentityInput) domaincontrol.AgentMattermostBotOwner {
		t.Fatal("exact terminal owner readback must not repeat ManageAgentMattermostBotIdentity")
		return domaincontrol.AgentMattermostBotOwner{}
	}
	service := newTestService(t, repository, &fakeProvider{readIdentity: identity}, &fakeCredentials{}, owner, teamSource())

	recovered, binding, err := service.recoverOwnerOutcome(context.Background(), operation)
	if err != nil || !finished || recovered.State != enum.AgentBotOperationRevoked || binding.AgentVersion != 8 {
		t.Fatalf("owner response-loss recovery mismatch: %#v %#v %v", recovered, binding, err)
	}
}

func TestInvalidIdempotencyKeyRejectedBeforeOwnerRead(t *testing.T) {
	t.Parallel()
	owner := ownerWithoutBot()
	service := newTestService(t, &fakeRepository{}, &fakeProvider{}, &fakeCredentials{}, owner, teamSource())
	_, _, err := service.CreateAndBind(context.Background(), testPrincipal, testAgentRef, 7,
		"agent-primary", "Agent primary", "not-a-uuid")
	if !errors.Is(err, domainerrs.ErrUnauthorized) || owner.getCalls != 0 {
		t.Fatalf("invalid semantic key reached owner read: %v calls=%d", err, owner.getCalls)
	}
}

func TestOperationIDHasOneWinnerPerSemanticPredecessor(t *testing.T) {
	t.Parallel()
	first := stableOperationID(testPrincipal, testAgentRef, 7, 4)
	competingPrincipal := testPrincipal
	competingPrincipal.ActorID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	competingActor := stableOperationID(competingPrincipal, testAgentRef, 7, 4)
	competingAction := stableOperationID(testPrincipal, testAgentRef, 7, 4)
	nextVersion := stableOperationID(testPrincipal, testAgentRef, 8, 5)
	if first != competingActor || first != competingAction || first == nextVersion {
		t.Fatalf("semantic winner identity is not predecessor-scoped: %q %q %q %q",
			first, competingActor, competingAction, nextVersion)
	}
}

type fakeRepository struct {
	begin           func(entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error)
	markEffect      func(entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error)
	deferRecovery   func(entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error)
	closeGeneration func(uint64) error
	accept          func(entity.AgentMattermostBotOperation, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error)
	finish          func(entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding) error
	binding         entity.AgentMattermostBotBinding
	admit           entity.AgentMattermostBotIdentity
	admitErr        error
	claim           domainrepo.RecoveryClaim
	backlog         domainrepo.RepairBacklog
	repairCalls     int
}

func (repository *fakeRepository) Check(context.Context) error { return nil }
func (repository *fakeRepository) ResolveCatalogOffset(context.Context, entity.TeamPrincipal, string, uint32) (uint32, error) {
	return 0, nil
}

func (repository *fakeRepository) SaveCatalogPage(context.Context, entity.TeamPrincipal, []entity.AgentMattermostBotIdentity, uint32, uint32, bool, time.Duration) ([]entity.AgentMattermostBotIdentity, string, error) {
	return nil, "", nil
}

func (repository *fakeRepository) ResolveSelector(context.Context, entity.TeamPrincipal, string) (entity.AgentMattermostBotIdentity, error) {
	return entity.AgentMattermostBotIdentity{}, domainrepo.ErrNotFound
}

func (*fakeRepository) ReserveProviderObject(context.Context, entity.AgentMattermostBotOperation, string) error {
	return nil
}

func (repository *fakeRepository) BeginOperation(_ context.Context, operation entity.AgentMattermostBotOperation, _ string, _, _ time.Duration) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
	return repository.begin(operation)
}

func (repository *fakeRepository) GetOperation(context.Context, entity.TeamPrincipal, string, string, string) (entity.AgentMattermostBotOperation, error) {
	return entity.AgentMattermostBotOperation{}, domainrepo.ErrNotFound
}

func (repository *fakeRepository) MarkEffectStarted(_ context.Context, operation entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
	return repository.markEffect(operation)
}

func (repository *fakeRepository) MarkMembershipPending(context.Context, entity.AgentMattermostBotOperation, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error) {
	return entity.AgentMattermostBotOperation{}, errors.New("unexpected membership")
}

func (repository *fakeRepository) DeferRecovery(_ context.Context, operation entity.AgentMattermostBotOperation, _ string, _ time.Duration) (entity.AgentMattermostBotOperation, error) {
	return repository.deferRecovery(operation)
}

func (repository *fakeRepository) AcceptProvider(_ context.Context, operation entity.AgentMattermostBotOperation, identity entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error) {
	return repository.accept(operation, identity)
}

func (repository *fakeRepository) Finish(_ context.Context, operation entity.AgentMattermostBotOperation, binding entity.AgentMattermostBotBinding) error {
	return repository.finish(operation, binding)
}

func (repository *fakeRepository) MarkRepairRequired(context.Context, entity.AgentMattermostBotOperation, string) error {
	repository.repairCalls++
	return nil
}

func (repository *fakeRepository) ClaimRecovery(context.Context, string, time.Duration) (domainrepo.RecoveryClaim, error) {
	return repository.claim, nil
}

func (repository *fakeRepository) RepairBacklog(context.Context) (domainrepo.RepairBacklog, error) {
	return repository.backlog, nil
}

func (repository *fakeRepository) GetBinding(context.Context, entity.TeamPrincipal, string) (entity.AgentMattermostBotBinding, error) {
	if repository.binding.AgentRef == "" {
		return entity.AgentMattermostBotBinding{}, domainrepo.ErrNotFound
	}
	return repository.binding, nil
}

func (repository *fakeRepository) CloseGeneration(_ context.Context, _ entity.AgentMattermostBotOperation, generation uint64) error {
	return repository.closeGeneration(generation)
}

func (repository *fakeRepository) AdmitRuntimeIdentity(context.Context, entity.TeamPrincipal, string, string, uint64) (entity.AgentMattermostBotIdentity, error) {
	return repository.admit, repository.admitErr
}

func (repository *fakeRepository) ResolveRuntimeIdentity(context.Context, entity.TeamPrincipal, string, string) (entity.AgentMattermostBotIdentity, error) {
	return repository.admit, repository.admitErr
}

type fakeProvider struct {
	createCalls     int
	createErr       error
	revoke          func(entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, error)
	resolveToken    func(entity.AgentMattermostBotIdentity, string) (string, bool, error)
	recoverErr      error
	readIdentity    entity.AgentMattermostBotIdentity
	readErr         error
	verifyErr       error
	verifyCalls     int
	permissionErr   error
	permissionCalls int
	readCalls       int
	read            func(int) (entity.AgentMattermostBotIdentity, error)
}

func (provider *fakeProvider) CheckBotIdentityLifecycle(context.Context) error { return nil }
func (provider *fakeProvider) CheckBotIdentityPermissions(context.Context, entity.TeamPrincipal, string) error {
	provider.permissionCalls++
	return provider.permissionErr
}
func (provider *fakeProvider) ListBotIdentities(context.Context, entity.TeamPrincipal, string, uint32, uint32) ([]entity.AgentMattermostBotIdentity, bool, error) {
	return nil, false, nil
}

func (provider *fakeProvider) CreateBotIdentity(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotCreateIntent, string) (entity.AgentMattermostBotIdentity, error) {
	provider.createCalls++
	return entity.AgentMattermostBotIdentity{}, provider.createErr
}

func (provider *fakeProvider) RecoverCreatedBotIdentity(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotCreateIntent, string) (entity.AgentMattermostBotIdentity, error) {
	if provider.recoverErr == nil && provider.readIdentity.ProviderUserID == "" {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotNotFound
	}
	return provider.readIdentity, provider.recoverErr
}

func (provider *fakeProvider) ReadBotIdentity(context.Context, entity.TeamPrincipal, string, string) (entity.AgentMattermostBotIdentity, error) {
	provider.readCalls++
	if provider.read != nil {
		return provider.read(provider.readCalls)
	}
	if provider.readErr != nil {
		return entity.AgentMattermostBotIdentity{}, provider.readErr
	}
	if provider.readIdentity.ProviderUserID == "" {
		return entity.AgentMattermostBotIdentity{}, domainmattermost.ErrBotNotFound
	}
	return provider.readIdentity, nil
}

func (provider *fakeProvider) EnsureBotTeamMembership(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, error) {
	return entity.AgentMattermostBotIdentity{}, errors.New("unexpected membership")
}

func (provider *fakeProvider) CreateBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) (string, string, error) {
	return "", "", errors.New("unexpected token create")
}

func (provider *fakeProvider) ResolveBotAccessToken(_ context.Context, _ entity.TeamPrincipal, identity entity.AgentMattermostBotIdentity,
	correlation string,
) (string, bool, error) {
	if provider.resolveToken != nil {
		return provider.resolveToken(identity, correlation)
	}
	return "", false, nil
}

func (provider *fakeProvider) RecoverBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) (string, bool, error) {
	return "", false, nil
}

func (*fakeProvider) RevokeBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity) (bool, error) {
	return true, nil
}

func (provider *fakeProvider) RevokeBotIdentity(_ context.Context, _ entity.TeamPrincipal, identity entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, bool, error) {
	revoked, err := provider.revoke(identity)
	return revoked, err == nil, err
}

func (provider *fakeProvider) VerifyRuntimeBotCredential(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) error {
	provider.verifyCalls++
	return provider.verifyErr
}

type fakeCredentials struct {
	revoke     func(string, uint64) error
	readCalls  int
	recover    domaincredential.Materialized
	recoverErr error
}

func (*fakeCredentials) MaterializeBotToken(context.Context, string, string) (domaincredential.Materialized, error) {
	return domaincredential.Materialized{}, errors.New("unexpected materialization")
}

func (credentials *fakeCredentials) RecoverBotToken(context.Context, string) (domaincredential.Materialized, error) {
	if credentials.recover.Version != 0 || credentials.recoverErr != nil {
		return credentials.recover, credentials.recoverErr
	}
	return domaincredential.Materialized{}, errors.New("not found")
}

func (credentials *fakeCredentials) ReadBotToken(context.Context, string, uint64, string) (string, error) {
	credentials.readCalls++
	return "secret", nil
}

func (credentials *fakeCredentials) RevokeBotToken(_ context.Context, bindingID string, version uint64) (bool, error) {
	return true, credentials.revoke(bindingID, version)
}
func (*fakeCredentials) CheckBotTokenRevoked(context.Context, string, uint64) error { return nil }
func (*fakeCredentials) Check(context.Context) error                                { return nil }

type fakeOwner struct {
	value          domaincontrol.AgentMattermostBotOwner
	getCalls       int
	manageCalls    int
	readinessCalls int
	readinessErr   error
	manage         func(domaincontrol.ManageAgentMattermostBotIdentityInput) domaincontrol.AgentMattermostBotOwner
}

func (owner *fakeOwner) GetAgentMattermostBotIdentity(context.Context, domaincontrol.ProviderCredential, string) (domaincontrol.AgentMattermostBotOwner, error) {
	owner.getCalls++
	return owner.value, nil
}

func (owner *fakeOwner) ManageAgentMattermostBotIdentity(_ context.Context, input domaincontrol.ManageAgentMattermostBotIdentityInput) (domaincontrol.AgentMattermostBotOwner, error) {
	if input.Readiness {
		owner.readinessCalls++
		if owner.readinessErr != nil {
			return domaincontrol.AgentMattermostBotOwner{}, owner.readinessErr
		}
		return owner.manage(input), nil
	}
	owner.manageCalls++
	return owner.manage(input), nil
}

type fakeTeam struct {
	binding entity.WorkspaceMattermostBinding
}

func (team fakeTeam) GetBinding(context.Context, entity.TeamPrincipal) (entity.WorkspaceMattermostBinding, error) {
	return team.binding, nil
}

type fakeSigner struct{}

func (fakeSigner) Sign(receipt domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error) {
	return domaincontrol.ProviderCredential{CompactJWS: "signed", Receipt: receipt}, nil
}

type failingSigner struct{}

func (failingSigner) Sign(domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error) {
	return domaincontrol.ProviderCredential{}, errors.New("signer trust profile is unavailable")
}

type manageFailingSigner struct{}

func (manageFailingSigner) Sign(receipt domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error) {
	if receipt.FullMethod == ownerManageFullMethod {
		return domaincontrol.ProviderCredential{}, errors.New("manage receipt profile is unavailable")
	}
	return fakeSigner{}.Sign(receipt)
}

type fakeMetrics struct{}

func (fakeMetrics) ObserveBotIdentityOperation(string, string)  {}
func (fakeMetrics) ObserveExternalEffect(string, string)        {}
func (fakeMetrics) SetBotIdentityRepairBacklog(string, float64) {}

type recordingMetrics struct {
	gauges map[string]float64
}

func (*recordingMetrics) ObserveBotIdentityOperation(string, string) {}
func (*recordingMetrics) ObserveExternalEffect(string, string)       {}
func (metrics *recordingMetrics) SetBotIdentityRepairBacklog(reason string, value float64) {
	metrics.gauges[reason] = value
}

func newTestService(t *testing.T, repository *fakeRepository, provider *fakeProvider,
	credentials *fakeCredentials, owner *fakeOwner, teams fakeTeam,
) *Service {
	t.Helper()
	if repository.begin == nil {
		repository.begin = func(entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
			return entity.AgentMattermostBotOperation{}, 0, errors.New("unexpected begin")
		}
	}
	if repository.markEffect == nil {
		repository.markEffect = func(value entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
			return value, nil
		}
	}
	if repository.deferRecovery == nil {
		repository.deferRecovery = func(value entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error) {
			return value, nil
		}
	}
	if repository.closeGeneration == nil {
		repository.closeGeneration = func(uint64) error { return nil }
	}
	if repository.accept == nil {
		repository.accept = func(value entity.AgentMattermostBotOperation, identity entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error) {
			value.Identity = identity
			return value, nil
		}
	}
	if repository.finish == nil {
		repository.finish = func(entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding) error { return nil }
	}
	if provider.revoke == nil {
		provider.revoke = func(value entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, error) {
			return value, nil
		}
	}
	if credentials.revoke == nil {
		credentials.revoke = func(string, uint64) error { return nil }
	}
	if owner.manage == nil {
		owner.manage = func(domaincontrol.ManageAgentMattermostBotIdentityInput) domaincontrol.AgentMattermostBotOwner {
			return owner.value
		}
	}
	service, err := New(repository, provider, credentials, owner, teams, fakeSigner{}, fakeMetrics{}, Config{
		InstanceID: "test", Lease: 10 * time.Second, SelectorTTL: time.Minute,
		RecoveryInterval: time.Second, RecoveryWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func ownerWithoutBot() *fakeOwner {
	return &fakeOwner{value: domaincontrol.AgentMattermostBotOwner{
		AgentRef:       testAgentRef,
		AgentStableKey: "agent-primary", AgentVersion: 7,
	}}
}

func teamSource() fakeTeam {
	return fakeTeam{binding: entity.WorkspaceMattermostBinding{
		Mapping: entity.WorkspaceMattermostMapping{
			ID: "99999999-9999-4999-8999-999999999999", State: "BOUND", Version: 2, Generation: 3,
			ProviderEffectVersion: 2, ProviderEffectGeneration: 3,
		},
		Team: entity.MattermostTeam{ProviderTeamID: "provider-team", ProviderSnapshotSHA256: hexDigest("f")},
	}}
}

func testIdentity(status enum.AgentBotIdentityStatus, generation uint64) entity.AgentMattermostBotIdentity {
	return entity.AgentMattermostBotIdentity{
		IdentityRef:       "11111111-1111-4111-8111-111111111111",
		ProviderObjectRef: "11111111-1111-4111-8111-111111111111",
		AgentRef:          testAgentRef, AgentStableKey: "agent-primary", ProviderBotID: "provider-bot",
		ProviderUserID: "provider-user", ProviderTeamID: "provider-team", ProviderTokenID: "provider-token",
		CredentialBindingID: "22222222-2222-4222-8222-222222222222", CredentialSecretVersion: 2,
		CredentialSHA256: hexDigest("2"), Username: "agent-primary", DisplayName: "Agent primary",
		Status: status, ProviderVersion: 1, ProviderGeneration: generation, ProviderSnapshotSHA256: hexDigest("3"),
	}
}

func hexDigest(symbol string) string {
	result := ""
	for range 64 {
		result += symbol
	}
	return result
}
