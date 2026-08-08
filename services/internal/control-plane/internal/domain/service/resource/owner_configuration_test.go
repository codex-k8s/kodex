package resource

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestProtectedConfigurationRegistriesAreSpecialized(t *testing.T) {
	t.Parallel()

	want := map[enum.Kind][]string{
		enum.KindRoleDefinition:    {"archive", "create", "delete", "reconcile_git", "update"},
		enum.KindAgent:             {"archive", "bind_bot", "create", "delete", "disable", "enable", "pause", "rebind_bot", "reconcile_git", "resume", "revoke_bot", "update"},
		enum.KindAgentAssignment:   {"assign", "unassign"},
		enum.KindInstructionSet:    {"archive", "copy", "create", "delete", "detach", "publish", "reconcile_git", "rollback", "update", "validate"},
		enum.KindProviderReference: {"archive", "refresh", "register"},
		enum.KindProviderPool:      {"archive", "create", "delete", "reconcile_git", "update"},
	}
	if len(protectedConfigurationActions) != len(want) {
		t.Fatalf("unexpected protected kind count: %d", len(protectedConfigurationActions))
	}
	for kind, actions := range want {
		if len(protectedConfigurationActions[kind]) != len(actions) {
			t.Fatalf("unexpected specialized action count for %s: %#v", kind, protectedConfigurationActions[kind])
		}
		for _, action := range actions {
			if _, ok := protectedConfigurationActions[kind][action]; !ok {
				t.Fatalf("specialized action %s/%s is absent", kind, action)
			}
		}
		for _, forbidden := range []string{"transition", "manage", "grant", "escalate"} {
			if _, ok := protectedConfigurationActions[kind][forbidden]; ok {
				t.Fatalf("generic authority-bearing action %s/%s is present", kind, forbidden)
			}
		}
	}
}

type providerMaterializationTestTransaction struct {
	domainrepo.Transaction
	domainrepo.ProtectedTransaction
	resources map[string]entity.Resource
	history   []domainrepo.ProtectedResourceHistory
	audits    []domainrepo.Audit
}

