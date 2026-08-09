package httptransport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRunSessionProjectionIsAllowlisted(t *testing.T) {
	now := timestamppb.Now()
	privateArchive := "s3://private-bucket/internal-object?credential=private"
	privateBinding := uuid.NewString()
	input := &controlplanev1.Resource{
		Id: uuid.NewString(), Kind: controlplanev1.ResourceKind_RESOURCE_KIND_SESSION,
		Name: "Owner session", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE,
		Version: 3, ProjectionSha256: strings.Repeat("a", 64), CreatedAt: now, UpdatedAt: now,
		Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Session{Session: &controlplanev1.SessionSpec{
			AgentId: uuid.NewString(), ProviderAccountBindingId: privateBinding, ArchiveRef: privateArchive,
		}}},
	}
	projection, err := castRunSession(input)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{privateArchive, privateBinding, "archiveRef", "providerAccountBindingId", "credential"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("private session value escaped: %q in %s", forbidden, text)
		}
	}
	if projection.SessionRef != input.GetId() || projection.DisplayName != input.GetName() || projection.Version != 3 {
		t.Fatalf("safe session metadata was lost: %#v", projection)
	}
	generic, err := ConvertResource(input)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	for _, forbidden := range []string{privateArchive, privateBinding, "archiveRef", "providerAccountBindingId", "agentId", "conversationId"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("private generic session value escaped: %q in %s", forbidden, text)
		}
	}
}

