package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testSSHRuntimeACLExpiryDuringConnectionLock proves a sole time-bound view grant cannot expire during an owner lock wait and still disclose output under a longer runtime lease.
// testSSHRuntimeACLExpiryDuringConnectionLock доказывает, что sole time-bound view grant не может истечь при ожидании owner lock и всё ещё выдать output под более длинной runtime lease.
func testSSHRuntimeACLExpiryDuringConnectionLock(t *testing.T, f *sshSetupNativeFixture, reader query.SSHRuntimeRead, actor value.Principal) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	current, err := f.repository.resolveScope(ctx, f.owner)
	if err != nil {
		t.Fatal("resolve original SSH ACL owner", err)
	}
	execute := newComponentCommandExecutor(t, ctx, f.service, "ssh-reader-acl-")
	role := execute(command.CreateAccessRole, f.transport, "role", nil, command.AccessRoleInput{Name: "Temporary SSH result view",
		PermissionKeys: []string{"run.view", "integration.view"}, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable finite read authority"}).AccessRole
	until := time.Now().UTC().Add(4 * time.Second).Truncate(time.Microsecond)
	bound := execute(command.CreateAccessBinding, f.transport, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: current.actorRef,
		RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}, Conditions: entity.AccessConditions{ValidUntil: &until}}).AccessBinding
	if bound == nil {
		t.Fatal("bind finite original read authority")
	}
	admin := evidenceRetentionAdministrator(t, ctx, f.pool.Config().ConnConfig)
	rows, err := admin.Query(ctx, `UPDATE control_plane.access_bindings binding SET state='REVOKED',version=binding.version+1
 FROM control_plane.application_role_versions version
 WHERE binding.organization_id=$1::uuid AND binding.role_version_id=version.id AND binding.state='ACTIVE' AND binding.ref<>$3
 AND ('run.view'=ANY(version.permission_keys) OR 'integration.view'=ANY(version.permission_keys))
 AND(binding.subject_id=$2::uuid OR binding.oidc_group_id IN(SELECT membership.group_id FROM control_plane.oidc_group_memberships membership
 WHERE membership.organization_id=$1::uuid AND membership.subject_id=$2::uuid)) RETURNING binding.id::text`, current.organizationID, current.actorID, bound.Ref)
	if err != nil {
		t.Fatal("isolate finite SSH reader ACL", err)
	}
	var demoted []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("read isolated SSH ACL binding")
		}
		demoted = append(demoted, id)
	}
	rows.Close()
	if rows.Err() != nil || len(demoted) == 0 {
		t.Fatal("finite SSH read fixture retained unlimited authority")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, `UPDATE control_plane.access_bindings SET state='ACTIVE',version=version+1 WHERE organization_id=$1::uuid AND id=ANY($2::uuid[])`, current.organizationID, demoted); err != nil {
			t.Error("restore disposable SSH reader ACL")
		}
	})
	trace := &kubernetesRuntimeReadTrace{started: make(chan uint32, 1)}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect SSH reader trace", err)
	}
	repository := *f.repository
	repository.pool = pool
	service, err := platformservice.New(&repository)
	if err != nil {
		pool.Close()
		t.Fatal("construct SSH reader trace service", err)
	}
	blocker, err := f.pool.Begin(ctx)
	if err != nil {
		pool.Close()
		t.Fatal("begin SSH reader blocker", err)
	}
	var id string
	if err := blocker.QueryRow(ctx, "SELECT id::text FROM control_plane.integration_connections WHERE ref=$1 FOR UPDATE", f.connection.Ref).Scan(&id); err != nil {
		_ = blocker.Rollback(ctx)
		pool.Close()
		t.Fatal("lock original SSH Connection", err)
	}
	// readOutcome carries only safe equality/error checks; output bytes never enter diagnostics.
	// readOutcome содержит лишь safe equality/error checks; output bytes не входят в diagnostics.
	type readOutcome struct {
		view entity.SSHRuntimeResult
		err  error
	}
	completed := make(chan readOutcome, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := blocker.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("rollback SSH reader blocker")
		}
		if !joined {
			select {
			case <-completed:
			case <-cleanup.Done():
				t.Error("SSH result reader did not join")
			}
		}
		pool.Close()
	})
	go func() {
		view, err := service.ReadSSHRuntimeInvocation(ctx, actor, reader)
		completed <- readOutcome{view, err}
	}()
	select {
	case <-trace.started:
		if !time.Now().Before(until) {
			t.Fatal("ACL expired before the actual lock wait")
		}
	case outcome := <-completed:
		joined = true
		t.Fatal("SSH reader did not reach its post-authorization lock", outcome.err)
	case <-ctx.Done():
		t.Fatal("SSH reader lock trace timed out")
	}
	select {
	case <-time.After(time.Until(until) + 100*time.Millisecond):
	case <-ctx.Done():
		t.Fatal("SSH ACL expiry wait timed out")
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal("release original SSH Connection", err)
	}
	select {
	case outcome := <-completed:
		joined = true
		if !errors.Is(outcome.err, errs.ErrNotFound) || outcome.view != (entity.SSHRuntimeResult{}) {
			t.Error("expired original ACL disclosed SSH result")
		}
	case <-ctx.Done():
		t.Fatal("SSH reader did not finish after lock release")
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin independent expired ACL check", err)
	}
	defer tx.Rollback(ctx)
	if err := f.repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: f.connection.Ref}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("finite ACL counterexample retained another view grant", err)
	}
}