func (tx *providerMaterializationTestTransaction) GetForUpdate(_ context.Context, _, _, id string) (entity.Resource, error) {
	resource, ok := tx.resources[id]
	if !ok {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func (tx *providerMaterializationTestTransaction) Insert(_ context.Context, resource entity.Resource) error {
	if _, exists := tx.resources[resource.ID]; exists {
		return errs.ErrStateConflict
	}
	tx.resources[resource.ID] = resource
	return nil
}

func (tx *providerMaterializationTestTransaction) AppendProtectedResourceHistory(_ context.Context, history domainrepo.ProtectedResourceHistory) error {
	tx.history = append(tx.history, history)
	return nil
}

func (tx *providerMaterializationTestTransaction) AppendAudit(_ context.Context, audit domainrepo.Audit) error {
	tx.audits = append(tx.audits, audit)
	return nil
}

type providerMaterializationTestRepository struct {
	domainrepo.Repository
	tx *providerMaterializationTestTransaction
}

func (repository *providerMaterializationTestRepository) Get(_ context.Context, organizationID, projectID, id string, kind enum.Kind) (entity.Resource, error) {
	resource, ok := repository.tx.resources[id]
	if !ok || resource.OrganizationID != organizationID || resource.ProjectID != projectID || resource.Kind != kind {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func TestProviderCredentialMaterializationAndReadbackAreExact(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	receipt := value.ProviderEffectReceipt{
		CredentialBindingID: "11111111-1111-4111-8111-111111111111", CredentialBindingVersion: 1,
		EffectGeneration: 3, Provider: "openai-codex", ProviderObjectRef: "22222222-2222-4222-8222-222222222222",
		SecretRef: "mattercodex/integration-gateway/provider-credentials/tenant/connection/3", SecretVersion: 7,
		SecretContentSHA256: digest, MaskedAccount: "o***@example.test", MaskedLabel: "plus",
		Capabilities: []string{"model-invoke"}, Eligible: true, ObservedUsage: 25, ObservedLimit: 100,
		ObservationRevision: 9, ObservedAt: now, WindowDurationSeconds: 300 * 60,
		ResetsAt: now.Add(time.Hour), ObservationExpiresAt: now.Add(5 * time.Minute), ObservationSHA256: digest,
		ReceiptID: "33333333-3333-4333-8333-333333333333", ReceiptRevision: 4,
	}
	materialization := controlplanecontract.ProviderCredentialMaterialization{
		CredentialBindingID: receipt.CredentialBindingID, BindingVersion: receipt.CredentialBindingVersion,
		CredentialGeneration: receipt.EffectGeneration, Provider: receipt.Provider, ProviderObjectRef: receipt.ProviderObjectRef,
		SecretRef: receipt.SecretRef, SecretVersion: receipt.SecretVersion, SecretContentSHA256: receipt.SecretContentSHA256,
		MaskedAccount: receipt.MaskedAccount, MaskedLabel: receipt.MaskedLabel, Capabilities: receipt.Capabilities,
		ObservedUsage: receipt.ObservedUsage, ObservedLimit: receipt.ObservedLimit, ObservationRevision: receipt.ObservationRevision,
		ObservedAt: receipt.ObservedAt, WindowSeconds: receipt.WindowDurationSeconds, ResetsAt: receipt.ResetsAt,
		ObservationExpiresAt: receipt.ObservationExpiresAt, ObservationSHA256: receipt.ObservationSHA256,
	}
	var err error
	receipt.CredentialBindingSHA256, err = controlplanecontract.ProviderCredentialMaterializationSHA256(materialization)
	if err != nil {
		t.Fatal(err)
	}
	principal := value.Principal{
		ActorID: "44444444-4444-4444-8444-444444444444", OrganizationID: "55555555-5555-4555-8555-555555555555",
		ProjectID: "66666666-6666-4666-8666-666666666666", Permission: permissionProviderReferenceManage,
		CallerWorkload: "integration-gateway", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		AuthoritySource: "PROVIDER_READBACK", AuthorityReference: receipt.ReceiptID,
		AuthorityRevision: receipt.ReceiptRevision, AuthorityGeneration: 1, AuthorityDigest: digest,
		CorrelationID: "77777777-7777-4777-8777-777777777777", PolicyRevision: 1,
	}
	tx := &providerMaterializationTestTransaction{resources: make(map[string]entity.Resource)}
	repository := &providerMaterializationTestRepository{tx: tx}
	service := &Service{repository: repository, integrationGatewayWorkload: principal.CallerWorkload,
		integrationGatewaySPIFFEID: principal.CallerSPIFFEID, now: func() time.Time { return now }}
	input := ManageProtectedConfigurationInput{Principal: principal, ProviderReceipt: receipt}

	created, err := service.materializeProviderCredential(context.Background(), tx, tx, input)
	if err != nil || created.ID != receipt.CredentialBindingID || created.Version != 1 ||
		len(tx.resources) != 1 || len(tx.history) != 1 || len(tx.audits) != 1 {
		t.Fatalf("atomic provider credential materialization failed: %#v %v", created, err)
	}
	replayed, err := service.materializeProviderCredential(context.Background(), tx, tx, input)
	if err != nil || replayed.ID != created.ID || len(tx.resources) != 1 || len(tx.history) != 1 {
		t.Fatalf("immutable provider credential replay failed: %#v %v", replayed, err)
	}
	readback, err := service.GetMaterializedProviderCredential(context.Background(), principal, created.ID)
	if err != nil || readback.ID != created.ID || readback.Version != created.Version {
		t.Fatalf("typed provider credential readback failed: %#v %v", readback, err)
	}
	conflict := input
	conflict.ProviderReceipt.SecretVersion++
	if _, err := service.materializeProviderCredential(context.Background(), tx, tx, conflict); err == nil || len(tx.resources) != 1 {
		t.Fatal("changed immutable secret coordinates mutated materialized credential")
	}
}

func TestRunTimelineCursorBindsChronologyAndRejectsTrailingPayload(t *testing.T) {
	t.Parallel()

	cursor := runTimelineCursor{
		OccurredAt: time.Date(2026, 8, 6, 12, 0, 0, 123000, time.UTC),
		ID:         "11111111-1111-4111-8111-111111111111",
	}
	token, err := encodeRunTimelineCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRunTimelineCursor(token)
	if err != nil || !decoded.OccurredAt.Equal(cursor.OccurredAt) || decoded.ID != cursor.ID {
		t.Fatalf("cursor round trip mismatch: %#v, %v", decoded, err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	withTrailing := base64.RawURLEncoding.EncodeToString(append(raw, []byte("{}")...))
	if _, err := decodeRunTimelineCursor(withTrailing); err == nil {
		t.Fatal("cursor with trailing JSON was accepted")
	}
}

func TestInstructionValidationIsServerComputedAndBounded(t *testing.T) {
	t.Parallel()

	if errorsFound := validateInstructionContent("обычная инструкция\nс табуляцией\t"); len(errorsFound) != 0 {
		t.Fatalf("valid content was rejected: %#v", errorsFound)
	}
	errorsFound := validateInstructionContent("line one\nline\x00two")
	if len(errorsFound) != 1 || errorsFound[0].Code != "unsupported_control_character" ||
		errorsFound[0].Line != 2 || errorsFound[0].Column != 5 {
		t.Fatalf("unexpected typed validation result: %#v", errorsFound)
	}
	if got := len(validateInstructionContent(strings.Repeat("\x00", 100))); got != 64 {
		t.Fatalf("validation error bound = %d, want 64", got)
	}
}

func TestProtectedConfigurationDoesNotDeclareFalseEventConsumer(t *testing.T) {
	t.Parallel()

	for _, kind := range []enum.Kind{
		enum.KindRoleDefinition, enum.KindAgent,
		enum.KindAgentAssignment, enum.KindInstructionSet, enum.KindProviderReference,
		enum.KindProviderPool, enum.KindWorkspaceBackup, enum.KindWorkspaceRestore,
		enum.KindWorkspaceMapping,
	} {
		if name, published := event.EventNameForKind(kind); published {
			t.Fatalf("protected kind %s declares unsupported event %s", kind, name)
		}
	}
	if name, published := event.EventNameForKind(enum.KindRuntimeRevision); !published || name != event.RuntimeConfigurationChanged {
		t.Fatal("materialized runtime revision lost its existing consumer event")
	}
}

func TestProtectedConfigurationStableKeysAreRecognized(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]entity.Spec{
		"role definition": entity.RoleDefinitionSpec{StableKey: "stable"},
		"agent":           entity.AgentSpec{StableKey: "stable"},
		"instruction":     entity.InstructionSetSpec{StableKey: "stable"},
		"provider ref":    entity.ProviderConnectionReferenceSpec{StableKey: "stable"},
		"provider pool":   entity.ProviderPoolSpec{StableKey: "stable"},
	} {
		t.Run(name, func(t *testing.T) {
			key, ok := protectedConfigurationStableKey(spec)
			if !ok || key != "stable" {
				t.Fatalf("protected stable key is not recognized: %q, %t", key, ok)
			}
		})
	}
	if _, ok := protectedConfigurationStableKey(entity.AgentAssignmentSpec{}); ok {
		t.Fatal("server-owned assignment unexpectedly exposes a stable key")
	}
}

func TestRuntimeIncidentTransitionRegistryIsClosed(t *testing.T) {
	t.Parallel()

	want := map[string]map[string]string{
		"OPEN":         {"acknowledge": "ACKNOWLEDGED", "retry": "RETRYING"},
		"ACKNOWLEDGED": {"retry": "RETRYING", "release": "RELEASED", "close": "CLOSED"},
		"RETRYING":     {"close": "CLOSED"},
		"RELEASED":     {"retry": "RETRYING", "close": "CLOSED"},
	}
	if !reflect.DeepEqual(runtimeIncidentTransitions, want) {
		t.Fatalf("unexpected incident lifecycle: %#v", runtimeIncidentTransitions)
	}
	for _, terminal := range []string{"CLOSED", "UNKNOWN"} {
		if len(runtimeIncidentTransitions[terminal]) != 0 {
			t.Fatalf("terminal incident state %s accepts an action", terminal)
		}
	}
}

func TestWorkspaceRecoveryActionRegistriesAreExact(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"create": {}, "cancel": {}, "retry": {},
	}
	if !reflect.DeepEqual(workspaceBackupActions, want) {
		t.Fatalf("unexpected workspace backup actions: %#v", workspaceBackupActions)
	}
	if !reflect.DeepEqual(workspaceRestoreActions, want) {
		t.Fatalf("unexpected workspace restore actions: %#v", workspaceRestoreActions)
	}
	if enum.TransitionAllowed(enum.KindWorkspaceMapping, enum.StateArchived, enum.StateActive) {
		t.Fatal("unlinked workspace mapping can be reopened without a fresh provider receipt")
	}
	if !enum.TransitionAllowed(enum.KindTurn, enum.StateCancelled, enum.StateQueued) {
		t.Fatal("full-envelope workspace restore cannot create a fresh cancelled attempt")
	}
}

func TestWorkspaceMappingIdempotencyIgnoresOneUseProviderProof(t *testing.T) {
	t.Parallel()

	base := ManageWorkspaceMappingInput{
		Principal: value.Principal{
			ActorID: "11111111-1111-4111-8111-111111111111", OrganizationID: "22222222-2222-4222-8222-222222222222",
			ProjectID: "33333333-3333-4333-8333-333333333333", Permission: permissionWorkspaceMappingManage,
			CallerWorkload: "interaction-gateway", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
			AuthoritySource: "PROVIDER_READBACK", AuthorityReference: "44444444-4444-4444-8444-444444444444",
			AuthorityRevision: 7, AuthorityDigest: strings.Repeat("a", 64), PolicyRevision: 9,
		},
		Action: "bind", Name: "Owner Workspace",
		ProviderReceipt: value.ProviderEffectReceipt{
			WorkspaceID: "33333333-3333-4333-8333-333333333333", ProviderTeamRef: "team-one",
			ProviderObjectRef: "owner-one", EffectGeneration: 7, EffectSHA256: strings.Repeat("b", 64),
			ReceiptID: "44444444-4444-4444-8444-444444444444", ReceiptRevision: 7,
			IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}
	first, err := workspaceMappingRequestHash(base)
	if err != nil {
		t.Fatal(err)
	}
	retry := base
	retry.Principal.AuthorityReference = "55555555-5555-4555-8555-555555555555"
	retry.Principal.AuthorityRevision++
	retry.Principal.AuthorityDigest = strings.Repeat("c", 64)
	retry.Principal.PolicyRevision++
	retry.ProviderReceipt.ReceiptID = retry.Principal.AuthorityReference
	retry.ProviderReceipt.ReceiptRevision++
	retry.ProviderReceipt.EffectGeneration++
	retry.ProviderReceipt.IssuedAt = retry.ProviderReceipt.IssuedAt.Add(time.Second)
	retry.ProviderReceipt.ExpiresAt = retry.ProviderReceipt.ExpiresAt.Add(time.Second)
	second, err := workspaceMappingRequestHash(retry)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("fresh one-use provider proof changed durable semantic idempotency")
	}
	retry.ProviderReceipt.ProviderTeamRef = "team-two"
	changed, err := workspaceMappingRequestHash(retry)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed provider Team reused durable semantic idempotency")
	}
}

func TestTargetScheduleDigestPinsPromptAndEverySelection(t *testing.T) {
	t.Parallel()

	spec := entity.ScheduleSpec{
		AgentID: "agent", AgentVersion: 1, AgentSHA256: strings.Repeat("1", 64),
		InstructionSetID: "instruction", InstructionSetVersion: 2, InstructionSetSHA256: strings.Repeat("2", 64),
		RuntimeSelectionRef: "runtime://standard", RuntimeSelectionVersion: 3,
		RuntimeSelectionSHA256: strings.Repeat("3", 64), ProviderPoolID: "pool",
		ProviderPoolVersion: 4, ProviderPoolSHA256: strings.Repeat("4", 64),
		AgentAssignmentID: "assignment", AgentAssignmentVersion: 5,
		AgentAssignmentSHA256: strings.Repeat("9", 64),
		TargetType:            "AGENT", SessionPolicy: "NEW",
	}
	base, err := targetScheduleEffectiveInput(spec, strings.Repeat("5", 64))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*entity.ScheduleSpec) string
	}{
		{"agent version", func(value *entity.ScheduleSpec) string { value.AgentVersion++; return strings.Repeat("5", 64) }},
		{"instruction digest", func(value *entity.ScheduleSpec) string {
			value.InstructionSetSHA256 = strings.Repeat("6", 64)
			return strings.Repeat("5", 64)
		}},
		{"runtime version", func(value *entity.ScheduleSpec) string {
			value.RuntimeSelectionVersion++
			return strings.Repeat("5", 64)
		}},
		{"pool digest", func(value *entity.ScheduleSpec) string {
			value.ProviderPoolSHA256 = strings.Repeat("7", 64)
			return strings.Repeat("5", 64)
		}},
		{"assignment version", func(value *entity.ScheduleSpec) string {
			value.AgentAssignmentVersion++
			return strings.Repeat("5", 64)
		}},
		{"prompt digest", func(_ *entity.ScheduleSpec) string { return strings.Repeat("8", 64) }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			changed := spec
			digest, digestErr := targetScheduleEffectiveInput(changed, test.edit(&changed))
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if digest == base {
				t.Fatal("changed target selection reused schedule digest")
			}
		})
	}
}

