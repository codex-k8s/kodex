package platform

import (
	"context"
	_ "embed"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryFixtureIsolateAccess removes only competing grants of the selected original-actor permission in a disposable fixture; it never modifies leases, intents or execution evidence.
// queryFixtureIsolateAccess удаляет лишь конкурирующие grants выбранного разрешения исходного actor в disposable fixture; leases, intents либо execution evidence не меняются.
//
//go:embed testdata/sql/ssh_execution_test__isolate_acl.sql
var queryFixtureIsolateAccess string

// querySSHExecutionTestRestoreACL restores exact fixture binding IDs after the native deadline observation, without changing their conditions or manufacturing an owner receipt.
// querySSHExecutionTestRestoreACL восстанавливает exact fixture binding IDs после native deadline observation без изменения conditions либо создания owner receipt.
//
//go:embed testdata/sql/ssh_execution_test__restore_acl.sql
var querySSHExecutionTestRestoreACL string

// querySSHExecutionTestPublication observes only the addressed invocation and durable publication cardinality after a native savepoint rollback.
// querySSHExecutionTestPublication наблюдает лишь addressed invocation и durable publication cardinality после native savepoint rollback.
//
//go:embed testdata/sql/ssh_execution_test__publication.sql
var querySSHExecutionTestPublication string

// TestSSHExecutionAuthorityDeadlineOwnerComponent delays successful native COMMIT acknowledgements beyond a real finite ACL; neither Begin/Get credit nor late Complete may extend the original bounded attempt even after a new permission is available.
// TestSSHExecutionAuthorityDeadlineOwnerComponent задерживает successful native COMMIT ACK за real finite ACL; ни Begin/Get credit, ни late Complete не продлевают original bounded attempt даже при появлении нового permission.
func TestSSHExecutionAuthorityDeadlineOwnerComponent(t *testing.T) {
	for _, phase := range []string{"begin", "get", "complete", "finish"} {
		t.Run(phase, func(t *testing.T) {
			f, binding := prepareSSHExecutionDeadlineFixture(t)
			until, restore := isolateSSHExecutionFiniteACL(t, f)
			trace := &sshExecutionDeadlineTrace{until: until, boundary: querySSHExecutionInsert}
			broker := f.broker
			broker.Permission = "platform.runtime.ssh.execution.begin"
			if phase != "begin" {
				admission, err := f.repository.BeginSSHExecution(f.ctx, broker, binding)
				if err != nil || admission.Validate(time.Now()) != nil || time.Until(until) < time.Second {
					t.Fatal("prepare live original finite SSH intent", err)
				}
				if phase == "complete" || phase == "finish" {
					if admission.ExpiresAt.After(until) {
						t.Error("SSH intent exceeded the original finite permission")
					}
					restore()
					ownerRepository := f.repository
					observation := managedssh.ExecutionObservation{Outcome: managedssh.ExecutionFailed}
					if phase == "complete" {
						timer := time.NewTimer(max(0, time.Until(until)+100*time.Millisecond))
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-f.ctx.Done():
							t.Fatal("late SSH completion caller expired")
						}
					} else {
						trace.boundary, trace.queryResult = queryWorkersCompleteintegrationinvocationInsertEffectReceipt, true
						config := f.pool.Config()
						config.ConnConfig.Tracer = trace
						pool, err := pgxpool.NewWithConfig(f.ctx, config)
						if err != nil {
							t.Fatal("open native SSH publication observer", err)
						}
						defer pool.Close()
						copyRepository := *f.repository
						copyRepository.pool = pool
						ownerRepository = &copyRepository
						result := []byte(`{"observed":"synthetic"}`)
						observation = managedssh.ExecutionObservation{Outcome: managedssh.ExecutionSucceeded, Result: result, ResultDigest: sshHealthHash(result)}
					}
					broker.Permission = "platform.runtime.ssh.execution.complete"
					receipt, err := ownerRepository.CompleteSSHExecution(f.ctx, broker, command.SSHExecutionComplete{Binding: binding,
						AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest,
						Observation: observation})
					if err != nil || !receipt.Recorded || receipt.Applied || receipt.State != "DISCARDED" || len(receipt.Observation.Result) != 0 {
						t.Error("new permission revived an expired original SSH attempt", err)
					}
					if f.ctx.Err() != nil {
						t.Fatal("late SSH completion was not observed with a live caller")
					}
					if phase == "finish" {
						var state, summary string
						var publications int
						current, err := f.repository.resolveScope(f.ctx, f.owner)
						if err != nil {
							t.Fatal("resolve original SSH publication observer", err)
						}
						if err := f.pool.QueryRow(f.ctx, querySSHExecutionTestPublication, current.organizationID, binding.InvocationRef).Scan(&state, &summary, &publications); err != nil ||
							state != "RUNNING" || summary != "" || publications != 0 || trace.delayed.Load() != 1 {
							t.Fatal("late SSH publication leaked a partial effect receipt or terminal state", err)
						}
					}
					return
				}
				trace.boundary = querySSHExecutionLease
			}
			config := f.pool.Config()
			config.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(f.ctx, config)
			if err != nil {
				t.Fatal("open native SSH execution deadline observer", err)
			}
			defer pool.Close()
			repository := *f.repository
			repository.pool = pool
			if phase == "begin" {
				admission, err := repository.BeginSSHExecution(f.ctx, broker, binding)
				if !errors.Is(err, errs.ErrForbidden) || admission.AttemptRef != "" || len(admission.CanonicalInput) != 0 {
					t.Error("expired finite ACL returned SSH dispatch admission")
				}
			} else {
				broker.Permission = "platform.runtime.ssh.execution.get"
				receipt, err := repository.GetSSHExecution(f.ctx, broker, binding)
				if err != nil || receipt.Recorded || receipt.State != "UNREPORTED" || !receipt.AuthorizedUntil.IsZero() {
					t.Error("expired finite ACL returned SSH pre-Start credit", err)
				}
			}
			if trace.delayed.Load() != 1 || f.ctx.Err() != nil || time.Now().Before(until) {
				t.Fatal("native SSH execution expiry boundary was not observed with a live caller")
			}
		})
	}
}

