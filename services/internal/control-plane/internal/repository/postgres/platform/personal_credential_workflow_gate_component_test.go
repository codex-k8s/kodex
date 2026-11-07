package platform

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalWorkflowGateInitiator runs two published server steps separated by a real
// result Human Gate. A second authorized user with a distinct ready personal credential
// approves the Gate, but the next physical run must retain the original root user's key.
// A newly constructed domain service and the actual Begin gRPC exercise durable owner state;
// transport peer admission, provider health/output and antivirus observations stay synthetic.
// This is not a live model, external API, process restart or certificate validation test.
// assertPersonalWorkflowGateInitiator исполняет два опубликованных server шага через
// настоящий result Human Gate. Другой допущенный пользователь с отдельным READY ключом
// одобряет Gate; следующий physical run сохраняет ключ исходного root user. Новый domain
// service и настоящий Begin gRPC проверяют durable owner state; transport peer admission,
// provider health/output и antivirus observations синтетические. Это не live model,
// внешний API, process restart либо проверка сертификата.
func assertPersonalWorkflowGateInitiator(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connectionRef string) {
	t.Helper()
	configureComponentArtifactScanner(t, repository)
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.personal-integrations.configure"
	binding, err = service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-gate-owner-replace", ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-gate-owner-key"))
	if err != nil {
		t.Fatal(err)
	}
	binding = verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connectionRef, "personal-gate-owner")
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: repository.pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	project := f.execute(command.CreateProject, "personal-gate-project", nil, command.ProjectInput{Name: "Personal workflow Gate lineage", Language: "ru"}).Project
	if project == nil {
		t.Fatal("missing personal Gate project")
	}
	f.projectRef = project.Ref
	second, secondRef := previewAuthorityActor(t, ctx, repository, service, owner, "personal-gate-reviewer",
		[]string{"project.view", "workflow.view", "run.view", "gate.resolve"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	f.execute(command.AddPlatformMembership, "personal-gate-reviewer-platform", nil, command.PlatformMembershipInput{UserRef: secondRef, Role: "MEMBER", Active: true})
	f.execute(command.AddMembership, "personal-gate-reviewer-project", nil, command.MembershipInput{ProjectRef: project.Ref, UserRef: secondRef, Permissions: []string{"VIEW", "RESOLVE_GATES"}, Active: true})
	previewAuthorityGrant(t, ctx, service, owner, "personal-gate-reviewer-integration", secondRef, []string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	second.Permission = "platform.personal-integrations.create"
	created, err := service.Execute(ctx, command.Command{Kind: command.CreatePersonalIntegrationCredential, Principal: second, Mutation: value.Mutation{IdempotencyKey: "personal-gate-reviewer-binding"}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef, ExpectedConnectionVersion: connection.Version}})
	if err != nil || created.PersonalIntegrationCredential == nil {
		t.Fatal("create Gate reviewer's separate personal binding", err)
	}
	other := *created.PersonalIntegrationCredential
	second.Permission = "platform.personal-integrations.configure"
	other, err = service.ConfigurePersonalIntegrationCredential(ctx, second, value.Mutation{IdempotencyKey: "personal-gate-reviewer-key", ExpectedVersion: &other.Version}, connectionRef, connection.Version, []byte("synthetic-gate-reviewer-key"))
	if err != nil {
		t.Fatal("configure Gate reviewer's independent key", err)
	}
	other = verifyPersonalAssistantKey(t, ctx, repository, service, workers, second, connectionRef, "personal-gate-reviewer")
	if other.Credential == nil || other.Credential.Ref == binding.Credential.Ref {
		t.Fatal("Gate reviewer lacks a distinct personal key")
	}

	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-gate-manager", "Personal Gate coordinator")
	f.managerRef = manager.Ref
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Personal Gate lineage", Purpose: "Preserve original initiator across an independently approved result", CoordinatorAgentRef: manager.Ref, Concurrency: 1, TimeoutSeconds: 240, ResultSchema: map[string]any{}}
	for index, key := range []string{"first", "second"} {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-gate-reader-"+key, "Personal "+key)
		agent = *f.execute(command.ChangeAgentCapability, "personal-gate-artifact-"+key, &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		f.agents[key] = agent.Ref
		connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
		if err != nil {
			t.Fatal(err)
		}
		f.execute(command.ChangeIntegrationGrant, "personal-gate-grant-"+key, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true})
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agent.Ref, Instructions: "Read synthetic metadata and return a report", TimeoutSeconds: 90, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability, "gitlab.project.metadata.read"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: key + ".md", MediaType: "text/markdown"}}}
		if index == 0 {
			step.HumanGateAfter = true
			step.GateDecisions = []string{"APPROVE", "REJECT", "CANCEL"}
		} else {
			step.DependsOn = []string{"first"}
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := f.execute(command.CreateWorkflow, "personal-gate-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "personal-gate-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "personal-gate-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow

	f.runRef = f.execute(command.LaunchRun, "personal-gate-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Read two synthetic reports across a reviewed Gate"}).Run.Ref
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	call := func(key string, lease map[string]any) {
		t.Helper()
		invocation, err := f.service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-gate-" + key}, map[string]any{})
		if err != nil {
			t.Fatal("resolve workflow personal invocation", err)
		}
		work, err := f.service.ClaimIntegrationInvocations(ctx, gateway, "personal-gate-gateway", 1)
		if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
			t.Fatal("claim workflow personal invocation", err)
		}
		request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-gate-gateway"}
		response, err := personalRuntimeRPCClient(t, workers).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		if err != nil {
			t.Fatal("workflow Begin RPC failed", err)
		}
		if !response.GetAdmitted() || response.GetCredentialRevision().GetRef() != binding.Credential.Ref || response.GetCredentialRevision().GetRef() == other.Credential.Ref {
			t.Fatal("Gate reviewer replaced the immutable root credential", err)
		}
		var subject, root string
		if err := repository.pool.QueryRow(ctx, `SELECT pin.subject_id::text,root.ref FROM control_plane.integration_personal_invocation_credentials pin
     JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
     JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.initiated_by=pin.subject_id
     WHERE invocation.ref=$1`, request.InvocationRef).Scan(&subject, &root); err != nil || subject != owner.ActorID || root != f.runRef {
			t.Fatal("workflow invocation lost root lineage", err)
		}
		summary := string(asJSON(map[string]any{"id": 4, "path_with_namespace": "fixture/gate-project", "default_branch": "main", "visibility": "private", "archived": false}))
		if _, err := f.service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-gate-complete-" + key}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:4", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
			t.Fatal("complete workflow personal invocation", err)
		}
	}
	first := f.claims("personal-gate-first")
	if len(first) != 1 || stringMap(first[0], "agentRef") != f.agents["first"] {
		t.Fatal("missing first personal workflow step")
	}
	call("first", first[0])
	f.complete("personal-gate-first-complete", first[0], "first.md", true)
	current, err := service.GetRun(ctx, owner, f.runRef)
	if err != nil || len(current.GateRefs) != 1 {
		t.Fatal("personal workflow did not wait at its exact result Gate", err)
	}
	gate, err := service.GetOwnerGate(ctx, owner, current.GateRefs[0])
	if err != nil || gate.State != "OPEN" {
		t.Fatal("missing personal result Gate", err)
	}
	if claims := f.claims("personal-gate-wait"); len(claims) != 0 {
		t.Fatal("dependent personal workflow ran before owner review")
	}
	approval := command.Command{Kind: command.ResolveOwnerGate, Principal: second, Mutation: value.Mutation{IdempotencyKey: "personal-gate-second-approve", ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Accept the first report without taking over its credentials"}}
	if _, err := service.Execute(ctx, approval); err != nil {
		t.Fatal("second user could not approve the scoped Gate", err)
	}
	if _, err := service.Execute(ctx, approval); err != nil {
		t.Fatal("result Gate replay lost its receipt", err)
	}
	rebuilt, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	f.service = rebuilt
	workers = rebuilt
	next := f.claims("personal-gate-second")
	if len(next) != 1 || stringMap(next[0], "agentRef") != f.agents["second"] {
		t.Fatal("missing dependent personal workflow after independent approval")
	}
	call("second", next[0])
	f.complete("personal-gate-second-complete", next[0], "second.md", true)
	final, err := rebuilt.GetRun(ctx, owner, f.runRef)
	if err != nil || final.State != "SUCCEEDED" {
		t.Fatal("personal workflow did not preserve final owner acceptance", err)
	}
}