func TestScheduleSessionCompatibilityPinsWholeTuple(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	schedule := entity.ScheduleSpec{
		AgentID: "agent", AgentVersion: 2, AgentSHA256: digest,
		ProviderPoolID: "pool", ProviderPoolVersion: 3, ProviderPoolSHA256: digest,
		AgentAssignmentID: "assignment", AgentAssignmentVersion: 4,
		AgentAssignmentSHA256: digest, RoomID: "room",
	}
	session := entity.SessionSpec{
		AgentID: schedule.AgentID, AgentVersion: schedule.AgentVersion, AgentSHA256: schedule.AgentSHA256,
		ProviderPoolID: schedule.ProviderPoolID, ProviderPoolVersion: schedule.ProviderPoolVersion,
		ProviderPoolSHA256: schedule.ProviderPoolSHA256,
		AgentAssignmentID:  schedule.AgentAssignmentID, AgentAssignmentVersion: schedule.AgentAssignmentVersion,
		AgentAssignmentSHA256: schedule.AgentAssignmentSHA256, ConversationID: schedule.RoomID,
	}
	if !scheduleSessionCompatible(session, schedule) {
		t.Fatal("exact Schedule/Session tuple was rejected")
	}
	session.AgentAssignmentVersion++
	if scheduleSessionCompatible(session, schedule) {
		t.Fatal("stale assignment remained compatible with Schedule")
	}
}

