package platform

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSSHExecutionReceiptScopeOwnerComponent proves shared SERVICE custody does not grant another original actor or project access to an invocation's immutable recipe; a fresh same-actor/project READ remains valid across runs.
// TestSSHExecutionReceiptScopeOwnerComponent доказывает, что shared SERVICE custody не даёт другому original actor либо project доступ к immutable recipe invocation; fresh same-actor/project READ допустим между runs.
func TestSSHExecutionReceiptScopeOwnerComponent(t *testing.T) {
	f := newSSHSetupNativeFixtureWithPolicy(t, true)
	t.Run("shared-setup-requires-organization-manage", func(t *testing.T) {
		assertSSHSharedSetupOrganizationAuthority(t, f)
	})
	authenticateSSHExecutionFixture(t, f)
	seedObservedCatalogFixture(t, f.ctx, f.repository)
	execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-receipt-native-")
	first := execute(command.CreateProject, f.transport, "first-project", nil, command.ProjectInput{Name: "Original SSH receipt owner", Language: "ru"}).Project
	second := execute(command.CreateProject, f.transport, "second-project", nil, command.ProjectInput{Name: "Other SSH receipt project", Language: "ru"}).Project
	originalAgent := createLifecycleAgent(t, f.ctx, f.service, f.transport, first.Ref, "ssh-receipt-write-agent", "Original receipt writer")
	readerAgent := createLifecycleAgent(t, f.ctx, f.service, f.transport, first.Ref, "ssh-receipt-read-agent", "Same owner receipt reader")
	projectAgent := createLifecycleAgent(t, f.ctx, f.service, f.transport, second.Ref, "ssh-receipt-project-agent", "Other project receipt reader")
	actorAgent := createLifecycleAgent(t, f.ctx, f.service, f.transport, first.Ref, "ssh-receipt-actor-agent", "Other actor receipt reader")
	for _, grant := range []struct{ agent, operation, policy string }{
		{originalAgent.Ref, "ssh.file.create", "HUMAN_EACH_EFFECT"}, {readerAgent.Ref, "ssh.receipt.read", "NONE"},
		{projectAgent.Ref, "ssh.receipt.read", "NONE"}, {actorAgent.Ref, "ssh.receipt.read", "NONE"},
	} {
		f.connection = *execute(command.ChangeIntegrationGrant, f.transport, grant.agent, &f.connection.Version,
			command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: grant.agent, CapabilityKey: grant.operation, ApprovalPolicy: grant.policy, Enabled: true}).Connection
	}
	// Existing shared custody admits grant changes; a fresh health test then observes their final Connection version without another material write.
	// Existing shared custody допускает grant changes; fresh health test затем наблюдает final Connection version без повторной записи material.
	testSSHExecutionFixtureHealth(t, f, "ssh-receipt-current-health")
	worker := resolvedTestPrincipal(t, f.ctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, f.ctx, f.repository)
	// resolve creates a genuine owner run and RuntimeRevision before resolving one exact registered capability.
	// resolve создаёт настоящий owner run и RuntimeRevision до resolve одной exact registered capability.
	var originalRun string
	resolve := func(t *testing.T, actor value.Principal, project, agent, key, operation string, input map[string]any) map[string]any {
		t.Helper()
		run := execute(command.LaunchRun, actor, key+"-launch", nil, command.LaunchRunInput{ProjectRef: project,
			Target: entity.RunTarget{Type: "AGENT", Ref: agent}, Task: "Use only the original synthetic SSH receipt"}).Run
		if key == "original" {
			originalRun = run.Ref
		}
		claims := execute(command.ClaimExecution, worker, key+"-runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-receipt-runtime", Limit: 1}).RuntimeItems
		if len(claims) != 1 || stringMap(claims[0], "runRef") != run.Ref {
			t.Fatal("claim receipt runtime origin")
		}
		inv, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(claims[0], "nodeRef"),
			"connection_ref": f.connection.Ref, "capability_key": operation, "idempotency_key": "ssh-receipt-" + key}, input)
		if err != nil {
			t.Fatal("resolve explicit receipt operation", err)
		}
		return inv
	}
	// claim adopts through the only common facade; it cannot assign a new original actor or choose another shared identity.
	// claim принимает через единственную common facade; новый original actor либо другая shared identity не назначаются.
	var originalIdentityRef string
	claim := func(t *testing.T, inv map[string]any) managedssh.Binding {
		t.Helper()
		claims, err := f.service.ClaimIntegrationInvocationsWithCapacity(f.ctx, gateway, "ssh-receipt-gateway", 1, value.IntegrationInvocationDeliveryCapacity{SSH: 1})
		if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(inv, "invocationRef") {
			t.Fatal("claim exact shared receipt invocation", err)
		}
		c := claims[0]
		identity := stringMap(c, "brokerIdentityRef")
		if originalIdentityRef == "" {
			originalIdentityRef = identity
		}
		if identity == "" || identity != originalIdentityRef {
			t.Fatal("shared receipt scenario selected another identity")
		}
		return managedssh.Binding{WorkloadInstance: "ssh-receipt-gateway", InvocationRef: stringMap(c, "invocationRef"), LeaseRef: stringMap(c, "leaseRef"),
			Fence: stringMap(c, "fence"), Generation: runtimeRevisionMapInt64(c, "generation"), InputDigest: stringMap(c, "inputDigest")}
	}
	original := resolve(t, f.transport, first.Ref, originalAgent.Ref, "original", "ssh.file.create", map[string]any{"resource": "files_synthetic", "path": "owned.txt", "content_base64": "dGVzdA=="})
	owner := f.transport
	owner.Permission = "platform.query.owner-gates.get"
	gate, err := f.service.GetOwnerGate(f.ctx, owner, stringMap(original, "gateRef"))
	if err != nil {
		t.Fatal("read original write Gate", err)
	}
	execute(command.ResolveOwnerGate, f.transport, "approve-original", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.execution.begin"
	if _, err := f.repository.BeginSSHExecution(f.ctx, broker, claim(t, original)); err != nil {
		t.Fatal("record original immutable helper recipe", err)
	}
	actorInput := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000002156", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Disposable other SSH receipt actor", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}
	if _, err := f.repository.ResolveProofAuthority(f.ctx, actorInput); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound other receipt actor unexpectedly admitted", err)
	}
	subjects, _, err := f.service.ListAccessSubjects(f.ctx, f.transport, query.Filter{Query: actorInput.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("resolve other receipt actor", err)
	}
	// Explicit view includes the original project/run, so a denial must come from original-owner scope rather than missing general read rights.
	// Explicit view включает original project/run; отказ должен исходить из original-owner scope, а не отсутствия общих read rights.
	receiptAccessBinding(t, f.ctx, f.service, f.transport, subjects[0].Ref, "ssh-receipt-other-actor",
		[]string{"integration.view", "agent.view", "agent.launch", "project.view", "run.view"}, entity.AccessScope{Kind: "ORGANIZATION"})
	other := resolvedTestPrincipal(t, f.ctx, f.repository, actorInput, "control-api-gateway")
	for _, scenario := range []struct {
		name, project, agent string
		actor                value.Principal
		allowed              bool
	}{
		{"same-owner-project", first.Ref, readerAgent.Ref, f.transport, true},
		{"other-project", second.Ref, projectAgent.Ref, f.transport, false},
		{"other-actor", first.Ref, actorAgent.Ref, other, false},
		{"finite-original-view", first.Ref, readerAgent.Ref, f.transport, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reader := resolve(t, scenario.actor, scenario.project, scenario.agent, scenario.name, "ssh.receipt.read", map[string]any{"receipt_invocation_ref": stringMap(original, "invocationRef")})
			binding := claim(t, reader)
			if scenario.name == "finite-original-view" {
				assertSSHReceiptFiniteOriginalView(t, f, binding, originalRun, first.Ref)
				return
			}
			trace := &sshReceiptSourceTrace{}
			cfg := f.pool.Config()
			cfg.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
			if err != nil {
				t.Fatal("open receipt scope trace", err)
			}
			defer pool.Close()
			ownerRepository := *f.repository
			ownerRepository.pool = pool
			admission, err := ownerRepository.BeginSSHExecution(f.ctx, broker, binding)
			if !trace.queried.Load() {
				t.Fatal("receipt negative did not reach original-source owner lookup", err)
			}
			if scenario.allowed {
				if err != nil || admission.ReceiptSource == nil || admission.ReceiptSource.InvocationRef != stringMap(original, "invocationRef") || admission.ReceiptSource.Operation != "ssh.file.create" {
					t.Fatal("fresh same-owner/project READ lost original recipe", err)
				}
			} else if !errors.Is(err, errs.ErrForbidden) || admission.ReceiptSource != nil || len(admission.CanonicalInput) != 0 {
				t.Fatal("shared identity crossed original actor/project receipt boundary", err)
			}
		})
	}
}