func TestOwnerRunAndRestoreKeepAuthoritativeActionsAndDisplay(t *testing.T) {
	now := timestamppb.Now()
	display := func(value string) *controlplanev1.OwnerDisplayValue {
		return &controlplanev1.OwnerDisplayValue{Status: controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_PRESENT, Value: value}
	}
	run, err := castRunView(&controlplanev1.RunOwnerProjection{
		RunRef: "run-safe", DisplayName: "Deploy docs", Version: 8,
		State:     controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE,
		Workspace: display("Workspace A"), Trigger: display("Owner request"), RuntimeStatus: display("Running"),
		Initiator: display("Owner"), Agent: display("Developer"), Role: display("Developer role"), Model: display("Runtime profile"), Provider: display("Provider pool"),
		Attempt: 2, UpdatedAt: now, Duration: durationpb.New(15 * time.Second),
		NextActions: []controlplanev1.RunNextAction{controlplanev1.RunNextAction_RUN_NEXT_ACTION_CANCEL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.NextActions) != 1 || run.NextActions[0] != generated.RunNextActionCANCEL || run.Agent.Value != "Developer" || run.Workspace.Value != "Workspace A" {
		t.Fatalf("authoritative run projection changed: %#v", run)
	}
	restore, err := castWorkspaceRestoreView(&controlplanev1.WorkspaceRestoreOwnerProjection{
		RestoreRef: "restore-safe", DisplayName: "Restore workspace", Version: 4,
		State:   controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_FAILED,
		Attempt: 1, Generation: 2, MemberCount: 3, CreatedAt: now, UpdatedAt: now,
		NextActions: []controlplanev1.WorkspaceRestoreNextAction{controlplanev1.WorkspaceRestoreNextAction_WORKSPACE_RESTORE_NEXT_ACTION_RETRY},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(restore.NextActions) != 1 || restore.NextActions[0] != generated.WorkspaceRestoreNextActionRETRY {
		t.Fatalf("authoritative restore actions changed: %#v", restore.NextActions)
	}
	if _, err := castRunNextActions([]controlplanev1.RunNextAction{controlplanev1.RunNextAction_RUN_NEXT_ACTION_UNSPECIFIED}); err == nil {
		t.Fatal("unknown authoritative next action did not fail closed")
	}
}

func TestBasicOwnerScheduleIntentDoesNotRequireAdvancedTuple(t *testing.T) {
	prompt := "Проверь состояние проекта"
	intent, ok := ownerScheduleIntent(generated.OwnerScheduleIntent{
		PresetKey: "daily", Timezone: "Europe/Moscow",
		Prompt: generated.OwnerSchedulePromptIntent{InlineMarkdown: &prompt},
	})
	if !ok || intent.GetPresetKey() != "daily" || intent.GetTimezone() != "Europe/Moscow" || intent.GetPrompt().GetInlineMarkdown() != prompt || intent.GetAdvancedOverrides() != nil {
		t.Fatalf("basic Schedule intent is not executable: %#v, ok=%v", intent, ok)
	}
}

func TestScheduleDefaultsRemainServerAuthoredAndTyped(t *testing.T) {
	digest := strings.Repeat("a", 64)
	defaults, err := castScheduleDefaults(&controlplanev1.OwnerScheduleDefaults{
		Revision: 9, Sha256: digest, Calendar: "GREGORIAN",
		OverlapPolicy:  controlplanev1.ScheduleOverlapPolicy_SCHEDULE_OVERLAP_POLICY_SKIP,
		MisfirePolicy:  controlplanev1.ScheduleMisfirePolicy_SCHEDULE_MISFIRE_POLICY_RUN_ONCE,
		DeliveryPolicy: "AT_LEAST_ONCE", MaximumAttempts: 3,
		InitialBackoff: durationpb.New(time.Second), MaximumBackoff: durationpb.New(time.Minute), DeadLetterAfter: durationpb.New(time.Hour),
		SessionPolicy:            controlplanev1.ScheduleSessionPolicy_SCHEDULE_SESSION_POLICY_PERSISTENT,
		NotificationPolicy:       controlplanev1.ScheduleNotificationPolicy_SCHEDULE_NOTIFICATION_POLICY_ON_FAILURE,
		MaximumExecutionDuration: durationpb.New(10 * time.Minute), Coalesce: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Revision != 9 || defaults.MaximumAttempts != 3 || defaults.OverlapPolicy != generated.ScheduleOverlapPolicySKIP || defaults.MisfirePolicy != generated.ScheduleMisfirePolicyRUNONCE {
		t.Fatalf("server-authored Schedule defaults changed: %#v", defaults)
	}
}

func TestBotProjectionOmitsProviderObjectAndRequestDigests(t *testing.T) {
	now := timestamppb.Now()
	privateObject := "provider-object-private"
	privateRequest := strings.Repeat("b", 64)
	identity := &interactiongatewayv1.AgentMattermostBotIdentityView{
		IdentityRef: privateObject, Selector: "bot-selector", Username: "agent-bot", DisplayName: "Agent bot",
		Status:          interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_AVAILABLE,
		ProviderVersion: 2, ProviderGeneration: 3, ProviderSnapshotSha256: strings.Repeat("a", 64), ObservedAt: now, UpdatedAt: now,
	}
	operation, err := castBotOperation(&interactiongatewayv1.AgentMattermostBotIdentityOperationView{
		OperationId: "operation-safe", AgentRef: "agent-safe", ExpectedAgentVersion: 4,
		Action:        interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND,
		State:         interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_BOUND,
		RequestSha256: privateRequest, CreatedAt: now, UpdatedAt: now,
		Result: &interactiongatewayv1.AgentMattermostBotIdentityBindingView{AgentRef: "agent-safe", AgentVersion: 5, Identity: identity, ReceiptSha256: strings.Repeat("c", 64), UpdatedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, privateObject) || strings.Contains(text, privateRequest) || strings.Contains(text, "identityRef") || strings.Contains(text, "requestSha256") {
		t.Fatalf("private bot identity metadata escaped: %s", text)
	}
}

func TestBotCommandActionsAcceptOnlyAuthoritativeSelectors(t *testing.T) {
	username, display, selector := "agent-bot", "Agent bot", "catalog-selector"
	generation := int64(3)
	if !validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionCREATEANDBIND, UsernameIntent: &username, DisplayName: &display}) ||
		!validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionBIND, IdentitySelector: &selector}) ||
		!validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionREBIND, IdentitySelector: &selector, ExpectedProviderGeneration: &generation}) ||
		!validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionREVOKE, ExpectedProviderGeneration: &generation}) {
		t.Fatal("valid bot identity action was rejected")
	}
	if validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionBIND}) ||
		validAgentBotCommand(generated.AgentBotIdentityCommand{Action: generated.AgentBotIdentityActionREVOKE, IdentitySelector: &selector, ExpectedProviderGeneration: &generation}) {
		t.Fatal("bot identity action accepted missing or extraneous selector input")
	}
}

