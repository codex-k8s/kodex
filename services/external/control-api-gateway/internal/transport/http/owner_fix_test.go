package httptransport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workspaceBoundaryControl struct {
	ControlPlane
	resources []*controlplanev1.Resource
}

func (control *workspaceBoundaryControl) ListResources(
	_ context.Context,
	request *controlplanev1.ListResourcesRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.ListResourcesResponse, error) {
	result := make([]*controlplanev1.Resource, 0, len(control.resources))
	for _, resource := range control.resources {
		if resource.GetKind() == request.GetKind() {
			result = append(result, resource)
		}
	}
	return &controlplanev1.ListResourcesResponse{Resources: result}, nil
}

func (control *workspaceBoundaryControl) GetResource(
	_ context.Context,
	request *controlplanev1.GetResourceRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.GetResourceResponse, error) {
	for _, resource := range control.resources {
		if resource.GetId() == request.GetResourceId() && resource.GetKind() == request.GetExpectedKind() {
			return &controlplanev1.GetResourceResponse{Resource: resource}, nil
		}
	}
	return &controlplanev1.GetResourceResponse{}, nil
}

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

func TestWorkspaceResourceProjectionRedactsPrivateLocatorsAndRelations(t *testing.T) {
	t.Parallel()
	private := []string{
		"vault-versioned://private/credential/7",
		"provider-account:private-principal",
		"ssh://private-repository/repo.git",
		"https://private-endpoint.internal/mcp",
		uuid.NewString(),
		uuid.NewString(),
	}
	ownership := &controlplanev1.ConfigurationOwnership{
		ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI,
		Drift:     controlplanev1.ConfigurationDrift_CONFIGURATION_DRIFT_NOT_APPLICABLE,
	}
	resources := []*controlplanev1.Resource{
		{Version: 2, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_CredentialBinding{CredentialBinding: &controlplanev1.CredentialBindingSpec{
			Purpose: "provider", ImmutableSecretRef: private[0], PrincipalRef: private[1], Revision: 2, Ownership: ownership,
		}}}},
		{Version: 2, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_RepositoryWorkspace{RepositoryWorkspace: &controlplanev1.RepositoryWorkspaceSpec{
			RepositoryRef: private[2], WorkspaceMode: "GIT", DefaultBranch: "main", CredentialBindingId: private[4], Ownership: ownership,
		}}}},
		{Version: 2, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Integration{Integration: &controlplanev1.IntegrationSpec{
			DefinitionRef: "definition-safe", DefinitionVersion: 1, CredentialBindingIds: []string{private[4]}, EndpointRef: private[3], Ownership: ownership,
		}}}},
		{Version: 2, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Team{Team: &controlplanev1.TeamSpec{
			StableKey: "team-safe", ExternalTeamRef: "private-team-locator", MemberActorIds: []string{private[4]}, RoleIds: []string{private[5]}, Ownership: ownership,
		}}}},
		{Version: 2, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Role{Role: &controlplanev1.RoleSpec{
			StableKey: "role-safe", AllowedTargetRoleIds: []string{private[5]}, PromptProfileId: private[4], RoleImageRecipeId: private[5],
			ProviderCredentialBindingIds: []string{private[4]}, RepositoryWorkspaceIds: []string{private[5]}, IntegrationIds: []string{private[4]},
			ProviderAccountPool: &controlplanev1.ProviderAccountPool{Policy: "least_used", PolicyRevision: 1, ObservationMaxAge: durationpb.New(time.Minute)}, Ownership: ownership,
		}}}},
	}
	for _, resource := range resources {
		projection, err := resourceProjection(resource)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(projection)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range append(private, "private-team-locator") {
			if strings.Contains(text, forbidden) {
				t.Fatalf("private workspace value escaped: %q in %s", forbidden, text)
			}
		}
		for _, forbiddenField := range []string{"immutableSecretRef", "principalRef", "repositoryRef", "endpointRef", "memberActorIds", "roleIds", "credentialBindingIds"} {
			if strings.Contains(text, forbiddenField) {
				t.Fatalf("private workspace field escaped: %q in %s", forbiddenField, text)
			}
		}
	}
}

