package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesCaptureACLRefSQL reads only one sealed fixture's canonical capture locator.
// kubernetesCaptureACLRefSQL читает canonical capture locator одного sealed fixture.
//
//go:embed testdata/sql/kubernetes_capture_acl_ref.sql
var kubernetesCaptureACLRefSQL string

// kubernetesCaptureACLLockSQL holds the real capture row before the public reader reaches it.
// kubernetesCaptureACLLockSQL удерживает real capture row до прихода public reader.
//
//go:embed testdata/sql/kubernetes_capture_acl_lock.sql
var kubernetesCaptureACLLockSQL string

// kubernetesCaptureACLBlockedSQL proves the test reached an actual PostgreSQL blocking edge.
// kubernetesCaptureACLBlockedSQL доказывает достижение actual PostgreSQL blocking edge.
//
//go:embed testdata/sql/kubernetes_capture_acl_blocked.sql
var kubernetesCaptureACLBlockedSQL string

// kubernetesCaptureACLBoundSQL changes only this disposable organization's existing ACL expiry.
// kubernetesCaptureACLBoundSQL меняет лишь expiry существующих ACL disposable organization.
//
//go:embed testdata/sql/kubernetes_capture_acl_bound.sql
var kubernetesCaptureACLBoundSQL string

// kubernetesCaptureACLCommitKey carries only a closed COMMIT label, never query arguments.
// kubernetesCaptureACLCommitKey переносит лишь closed COMMIT label, не query arguments.
type kubernetesCaptureACLCommitKey struct{}

// kubernetesCaptureACLTrace observes one fixed query and optionally delays its actual successful COMMIT.
// Neither SQL arguments, private objects nor actor/task bindings enter the trace.
// kubernetesCaptureACLTrace наблюдает один fixed query и при необходимости задерживает actual successful COMMIT.
// SQL arguments, private objects и actor/task bindings не попадают в trace.
type kubernetesCaptureACLTrace struct {
	marker      string
	reached     chan struct{}
	once        sync.Once
	pid         atomic.Uint32
	armed       atomic.Bool
	commitUntil time.Time
}

// TraceQueryStart records only the fixed marker and backend PID needed for the native lock proof.
// TraceQueryStart сохраняет лишь fixed marker и backend PID для native lock proof.
func (tr *kubernetesCaptureACLTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == tr.marker {
		tr.pid.Store(conn.PgConn().PID())
		tr.armed.Store(true)
		tr.once.Do(func() { close(tr.reached) })
	}
	return context.WithValue(ctx, kubernetesCaptureACLCommitKey{}, strings.EqualFold(strings.TrimSpace(data.SQL), "commit"))
}