// prepareSSHExecutionDeadlineFixture creates and claims one real public READ invocation before narrowing the original actor's current authority; no execution intent or provider effect is manufactured.
// prepareSSHExecutionDeadlineFixture создаёт и принимает real public READ invocation до сужения current authority original actor; execution intent либо provider effect не создаются оснасткой.
func prepareSSHExecutionDeadlineFixture(t *testing.T) (*sshSetupNativeFixture, managedssh.Binding) {
	t.Helper()
	f := newSSHSetupNativeFixture(t)
	authenticateSSHExecutionFixture(t, f)
	seedObservedCatalogFixture(t, f.ctx, f.repository)
	execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-credit-deadline-")
	project := execute(command.CreateProject, f.transport, "project", nil, command.ProjectInput{Name: "Finite SSH dispatch authority", Language: "ru"}).Project
	agent := createLifecycleAgent(t, f.ctx, f.service, f.transport, project.Ref, "ssh-credit-agent", "Finite SSH reader")
	f.connection = *execute(command.ChangeIntegrationGrant, f.transport, "grant", &f.connection.Version,
		command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: agent.Ref, CapabilityKey: "ssh.system.read", ApprovalPolicy: "NONE", Enabled: true}).Connection
	run := execute(command.LaunchRun, f.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
		Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read only under the actual finite SSH authority"}).Run
	worker := resolvedTestPrincipal(t, f.ctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-credit-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("claim exact finite SSH runtime origin")
	}
	invocation, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"),
		"connection_ref": f.connection.Ref, "capability_key": "ssh.system.read", "idempotency_key": "ssh-credit-read"}, map[string]any{})
	if err != nil {
		t.Fatal("resolve finite SSH invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, f.ctx, f.repository)
	claims, err := f.service.ClaimIntegrationInvocationsWithCapacity(f.ctx, gateway, "ssh-credit-gateway", 1, value.IntegrationInvocationDeliveryCapacity{SSH: 1})
	if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(invocation, "invocationRef") {
		t.Fatal("claim exact finite SSH invocation", err)
	}
	claim := claims[0]
	return f, managedssh.Binding{WorkloadInstance: "ssh-credit-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
		Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
}

// isolateSSHExecutionFiniteACL creates a genuine finite view binding and temporarily revokes competing alternatives through the disposable administrator; the real access evaluator still decides eligibility.
// isolateSSHExecutionFiniteACL создаёт genuine finite view binding и временно отзывает competing alternatives через disposable administrator; eligibility определяет настоящий access evaluator.
func isolateSSHExecutionFiniteACL(t *testing.T, f *sshSetupNativeFixture) (time.Time, func()) {
	t.Helper()
	current, err := f.repository.resolveScope(f.ctx, f.owner)
	if err != nil {
		t.Fatal("resolve finite SSH execution actor", err)
	}
	execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-credit-acl-")
	role := execute(command.CreateAccessRole, f.transport, "role", nil, command.AccessRoleInput{Name: "Finite original SSH use",
		PermissionKeys: []string{"run.view", "integration.view", "agent.view", "agent.launch", "project.view"}, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable execution deadline"}).AccessRole
	until := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
	bound := execute(command.CreateAccessBinding, f.transport, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: current.actorRef,
		RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}, Conditions: entity.AccessConditions{ValidUntil: &until}}).AccessBinding
	admin := evidenceRetentionAdministrator(t, f.ctx, f.pool.Config().ConnConfig)
	rows, err := admin.Query(f.ctx, queryFixtureIsolateAccess, current.organizationID, current.actorID, bound.Ref, "integration.view")
	if err != nil {
		t.Fatal("isolate finite SSH execution binding", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("observe isolated SSH execution binding")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) == 0 {
		t.Fatal("finite SSH execution retained an unbounded alternative")
	}
	restored := false
	// restore returns only the original disposable bindings and runs once, including when invoked before the final native read.
	// restore возвращает лишь original disposable bindings и выполняется один раз, включая вызов до final native read.
	restore := func() {
		if restored {
			return
		}
		restored = true
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := admin.Exec(ctx, querySSHExecutionTestRestoreACL, current.organizationID, ids); err != nil {
			t.Error("restore disposable SSH execution binding")
		}
	}
	t.Cleanup(restore)
	return until, restore
}

// sshExecutionDeadlineTrace delays only a successful real COMMIT after the chosen fixed owner query; it never changes rows, clocks, arguments or returned authority.
// sshExecutionDeadlineTrace задерживает лишь successful real COMMIT после выбранного fixed owner query; rows, clocks, arguments либо returned authority не меняются.
type sshExecutionDeadlineTrace struct {
	boundary    string
	until       time.Time
	queryResult bool
	armed       atomic.Bool
	delayed     atomic.Int32
}

// sshExecutionQueryBoundary carries only fixed query identity flags between pgx Start/End; neither SQL nor private arguments enter the trace context.
// sshExecutionQueryBoundary передаёт лишь fixed query identity flags между pgx Start/End; SQL либо private arguments не входят в trace context.
type sshExecutionQueryBoundary struct{ owner, commit bool }

// TraceQueryStart preserves the caller lifetime and attaches only fixed query flags for the corresponding QueryEnd callback.
// TraceQueryStart сохраняет caller lifetime и прикрепляет лишь fixed query flags для соответствующего QueryEnd callback.
func (trace *sshExecutionDeadlineTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, trace, sshExecutionQueryBoundary{owner: data.SQL == trace.boundary, commit: data.SQL == "commit"})
}

// TraceQueryEnd waits past the finite permission after the addressed successful COMMIT or fixed publication query; cancellation still bounds the observer and query outcomes stay unchanged.
// TraceQueryEnd ждёт конца finite permission после addressed successful COMMIT либо fixed publication query; отмена ограничивает observer, query outcomes не меняются.
func (trace *sshExecutionDeadlineTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	boundary, _ := ctx.Value(trace).(sshExecutionQueryBoundary)
	if boundary.owner && data.Err == nil {
		if trace.queryResult {
			trace.wait(ctx)
			return
		}
		trace.armed.Store(true)
	}
	if !boundary.commit || data.Err != nil || !trace.armed.CompareAndSwap(true, false) {
		return
	}
	trace.wait(ctx)
}

// wait crosses the real finite permission boundary within the existing caller context, without changing database time or returning an invented SQL outcome.
// wait пересекает real finite permission boundary внутри existing caller context без изменения database time либо придуманного SQL outcome.
func (trace *sshExecutionDeadlineTrace) wait(ctx context.Context) {
	trace.delayed.Add(1)
	timer := time.NewTimer(max(0, time.Until(trace.until)+100*time.Millisecond))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