func TestAgentCommandUsesOwnerRuntimeSelectionKey(t *testing.T) {
	name, stableKey := "Agent", "agent"
	runtimeSelectionKey, instructionKey, poolKey := "runtime-catalog-selection", "instructions", "provider-pool"
	enabled := true
	body := generated.AgentCommand{
		Action:                  generated.ProtectedConfigurationActionCREATE,
		Name:                    &name,
		StableKey:               &stableKey,
		RuntimeSelectionKey:     &runtimeSelectionKey,
		InstructionSetStableKey: &instructionKey,
		ProviderPoolStableKey:   &poolKey,
		Capabilities:            &[]string{},
		Enabled:                 &enabled,
	}
	if !validateAgentCommand(body, controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE) {
		t.Fatal("authoritative runtime catalog selection was rejected")
	}
	body.RuntimeSelectionKey = nil
	if validateAgentCommand(body, controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE) {
		t.Fatal("agent command without runtime catalog selection was accepted")
	}
}

func TestWorkspaceBackupAndRestoreActionValidation(t *testing.T) {
	reason, ref := "OWNER_CANCELLED", "resource-safe"
	backupCancel := generated.WorkspaceBackupCommand{Action: generated.WorkspaceBackupCommandActionCANCEL, BackupRef: &ref, TerminalReasonCode: &reason}
	backupRetry := generated.WorkspaceBackupCommand{Action: generated.WorkspaceBackupCommandActionRETRY, BackupRef: &ref}
	if !validWorkspaceBackupCommand(backupCancel, controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CANCEL) || !validWorkspaceBackupCommand(backupRetry, controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_RETRY) {
		t.Fatal("valid backup CANCEL/RETRY was rejected")
	}
	backupRetry.TerminalReasonCode = &reason
	if validWorkspaceBackupCommand(backupRetry, controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_RETRY) {
		t.Fatal("backup RETRY accepted terminalReasonCode")
	}
	blankReason := "  "
	backupCancel.TerminalReasonCode = &blankReason
	if validWorkspaceBackupCommand(backupCancel, controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CANCEL) {
		t.Fatal("backup CANCEL accepted blank terminalReasonCode")
	}
	restoreCancel := generated.WorkspaceRestoreCommand{Action: generated.WorkspaceRestoreCommandActionCANCEL, RestoreRef: &ref, TerminalReasonCode: &reason}
	restoreRetry := generated.WorkspaceRestoreCommand{Action: generated.WorkspaceRestoreCommandActionRETRY, RestoreRef: &ref}
	if !validWorkspaceRestoreCommand(restoreCancel, controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CANCEL) || !validWorkspaceRestoreCommand(restoreRetry, controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_RETRY) {
		t.Fatal("valid restore CANCEL/RETRY was rejected")
	}
	restoreRetry.TerminalReasonCode = &reason
	if validWorkspaceRestoreCommand(restoreRetry, controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_RETRY) {
		t.Fatal("restore RETRY accepted terminalReasonCode")
	}
}

func TestProviderAndIntegrationEnumsFailClosed(t *testing.T) {
	now := timestamppb.Now()
	digest := strings.Repeat("d", 64)
	if _, err := ConvertProviderPool(&integrationgatewayv1.ProviderPool{
		ProviderPoolId: "pool", StableKey: "pool", DisplayName: "Pool", Policy: "weighted", Version: 1,
		DesiredDigestSha256: digest, ObservationVersion: 1, ObservationDigestSha256: digest, EffectiveDigestSha256: digest,
		State: "FUTURE_STATE", UpdatedAt: now,
	}); err == nil {
		t.Fatal("unknown provider pool state did not fail closed")
	}
	if _, err := ConvertIntegrationDefinition(&integrationgatewayv1.IntegrationDefinitionSummary{
		DefinitionId: "definition", Version: 1, DigestSha256: digest, DisplayName: "Definition", State: "FUTURE_STATE",
	}); err == nil {
		t.Fatal("unknown integration definition state did not fail closed")
	}
	if _, err := ConvertIntegrationConfiguration(&integrationgatewayv1.IntegrationConfiguration{
		ConfigurationId: "configuration", StableKey: "configuration", Version: 1, DigestSha256: digest,
		DefinitionId: "definition", DefinitionVersion: 1, DefinitionDigestSha256: digest,
		ConnectionId: "connection", ConnectionVersion: 1, ConnectionGeneration: 1, CapabilityDigestSha256: digest,
		EffectKind: "FUTURE_EFFECT", State: "ACTIVE", UpdatedAt: now,
	}); err == nil {
		t.Fatal("unknown integration effect kind did not fail closed")
	}
}
