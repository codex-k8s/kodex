package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalProjectAssistantRuntime creates the approved Connection purpose through an actual
// assistant plan, then selects personal mode through owner commands. No raw origin/grant/readiness
// SQL bypasses are used. The helper proves missing-key refusal before a turn and exact personal
// root lineage through runtime and Begin RPC; provider/TLS observations remain synthetic.
// assertPersonalProjectAssistantRuntime создаёт согласованный Connection purpose настоящим
// assistant plan и выбирает personal mode командами владельца. Прямого SQL обхода origin/grant/
// readiness нет. Проверяются missing-key refusal до turn и точная personal root lineage через
// runtime и Begin RPC; provider/TLS observations остаются синтетическими.
func assertPersonalProjectAssistantRuntime(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-project-")
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Personal project assistant", Language: "ru"}).Project
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Personal helper", Purpose: "Read a scoped synthetic project", Instructions: "Use only the initiator's permitted tools."}).ProjectAssistant
	setupConversation := execute(command.CreateAssistantConversation, owner, "setup-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	setupTurn := execute(command.AddAssistantTurn, owner, "setup-turn", nil, command.AssistantTurnInput{ConversationRef: setupConversation.Ref, Content: "Prepare synthetic project connection", DeliveryMode: "QUEUE"}).Conversation
	setupLeases := execute(command.ClaimExecution, worker, "setup-claim", nil, command.LeaseInput{WorkloadInstance: "personal-project-setup", Limit: 1}).RuntimeItems
	if len(setupLeases) != 1 {
		t.Fatal("project personal setup runtime was not claimed")
	}
	lease := setupLeases[0]
	plan := execute(command.ProposeAssistantPlan, worker, "setup-plan", nil, command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Summary: "Prepare synthetic personal connection", Operations: []entity.AssistantPlanOperation{{Key: "connection", Type: prepareProjectAssistantConnection, Title: "Synthetic project connection", Summary: "Confirm scoped project metadata access", Parameters: map[string]any{"projectAssistantRef": profile.AgentRef, "definitionKey": "gitlab", "name": "Personal project GitLab", "publicConfiguration": map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/personal-project"}}}}}).Plan
	plan = execute(command.ValidateAssistantPlan, owner, "setup-validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}).Plan
	applied := execute(command.ApplyAssistantPlan, owner, "setup-apply", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
	if applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 1 {
		t.Fatal("personal project connection purpose was not approved")
	}
	connection, err := service.GetIntegrationConnection(ctx, owner, applied.PlanReceipt.Operations[0].ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	setupRun, err := service.GetRun(ctx, owner, setupTurn.Turns[0].RunRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "setup-stop", &setupRun.Version, command.RunCommandInput{RunRef: setupRun.Ref})
	disabled := execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
	if disabled == nil {
		t.Fatal("personal project connection was not disabled")
	}
	policyOwner := owner
	policyOwner.Permission = "platform.integrations.credential-policy.set"
	selected := execute(command.SetIntegrationCredentialPolicy, policyOwner, "policy", &disabled.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
	if selected == nil {
		t.Fatal("personal project policy was not selected")
	}
	enabled := execute(command.SetConnectionEnabled, owner, "enable", &selected.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
	if enabled == nil {
		t.Fatal("personal project connection was not enabled")
	}
	candidates, err := service.GetProjectAssistantIntegrationGrantCandidates(ctx, owner, project.Ref, connection.Ref, "gitlab.project.metadata.read", query.Page{Size: 10})
	if err != nil || len(candidates.Items) != 1 {
		t.Fatal("project personal connection purpose was filtered by shared readiness", err)
	}
	granted := execute(command.ChangeProjectAssistantIntegrationGrant, owner, "grant", &enabled.Version, command.ProjectAssistantIntegrationGrantInput{AssistantRef: profile.AgentRef, Grant: command.SystemAssistantIntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "gitlab.project.metadata.read", Enabled: true, ApprovalPolicy: "NONE"}}).Connection
	if granted == nil || granted.CredentialRevision != nil || granted.State == "CONNECTED" {
		t.Fatal("project grant manufactured shared credentials")
	}
	reader := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-project-reader", "Personal planned reader")
	granted = execute(command.ChangeIntegrationGrant, owner, "reader-grant", &granted.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: reader.Ref, CapabilityKey: "gitlab.project.metadata.read", Enabled: true, ApprovalPolicy: "NONE"}).Connection
	if granted == nil {
		t.Fatal("planned personal reader grant is absent")
	}
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref, Context: entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: reader.Ref}}).Conversation
	var runsBefore, turnsBefore, runsAfter, turnsAfter int64
	if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.runs),(SELECT count(*) FROM control_plane.session_turns)").Scan(&runsBefore, &turnsBefore); err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-project-missing-key"}, Payload: command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic blocked project personal turn", DeliveryMode: "QUEUE"}})
	var preflight *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &preflight) || len(preflight.Issues) != 1 || preflight.Issues[0].Problem != entity.IntegrationCredentialMissing {
		t.Fatal("project assistant borrowed another Connection's key", err)
	}
	if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.runs),(SELECT count(*) FROM control_plane.session_turns)").Scan(&runsAfter, &turnsAfter); err != nil || runsBefore != runsAfter || turnsBefore != turnsAfter {
		t.Fatal("blocked project turn committed execution state", err)
	}
	self := owner
	self.Permission = "platform.personal-integrations.create"
	created, err := service.Execute(ctx, command.Command{Kind: command.CreatePersonalIntegrationCredential, Principal: self, Mutation: value.Mutation{IdempotencyKey: "personal-project-binding"}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: granted.Version}})
	if err != nil || created.PersonalIntegrationCredential == nil {
		t.Fatal(err)
	}
	self.Permission = "platform.personal-integrations.configure"
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-project-key", ExpectedVersion: &created.PersonalIntegrationCredential.Version}, connection.Ref, granted.Version, []byte("synthetic-project-assistant-key")); err != nil {
		t.Fatal(err)
	}
	binding := verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connection.Ref, "personal-project-key")
	turn := execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic project personal read", DeliveryMode: "QUEUE"}).Conversation
	if turn == nil || len(turn.Turns) != 1 {
		t.Fatal("project personal turn was not created")
	}
	claimed := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "personal-project-worker", Limit: 1}).RuntimeItems
	if len(claimed) != 1 || stringMap(claimed[0], "runRef") != turn.Turns[0].RunRef {
		t.Fatal("project personal runtime was not claimed")
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": turn.Turns[0].RunRef, "node_ref": stringMap(claimed[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-project-read"}, map[string]any{})
	if err != nil {
		t.Fatal("resolve project personal invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-project-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
		t.Fatal("project claim selected another credential or intent", err)
	}
	request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-project-gateway"}
	response, err := personalRuntimeRPCClient(t, workers).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
	if err != nil || !response.GetAdmitted() || response.GetCredentialRevision().GetRef() != binding.Credential.Ref {
		t.Fatal("project assistant Begin lost the root user's key", err)
	}
	var initiator string
	if err := repository.pool.QueryRow(ctx, `SELECT root.initiated_by::text FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
WHERE invocation.ref=$1 AND root.initiated_by=pin.subject_id`, request.InvocationRef).Scan(&initiator); err != nil || initiator != owner.ActorID {
		t.Fatal("project assistant immutable pin substituted its root initiator", err)
	}
	summary := string(asJSON(map[string]any{"id": 4, "path_with_namespace": "fixture/personal-project", "default_branch": "main", "visibility": "private", "archived": false}))
	if _, err := workers.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-project-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:4", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
		t.Fatal("complete project personal effect", err)
	}
	launchProposal, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "personal-project-launch-plan"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(claimed[0], "leaseRef"), Fence: stringMap(claimed[0], "fence"), Generation: runtimeRevisionMapInt64(claimed[0], "generation"), Summary: "Launch the approved personal reader", Operations: []entity.AssistantPlanOperation{{Key: "launch", Type: "LAUNCH_RUN", Title: "Synthetic personal launch", Summary: "Launch only with the initiator's usable key", Action: "EXECUTE", Selected: true, Target: entity.AssistantPlanTarget{Kind: "AGENT", Ref: reader.Ref, Name: reader.Name}, Before: map[string]any{}, After: map[string]any{}, Parameters: map[string]any{"projectRef": project.Ref, "targetType": "AGENT", "targetRef": reader.Ref, "title": "Synthetic planned read", "task": "Read scoped synthetic project metadata", "input": map[string]any{}}}}}})
	if err != nil {
		stage, category, index, _ := errs.AssistantPlanDiagnostic(err)
		t.Fatalf("planned personal launch proposal failed: stage=%s category=%s index=%d field=%s", stage, category, index, errs.AssistantPlanField(err))
	}
	launchPlan := launchProposal.Plan
	if launchPlan == nil {
		t.Fatal("project personal launch plan is absent")
	}
	self.Permission = "platform.personal-integrations.revoke"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: self, Mutation: value.Mutation{IdempotencyKey: "personal-project-revoke-before-validation", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}}); err != nil {
		t.Fatal(err)
	}
	var planVersionBefore, planVersionAfter int64
	var planStateBefore, planStateAfter string
	if err := repository.pool.QueryRow(ctx, "SELECT version,state FROM control_plane.assistant_plans WHERE ref=$1", launchPlan.Ref).Scan(&planVersionBefore, &planStateBefore); err != nil {
		t.Fatal(err)
	}
	validation := command.Command{Kind: command.ValidateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-project-launch-validate", ExpectedVersion: &launchPlan.Version}, Payload: command.AssistantPlanInput{PlanRef: launchPlan.Ref, Revision: launchPlan.Revision}}
	_, err = service.Execute(ctx, validation)
	var launchFailure *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &launchFailure) || len(launchFailure.Issues) != 1 || launchFailure.Issues[0].Problem != entity.IntegrationCredentialRevoked {
		t.Fatal("planned launch hid the complete personal preflight behind a generic runtime error", err)
	}
	if err := repository.pool.QueryRow(ctx, "SELECT version,state FROM control_plane.assistant_plans WHERE ref=$1", launchPlan.Ref).Scan(&planVersionAfter, &planStateAfter); err != nil || planVersionBefore != planVersionAfter || planStateBefore != planStateAfter {
		t.Fatal("refused personal validation changed the stored plan", err)
	}
	self.Permission = "platform.personal-integrations.get"
	revoked, err := service.GetPersonalIntegrationCredential(ctx, self, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	self.Permission = "platform.personal-integrations.configure"
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-project-plan-new-key", ExpectedVersion: &revoked.Version}, connection.Ref, revoked.ConnectionVersion, []byte("synthetic-project-plan-new-key")); err != nil {
		t.Fatal(err)
	}
	binding = verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connection.Ref, "personal-project-plan-new-key")
	validated, err := service.Execute(ctx, validation)
	if err != nil || validated.Plan == nil || validated.Plan.State != "VALID" {
		t.Fatal("personal readback and verification did not allow explicit re-validation", err)
	}
	launched := execute(command.ApplyAssistantPlan, owner, "launch-apply", &validated.Plan.Version, command.AssistantPlanInput{PlanRef: validated.Plan.Ref, Revision: validated.Plan.Revision})
	if launched.Plan == nil || launched.Plan.State != "APPLIED" || launched.PlanReceipt == nil || len(launched.PlanReceipt.Operations) != 1 {
		t.Fatal("approved personal plan did not launch the reader")
	}
	run, err := service.GetRun(ctx, owner, turn.Turns[0].RunRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref})
	plannedRun, err := service.GetRun(ctx, owner, launched.PlanReceipt.Operations[0].ResourceRef)
	if err != nil || plannedRun.Target.Ref != reader.Ref || plannedRun.SessionRef == conversation.SessionRef {
		t.Fatal("approved launch replaced its initiator session or target", err)
	}
	readerWork := execute(command.ClaimExecution, worker, "planned-runtime", nil, command.LeaseInput{WorkloadInstance: "personal-planned-worker", Limit: 1}).RuntimeItems
	if len(readerWork) != 1 || stringMap(readerWork[0], "runRef") != plannedRun.Ref {
		t.Fatal("planned personal reader was not claimed")
	}
	plannedInvocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": plannedRun.Ref, "node_ref": stringMap(readerWork[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-planned-read"}, map[string]any{})
	if err != nil {
		t.Fatal("planned personal invocation lost root eligibility", err)
	}
	plannedWork, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-planned-gateway", 1)
	if err != nil || len(plannedWork) != 1 || stringMap(plannedWork[0], "invocationRef") != stringMap(plannedInvocation, "invocationRef") {
		t.Fatal("planned personal invocation was not claimed", err)
	}
	plannedRequest := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(plannedWork[0], "invocationRef"), LeaseRef: stringMap(plannedWork[0], "leaseRef"), Fence: stringMap(plannedWork[0], "fence"), Generation: runtimeRevisionMapInt64(plannedWork[0], "generation"), WorkloadInstance: "personal-planned-gateway"}
	plannedResponse, err := personalRuntimeRPCClient(t, workers).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), plannedRequest)
	if err != nil || !plannedResponse.GetAdmitted() || plannedResponse.GetCredentialRevision().GetRef() != binding.Credential.Ref || plannedResponse.GetCredentialRevision().GetRef() == response.GetCredentialRevision().GetRef() {
		t.Fatal("planned reader reused the revoked key instead of the verified replacement", err)
	}
	if _, err := workers.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-planned-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: plannedRequest.InvocationRef, LeaseRef: plannedRequest.LeaseRef, Fence: plannedRequest.Fence, Generation: plannedRequest.Generation, Success: true, EffectKey: stringMap(plannedWork[0], "effectKey"), InputDigest: stringMap(plannedWork[0], "inputDigest"), ProviderEffectRef: "gitlab-project:4", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
		t.Fatal("complete planned personal effect", err)
	}
	plannedRun, err = service.GetRun(ctx, owner, plannedRun.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "planned-cancel", &plannedRun.Version, command.RunCommandInput{RunRef: plannedRun.Ref})
}