// assertSSHReceiptFiniteOriginalView retains unbounded integration use but bounds only the original run.view permission, proving recipe disclosure cannot borrow the reader invocation's longer authority.
// assertSSHReceiptFiniteOriginalView сохраняет unbounded integration use, ограничивая лишь original run.view: recipe disclosure не заимствует более длинную authority reader invocation.
func assertSSHReceiptFiniteOriginalView(t *testing.T, f *sshSetupNativeFixture, binding managedssh.Binding, originalRun, project string) {
	t.Helper()
	current, err := f.repository.resolveScope(f.ctx, f.owner)
	if err != nil {
		t.Fatal("resolve original receipt disclosure actor", err)
	}
	receiptAccessBinding(t, f.ctx, f.service, f.transport, current.actorRef, "ssh-receipt-unbounded-use",
		[]string{"integration.view"}, entity.AccessScope{Kind: "ORGANIZATION"})
	execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-receipt-finite-")
	role := execute(command.CreateAccessRole, f.transport, "role", nil, command.AccessRoleInput{Name: "Finite original SSH receipt view",
		PermissionKeys: []string{"run.view"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Disposable original view bound"}).AccessRole
	until := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
	bound := execute(command.CreateAccessBinding, f.transport, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: current.actorRef,
		RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project, ResourceKind: "RUN", ResourceRef: originalRun},
		Conditions: entity.AccessConditions{ValidUntil: &until}}).AccessBinding
	admin := evidenceRetentionAdministrator(t, f.ctx, f.pool.Config().ConnConfig)
	rows, err := admin.Query(f.ctx, queryFixtureIsolateAccess, current.organizationID, current.actorID, bound.Ref, "run.view")
	if err != nil {
		t.Fatal("isolate original receipt run view", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("observe original receipt view alternatives")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) == 0 {
		t.Fatal("original receipt view retained an unbounded alternative")
	}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := admin.Exec(ctx, querySSHExecutionTestRestoreACL, current.organizationID, ids); err != nil {
			t.Error("restore disposable original receipt view")
		}
	})
	trace := &sshExecutionDeadlineTrace{until: until, boundary: querySSHExecutionInsert}
	config := f.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal("open original receipt deadline observer", err)
	}
	defer pool.Close()
	repository := *f.repository
	repository.pool = pool
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.execution.begin"
	admission, err := repository.BeginSSHExecution(f.ctx, broker, binding)
	if !errors.Is(err, errs.ErrForbidden) || admission.AttemptRef != "" || admission.ReceiptSource != nil || len(admission.CanonicalInput) != 0 || trace.delayed.Load() != 1 || f.ctx.Err() != nil {
		t.Fatal("expired original run.view disclosed a recipe or dispatch grant", err)
	}
}