func TestWorkspaceSelectorMaterializesPrivateSpecOnlyInsideGateway(t *testing.T) {
	t.Parallel()
	privateRepository := "ssh://private-repository/repo.git"
	privateEndpoint := "https://private-endpoint.internal/mcp"
	privateSecret := "vault-versioned://private/credential/7"
	bindingID := uuid.NewString()
	resources := []*controlplanev1.Resource{
		{Id: uuid.NewString(), Kind: controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, Name: "owner-repository", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_RepositoryWorkspace{RepositoryWorkspace: &controlplanev1.RepositoryWorkspaceSpec{RepositoryRef: privateRepository}}}},
		{Id: uuid.NewString(), Kind: controlplanev1.ResourceKind_RESOURCE_KIND_INTEGRATION, Name: "owner-integration", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_Integration{Integration: &controlplanev1.IntegrationSpec{EndpointRef: privateEndpoint}}}},
		{Id: bindingID, Kind: controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING, Name: "owner-credential", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_CredentialBinding{CredentialBinding: &controlplanev1.CredentialBindingSpec{ImmutableSecretRef: privateSecret, PrincipalRef: "private-principal"}}}},
		{Id: uuid.NewString(), Kind: controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_CONNECTION_REFERENCE, Name: "provider-account", State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Spec: &controlplanev1.ResourceSpec{Value: &controlplanev1.ResourceSpec_ProviderConnectionReference{ProviderConnectionReference: &controlplanev1.ProviderConnectionReferenceSpec{StableKey: "provider-selector", CredentialBindingId: bindingID}}}},
	}
	server := &Server{control: &workspaceBoundaryControl{resources: resources}}

	repositorySelector := "owner-repository"
	_, repositorySpec, err := server.mutableWorkspaceSpec(context.Background(), "", generated.MutableResourceKindREPOSITORYWORKSPACE, generated.ResourceSpecInput{RepositoryWorkspace: &generated.RepositoryWorkspaceSpec{RepositorySelector: &repositorySelector, WorkspaceMode: "GIT", DefaultBranch: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if repositorySpec.GetRepositoryWorkspace().GetRepositoryRef() != privateRepository {
		t.Fatal("repository selector did not resolve server-owned locator")
	}

	integrationSelector := "owner-integration"
	_, integrationSpec, err := server.mutableWorkspaceSpec(context.Background(), "", generated.MutableResourceKindINTEGRATION, generated.ResourceSpecInput{Integration: &generated.IntegrationSpec{DefinitionRef: "safe-definition", DefinitionVersion: 1, Capabilities: []string{}, CredentialBindingSelectors: []string{}, SourceSelector: &integrationSelector}})
	if err != nil {
		t.Fatal(err)
	}
	if integrationSpec.GetIntegration().GetEndpointRef() != privateEndpoint {
		t.Fatal("integration selector did not resolve server-owned locator")
	}

	sourceKind := generated.CredentialBindingSourceKindPROVIDERCONNECTIONREFERENCE
	providerSelector := "provider-selector"
	_, credentialSpec, err := server.mutableWorkspaceSpec(context.Background(), "", generated.MutableResourceKindCREDENTIALBINDING, generated.ResourceSpecInput{CredentialBinding: &generated.CredentialBindingSpec{Purpose: "provider", Revision: 2, SourceKind: &sourceKind, SourceSelector: &providerSelector}})
	if err != nil {
		t.Fatal(err)
	}
	if credentialSpec.GetCredentialBinding().GetImmutableSecretRef() != privateSecret {
		t.Fatal("credential selector did not resolve server-owned secret reference")
	}
	if _, err := server.resolveWorkspaceSelector(context.Background(), controlplanev1.ResourceKind_RESOURCE_KIND_REPOSITORY_WORKSPACE, resources[0].GetId()); err == nil {
		t.Fatal("raw relationship UUID was accepted as a browser selector")
	}
}

func TestGatewayProjectsStoredConfigurationDriftWithoutInference(t *testing.T) {
	t.Parallel()
	projection, err := projectionOwnership(&controlplanev1.ConfigurationOwnership{
		ManagedBy:      controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT,
		SourceRef:      "git://owner/configuration/role",
		SourceRevision: 8,
		SourceSha256:   strings.Repeat("a", 64),
		Drift:          controlplanev1.ConfigurationDrift_CONFIGURATION_DRIFT_DRIFTED,
	}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Drift != generated.ConfigurationDriftDRIFTED || projection.Revision != 8 {
		t.Fatalf("gateway changed stored drift readback: %#v", projection)
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

func TestBotCatalogAcceptsUnboundIdentityButBindingRequiresGeneration(t *testing.T) {
	now := timestamppb.Now()
	identity := &interactiongatewayv1.AgentMattermostBotIdentityView{
		Selector: "bot-selector", Username: "agent-bot", DisplayName: "Agent bot",
		Status:          interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_AVAILABLE,
		ProviderVersion: 2, ProviderSnapshotSha256: strings.Repeat("a", 64), ObservedAt: now, UpdatedAt: now,
	}
	projected, err := castBotIdentity(identity)
	if err != nil || projected.ProviderGeneration != 0 {
		t.Fatalf("unbound catalog identity was rejected: projection=%#v err=%v", projected, err)
	}
	if _, err = castBotBinding(&interactiongatewayv1.AgentMattermostBotIdentityBindingView{
		AgentRef: "agent-safe", AgentVersion: 1, Identity: identity,
		ReceiptSha256: strings.Repeat("b", 64), UpdatedAt: now,
	}); err == nil {
		t.Fatal("bound identity accepted zero provider generation")
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