// TraceQueryEnd delays only a successful selected COMMIT past the known finite allowance.
// TraceQueryEnd задерживает лишь successful selected COMMIT за известный finite allowance.
func (tr *kubernetesCaptureACLTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(kubernetesCaptureACLCommitKey{}).(bool)
	if tr.commitUntil.IsZero() || !commit || data.Err != nil || !tr.armed.CompareAndSwap(true, false) {
		return
	}
	timer := time.NewTimer(time.Until(tr.commitUntil.Add(30 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// newKubernetesCaptureACLService preserves the actual repository configuration and principal normalization.
// Its additional disposable pool observes statement boundaries without changing authority.
// newKubernetesCaptureACLService сохраняет actual repository configuration и principal normalization.
// Дополнительный disposable pool наблюдает statement boundaries без изменения authority.
func newKubernetesCaptureACLService(t *testing.T, ctx context.Context, r *Repository, tr *kubernetesCaptureACLTrace) *platformservice.Service {
	t.Helper()
	config := r.pool.Config().Copy()
	config.ConnConfig.Tracer = tr
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create finite capture observer", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("connect finite capture observer", err)
	}
	copy := *r
	copy.pool = pool
	service, err := platformservice.New(&copy)
	if err != nil {
		t.Fatal("construct actual finite capture service", err)
	}
	return service
}

// awaitKubernetesCaptureACLBlock checks both the marker and pg_blocking_pids, not elapsed time alone.
// awaitKubernetesCaptureACLBlock проверяет marker и pg_blocking_pids, не только elapsed time.
func awaitKubernetesCaptureACLBlock(t *testing.T, ctx context.Context, r *Repository, tr *kubernetesCaptureACLTrace) {
	t.Helper()
	wait, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	select {
	case <-tr.reached:
	case <-wait.Done():
		t.Fatal("capture ACL boundary was not reached")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := r.pool.QueryRow(wait, kubernetesCaptureACLBlockedSQL, tr.pid.Load()).Scan(&blocked); err != nil {
			t.Fatal("read actual capture lock edge", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-wait.Done():
			t.Fatal("capture ACL boundary did not block")
		}
	}
}

// kubernetesCaptureACLResult owns one metadata-only call result while the test controls its lock.
// kubernetesCaptureACLResult владеет одним metadata-only call result во время удержания test lock.
type kubernetesCaptureACLResult struct {
	view entity.KubernetesCaptureStatusReadback
	err  error
}

// exerciseKubernetesCaptureRepairACL proves a finite upload-only allowance cannot survive a lock wait,
// even when a previously revoked unbounded alternative is restored before that lock is released.
// exerciseKubernetesCaptureRepairACL доказывает, что finite upload-only allowance не переживает lock wait,
// даже если ранее отозванная unbounded alternative восстановлена перед освобождением lock.
func exerciseKubernetesCaptureRepairACL(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, actor, repair value.Principal, ref string, source kubernetesCaptureSource, version int64) {
	t.Helper()
	resolved, err := r.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatal("normalize finite repair fixture", err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal("resolve finite repair fixture", err)
	}
	tr := &kubernetesCaptureACLTrace{marker: queryCommandsExecuteLockIdempotencyScope, reached: make(chan struct{})}
	observed := newKubernetesCaptureACLService(t, ctx, r, tr)
	execute := newComponentCommandExecutor(t, ctx, service, "capture-finite-upload-")
	role := execute(command.CreateAccessRole, actor, "role", nil, command.AccessRoleInput{Name: "Capture finite uploader", PermissionKeys: []string{"artifact.upload"}, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable finite repair admission"}).AccessRole
	until := time.Now().Add(3 * time.Second)
	execute(command.CreateAccessBinding, actor, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: current.actorRef, RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}, Conditions: entity.AccessConditions{ValidUntil: &until}})
	restore := limitKubernetesInitiatorToExecutionPermissions(t, ctx, kubernetesSetupOwnerFixture{repository: r, pool: r.pool, current: current, transport: actor}, service, source.ConnectionRef)
	defer restore()
	blocker, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin finite repair blocker", err)
	}
	defer blocker.Rollback(context.Background())
	const key = "capture-repair-finite-upload"
	if _, err := blocker.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID, kubernetesCaptureRepairOperation, key); err != nil {
		t.Fatal("hold exact repair idempotency edge", err)
	}
	result := make(chan kubernetesCaptureACLResult, 1)
	go func() {
		view, err := observed.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, command.KubernetesCaptureRepair{CaptureRef: ref})
		result <- kubernetesCaptureACLResult{view, err}
	}()
	awaitKubernetesCaptureACLBlock(t, ctx, r, tr)
	timer := time.NewTimer(time.Until(until.Add(40 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("finite repair overlap budget exhausted")
	}
	restore()
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal("release finite repair blocker", err)
	}
	var got kubernetesCaptureACLResult
	select {
	case got = <-result:
	case <-ctx.Done():
		t.Fatal("finite repair caller did not join")
	}
	if !errors.Is(got.err, errs.ErrNotFound) || got.view.RepairReceipt != nil || got.view.View.CaptureRef != "" {
		t.Fatal("expired upload allowance created or disclosed a repair", got.err)
	}
	reader := repair
	reader.Permission = "platform.integrations.kubernetes.capture.get"
	after, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: ref, RepairIdempotencyKey: key})
	if err != nil || after.View.State != enum.KubernetesCaptureWaitingOwner || after.View.Version != version || after.View.RepairGeneration != 0 || after.RepairReceipt != nil {
		t.Fatal("denied finite repair left a durable mutation", err)
	}
}