// assertSSHSharedSetupOrganizationAuthority uses a separate authenticated actor with explicit integration authority but no organization authority; denial must leave no setup receipt or upload permit.
// assertSSHSharedSetupOrganizationAuthority использует отдельного authenticated actor с explicit integration authority без organization authority; отказ не оставляет setup receipt либо upload permit.
func assertSSHSharedSetupOrganizationAuthority(t *testing.T, f *sshSetupNativeFixture) {
	t.Helper()
	input := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000002157", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Disposable SSH integration manager", CallerWorkload: "control-api-gateway", Operation: sshSetupBeginOperation}
	if _, err := f.repository.ResolveProofAuthority(f.ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound SSH integration manager unexpectedly admitted", err)
	}
	subjects, _, err := f.service.ListAccessSubjects(f.ctx, f.transport, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("resolve bounded SSH integration manager", err)
	}
	receiptAccessBinding(t, f.ctx, f.service, f.transport, subjects[0].Ref, "ssh-shared-integration-manager",
		[]string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: f.connection.Ref})
	// Resolve without OwnerClaim: the ordinary active binding must not be upgraded into installation-owner authority by the fixture.
	// Resolve без OwnerClaim: оснастка не повышает ordinary active binding до authority владельца установки.
	authority, err := f.repository.ResolveProofAuthority(f.ctx, input)
	if err != nil {
		t.Fatal("resolve integration-only SSH authority", err)
	}
	actor := value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, Permission: input.Operation,
		CallerWorkload: input.CallerWorkload, CorrelationRef: "ssh-shared-denial-component", CredentialRevision: 1}
	key, version := "ssh-shared-denied-organization", f.connection.Version
	admission, err := f.service.BeginSSHIdentitySetup(f.ctx, actor, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
		command.SSHIdentitySetupBegin{ConnectionRef: f.connection.Ref, ExpectedConnectionVersion: version, ContentSHA256: strings.Repeat("c", 64), ContentBytes: 128})
	if !errors.Is(err, errs.ErrForbidden) || admission.View.Ref != "" || len(admission.Permit) != 0 {
		t.Fatal("integration manager gained shared SERVICE setup authority", err)
	}
	var ref, connection, intent string
	err = f.pool.QueryRow(f.ctx, querySSHSetupBeginReceipt, pgx.StrictNamedArgs{"organization_id": authority.OrganizationID, "actor_id": authority.ActorID, "begin_key": key}).Scan(&ref, &connection, &intent)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("denied shared SSH setup retained a receipt", err)
	}
}

// sshReceiptSourceTrace proves the actual owner lookup was reached; it retains neither arguments nor returned private data.
// sshReceiptSourceTrace доказывает достижение actual owner lookup; arguments и возвращённые private data не сохраняются.
type sshReceiptSourceTrace struct{ queried atomic.Bool }

// TraceQueryStart records only identity of the fixed receipt query, leaving every SQL argument and outcome unchanged.
// TraceQueryStart отмечает лишь identity fixed receipt query, не изменяя SQL arguments либо outcome.
func (trace *sshReceiptSourceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == querySSHExecutionReceiptSource {
		trace.queried.Store(true)
	}
	return ctx
}

// TraceQueryEnd does not inspect or retain database result data.
// TraceQueryEnd не читает и не сохраняет database result data.
func (*sshReceiptSourceTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