func TestScheduleRebindCycleLeavesOneAdmissionActiveTuple(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("c", 64)
	tuple := func(agent, assignment string) entity.ScheduleSpec {
		return entity.ScheduleSpec{
			AgentID: agent, AgentVersion: 1, AgentSHA256: digest,
			ProviderPoolID: "pool", ProviderPoolVersion: 1, ProviderPoolSHA256: digest,
			AgentAssignmentID: assignment, AgentAssignmentVersion: 1,
			AgentAssignmentSHA256: digest, RoomID: "room",
		}
	}
	session := func(id string, state enum.State, spec entity.ScheduleSpec) entity.Resource {
		return entity.Resource{
			ID: id, OwnerActorID: "owner", Kind: enum.KindSession, State: state,
			Spec: entity.SessionSpec{
				AgentID: spec.AgentID, AgentVersion: spec.AgentVersion,
				AgentSHA256: spec.AgentSHA256, ProviderPoolID: spec.ProviderPoolID,
				ProviderPoolVersion: spec.ProviderPoolVersion, ProviderPoolSHA256: spec.ProviderPoolSHA256,
				AgentAssignmentID:      spec.AgentAssignmentID,
				AgentAssignmentVersion: spec.AgentAssignmentVersion,
				AgentAssignmentSHA256:  spec.AgentAssignmentSHA256, ConversationID: spec.RoomID,
			},
		}
	}
	t1, t2 := tuple("agent-t1", "assignment-t1"), tuple("agent-t2", "assignment-t2")
	resources := []entity.Resource{session("s1", enum.StateActive, t1)}
	if got := scheduleSessionCandidateIDs(resources, "owner", t1); !reflect.DeepEqual(got, []string{"s1"}) {
		t.Fatalf("initial T1 candidate mismatch: %#v", got)
	}
	resources[0].State = enum.StateArchived
	resources = append(resources, session("s2", enum.StateActive, t2))
	if got := scheduleSessionCandidateIDs(resources, "owner", t1); len(got) != 0 {
		t.Fatalf("archived T1 remained admission-active: %#v", got)
	}
	resources[1].State = enum.StateArchived
	resources = append(resources, session("s3", enum.StateActive, t1))
	if got := scheduleSessionCandidateIDs(resources, "owner", t1); !reflect.DeepEqual(got, []string{"s3"}) {
		t.Fatalf("T1 -> T2 -> T1 produced ambiguous active tuple: %#v", got)
	}
}