// TestKubernetesCaptureDeadlineOwnerComponent proves real row-lock and successful COMMIT waits cannot
// disclose retained metadata after run.view expired; no task, native effect or storage bytes are created.
// TestKubernetesCaptureDeadlineOwnerComponent доказывает, что real row-lock и successful COMMIT waits
// не раскрывают retained metadata после run.view expiry; task, native effect и storage bytes не создаются.
func TestKubernetesCaptureDeadlineOwnerComponent(t *testing.T) {
	for _, mode := range []string{"row_lock", "commit", "history_row_lock", "history_commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			r, service, _, gateway, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
			gateway.Permission = "platform.runtime.kubernetes.cancel"
			if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
				t.Fatal("seal finite metadata fixture", err)
			}
			resolved, err := r.ResolvePrincipal(ctx, actor)
			if err != nil {
				t.Fatal("normalize finite metadata actor", err)
			}
			current, err := r.resolveScope(ctx, resolved)
			if err != nil {
				t.Fatal("resolve finite metadata actor", err)
			}
			var ref string
			if err := r.pool.QueryRow(ctx, kubernetesCaptureACLRefSQL, admission.Stream.Ref, current.organizationID).Scan(&ref); err != nil {
				t.Fatal("read sealed finite metadata fixture", err)
			}
			actor.Permission = ""
			method := cp.ManagedKubernetesCaptureService_GetManagedKubernetesCaptureStatus_FullMethodName
			if strings.HasPrefix(mode, "history_") {
				method = cp.ManagedKubernetesCaptureService_ListManagedKubernetesCaptures_FullMethodName
			}
			readPermission := ""
			for operation, target := range controlplaneclient.ControlAPIGatewayOperations() {
				if target == cp.ManagedKubernetesCaptureService_GetManagedKubernetesCaptureStatus_FullMethodName {
					readPermission = operation
				}
				if target == method {
					actor.Permission = operation
				}
			}
			if actor.Permission == "" {
				t.Fatal("capture metadata operation is not registered")
			}
			reader := actor
			reader.Permission = readPermission
			initial, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: ref})
			if err != nil {
				t.Fatal("read history fixture locators", err)
			}
			tr := &kubernetesCaptureACLTrace{marker: queryKubernetesCapturePublicRead, reached: make(chan struct{})}
			observed := newKubernetesCaptureACLService(t, ctx, r, tr)
			var blocker pgx.Tx
			if strings.HasSuffix(mode, "row_lock") {
				blocker, err = r.pool.Begin(ctx)
				if err != nil {
					t.Fatal("begin finite metadata blocker", err)
				}
				defer blocker.Rollback(context.Background())
				var locked string
				if err := blocker.QueryRow(ctx, kubernetesCaptureACLLockSQL, ref, current.organizationID).Scan(&locked); err != nil || locked != ref {
					t.Fatal("hold finite metadata capture row", err)
				}
			}
			until := time.Now().Add(3 * time.Second)
			if strings.HasSuffix(mode, "commit") {
				tr.commitUntil = until
			}
			if tag, err := r.pool.Exec(ctx, kubernetesCaptureACLBoundSQL, current.organizationID, until); err != nil || tag.RowsAffected() == 0 {
				t.Fatal("bound actual metadata ACL", err)
			}
			result := make(chan kubernetesCaptureACLResult, 1)
			go func() {
				if strings.HasPrefix(mode, "history_") {
					page, err := observed.ListKubernetesCaptures(ctx, actor, query.KubernetesCaptureList{RunRef: initial.View.RunRef, NodeRef: initial.View.NodeRef})
					var view entity.KubernetesCaptureStatusReadback
					if len(page.Items) > 0 {
						view.View = page.Items[0]
					}
					result <- kubernetesCaptureACLResult{view, err}
					return
				}
				view, err := observed.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: ref})
				result <- kubernetesCaptureACLResult{view, err}
			}()
			if strings.HasSuffix(mode, "row_lock") {
				awaitKubernetesCaptureACLBlock(t, ctx, r, tr)
				timer := time.NewTimer(time.Until(until.Add(40 * time.Millisecond)))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					t.Fatal("finite metadata overlap budget exhausted")
				}
				if err := blocker.Rollback(ctx); err != nil {
					t.Fatal("release finite metadata blocker", err)
				}
			}
			var got kubernetesCaptureACLResult
			select {
			case got = <-result:
			case <-ctx.Done():
				t.Fatal("finite metadata caller did not join")
			}
			if !errors.Is(got.err, errs.ErrNotFound) || got.view.View.CaptureRef != "" || got.view.RepairReceipt != nil {
				t.Fatal("expired metadata ACL survived actual owner boundary", got.err)
			}
		})
	}
}