type scheduleFenceTestTransaction struct {
	domainrepo.Transaction
	order     []string
	resources []entity.Resource
}

func (tx *scheduleFenceTestTransaction) LockScheduleSessionProjectFence(
	context.Context, string, string,
) error {
	tx.order = append(tx.order, "fence")
	return nil
}

func (tx *scheduleFenceTestTransaction) ListScheduleSessionConversationForUpdate(
	context.Context, string, string, string,
) ([]entity.Resource, error) {
	tx.order = append(tx.order, "reread_for_update")
	return tx.resources, nil
}

func TestScheduleSessionFencePrecedesLockedCandidateReread(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("d", 64)
	spec := entity.ScheduleSpec{
		AgentID: "agent", AgentVersion: 1, AgentSHA256: digest,
		ProviderPoolID: "pool", ProviderPoolVersion: 1, ProviderPoolSHA256: digest,
		AgentAssignmentID: "assignment", AgentAssignmentVersion: 1,
		AgentAssignmentSHA256: digest, RoomID: "room",
	}
	tx := &scheduleFenceTestTransaction{resources: []entity.Resource{{
		ID: "session", OwnerActorID: "owner", Kind: enum.KindSession, State: enum.StateActive,
		Spec: entity.SessionSpec{
			AgentID: spec.AgentID, AgentVersion: spec.AgentVersion, AgentSHA256: spec.AgentSHA256,
			ProviderPoolID: spec.ProviderPoolID, ProviderPoolVersion: spec.ProviderPoolVersion,
			ProviderPoolSHA256: spec.ProviderPoolSHA256,
			AgentAssignmentID:  spec.AgentAssignmentID, AgentAssignmentVersion: spec.AgentAssignmentVersion,
			AgentAssignmentSHA256: spec.AgentAssignmentSHA256, ConversationID: spec.RoomID,
		},
	}}}
	principal := value.Principal{OrganizationID: "organization", ProjectID: "project", ActorID: "owner"}
	scheduleTx, err := lockScheduleSessionProjectFence(context.Background(), tx, principal)
	if err != nil {
		t.Fatal(err)
	}
	candidate, found, err := lockUniqueScheduleSessionAfterFence(
		context.Background(), scheduleTx, principal, spec,
	)
	if err != nil || !found || candidate.ID != "session" {
		t.Fatalf("locked candidate was not re-read: %#v, %v", candidate, err)
	}
	if !reflect.DeepEqual(tx.order, []string{"fence", "reread_for_update"}) {
		t.Fatalf("candidate list ran before deterministic fence: %#v", tx.order)
	}
}

func TestExternalSemanticReceiptReturnsImmutableResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	result, err := entity.New("11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "",
		"44444444-4444-4444-8444-444444444444", enum.KindRoleDefinition, "role",
		entity.RoleDefinitionSpec{
			StableKey: "role", Capabilities: []string{"resource.read"},
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err = result.Update(result.Name, result.Spec, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := entity.ProjectionSHA256(result)
	if err != nil {
		t.Fatal(err)
	}
	expected := domainrepo.ExternalCommandReceipt{
		Issuer: "issuer", Purpose: "purpose", ReceiptID: "55555555-5555-4555-8555-555555555555",
		OrganizationID: result.OrganizationID, ProjectID: result.ProjectID, OwnerActorID: result.OwnerActorID,
		TargetKind: "role_definition", TargetResourceID: result.ID, TargetStableKey: "role",
		Action: "reconcile_git", Effect: "git_configuration", EffectGeneration: 7,
		EffectSHA256: strings.Repeat("a", 64), CommandIntentSHA256: strings.Repeat("b", 64),
		AuthoritySHA256: strings.Repeat("c", 64),
	}
	stored := expected
	stored.ResultResourceID, stored.ResultVersion, stored.ResultSHA256 = result.ID, result.Version, digest
	stored.Result = result
	replayed, err := externalCommandReceiptReplay(stored, expected)
	if err != nil || replayed.Version != 2 {
		t.Fatalf("immutable semantic replay failed: %#v, %v", replayed, err)
	}
	conflicting := expected
	conflicting.Action = "different"
	if _, err := externalCommandReceiptReplay(stored, conflicting); err == nil {
		t.Fatal("conflicting semantic intent reused one-use receipt")
	}
	stale := expected
	stale.EffectGeneration--
	if _, err := externalCommandReceiptReplay(stored, stale); err == nil {
		t.Fatal("stale receipt generation reused immutable result")
	}
	proofChanged := expected
	proofChanged.AuthoritySHA256 = strings.Repeat("d", 64)
	if _, err := externalCommandReceiptReplay(stored, proofChanged); err == nil {
		t.Fatal("receipt signed by another transport proof reused immutable result")
	}
	incomplete := stored
	incomplete.Result, incomplete.ResultResourceID = entity.Resource{}, ""
	if _, err := externalCommandReceiptReplay(incomplete, expected); err == nil {
		t.Fatal("incomplete reservation was replayed")
	}
}

func TestExternalSemanticReplayRegistryIsNarrow(t *testing.T) {
	t.Parallel()

	for _, input := range []ManageProtectedConfigurationInput{
		{Kind: enum.KindAgent, Action: "bind_bot", ResourceID: "agent"},
		{Kind: enum.KindAgent, Action: "rebind_bot", ResourceID: "agent"},
		{Kind: enum.KindAgent, Action: "revoke_bot", ResourceID: "agent"},
		{Kind: enum.KindProviderReference, Action: "refresh", ResourceID: "provider"},
		{Kind: enum.KindRoleDefinition, Action: "reconcile_git", ResourceID: "role"},
		{Kind: enum.KindAgent, Action: "reconcile_git", ResourceID: "agent"},
		{Kind: enum.KindInstructionSet, Action: "reconcile_git", ResourceID: "instruction"},
		{Kind: enum.KindProviderPool, Action: "reconcile_git", ResourceID: "pool"},
	} {
		if !protectedExternalSemanticReplay(input) {
			t.Fatalf("approved semantic replay path was rejected: %s/%s", input.Kind, input.Action)
		}
	}
	for _, input := range []ManageProtectedConfigurationInput{
		{Kind: enum.KindAgent, Action: "update", ResourceID: "agent"},
		{Kind: enum.KindProviderPool, Action: "reconcile_git"},
		{Kind: enum.KindInstructionSet, Action: "publish", ResourceID: "instruction"},
	} {
		if protectedExternalSemanticReplay(input) {
			t.Fatalf("generic mutation gained semantic replay authority: %s/%s", input.Kind, input.Action)
		}
	}
}

func TestInstructionArtifactIdentityMatchesObjectDedupDomain(t *testing.T) {
	t.Parallel()

	projectID := "11111111-1111-4111-8111-111111111111"
	digest := strings.Repeat("b", 64)
	first := instructionArtifactID(projectID, "instructions-a", digest)
	if first != instructionArtifactID(projectID, "instructions-a", digest) {
		t.Fatal("instruction artifact identity is not deterministic")
	}
	if first == instructionArtifactID(projectID, "instructions-b", digest) {
		t.Fatal("different InstructionSet stable keys share one artifact identity")
	}
}

func TestRuntimeIncidentRetryFenceIsMonotonic(t *testing.T) {
	t.Parallel()

	incident := domainrepo.RuntimeIncident{State: "ACKNOWLEDGED", ExecutionFence: 4}
	execution := RuntimeExecution{State: "FAILED", Fence: 6}
	if !runtimeIncidentRetryEligible(incident, execution, true) {
		t.Fatal("terminal execution with a monotonic post-incident fence was rejected")
	}
	incident.ExecutionFence = 7
	if runtimeIncidentRetryEligible(incident, execution, true) {
		t.Fatal("future incident fence was accepted")
	}
	incident = domainrepo.RuntimeIncident{State: "RELEASED", ExecutionFence: 6}
	execution.State = "CANCELLED"
	if !runtimeIncidentRetryEligible(incident, execution, true) ||
		runtimeIncidentRetryEligible(incident, execution, false) {
		t.Fatal("released incident retry does not require exact current cancelled execution")
	}
}

func TestRunLineageLinksProcessesAndAttempts(t *testing.T) {
	t.Parallel()

	lineage := RunLineageResult{
		Processes: []domainrepo.RunGraphNode{
			{ID: "root"},
			{ID: "child-b", ParentProcessRunID: "root"},
			{ID: "child-a", ParentProcessRunID: "root"},
		},
		Attempts: []domainrepo.RunGraphNode{
			{ID: "attempt-3", TurnID: "turn", Attempt: 3},
			{ID: "attempt-1", TurnID: "turn", Attempt: 1},
			{ID: "attempt-2", TurnID: "turn", Attempt: 2},
		},
	}
	linkRunLineage(&lineage)
	if !reflect.DeepEqual(lineage.Processes[0].ChildIDs, []string{"child-a", "child-b"}) {
		t.Fatalf("unexpected child graph: %#v", lineage.Processes[0].ChildIDs)
	}
	byID := make(map[string]domainrepo.RunGraphNode, len(lineage.Attempts))
	for _, attempt := range lineage.Attempts {
		byID[attempt.ID] = attempt
	}
	if byID["attempt-1"].SuccessorID != "attempt-2" ||
		byID["attempt-2"].PredecessorID != "attempt-1" ||
		byID["attempt-2"].SuccessorID != "attempt-3" ||
		byID["attempt-3"].PredecessorID != "attempt-2" {
		t.Fatalf("unexpected attempt graph: %#v", lineage.Attempts)
	}
}

func TestRunLineageLinksPredecessorTurns(t *testing.T) {
	t.Parallel()

	lineage := RunLineageResult{Attempts: []domainrepo.RunGraphNode{
		{ID: "first", TurnID: "turn-a", Attempt: 1},
		{ID: "second", TurnID: "turn-b", Attempt: 2, PredecessorID: "turn-a"},
	}}
	linkRunLineage(&lineage)
	if lineage.Attempts[0].SuccessorID != "second" || lineage.Attempts[1].PredecessorID != "first" {
		t.Fatalf("cross-turn lineage is incomplete: %#v", lineage.Attempts)
	}
}

func TestLegacyCutoverIDsAreDeterministic(t *testing.T) {
	t.Parallel()

	const source = "mattercodex:legacy-agent:1a8a43c2-917b-4f04-8339-fd6bbf0421af"
	if first, second := deterministicLegacyID(source), deterministicLegacyID(source); first == "" || first != second {
		t.Fatalf("legacy mapping is not deterministic: %q != %q", first, second)
	}
	if deterministicLegacyID(source) == deterministicLegacyID(source+":different") {
		t.Fatal("different legacy identities produced one target identity")
	}
}
