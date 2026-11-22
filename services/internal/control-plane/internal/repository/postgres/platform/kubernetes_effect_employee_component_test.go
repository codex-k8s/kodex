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
)

// limitKubernetesInitiatorToExecutionPermissions keeps the active original USER/root/personal
// identity but removes all management-bearing bindings in its isolated fixture. Public role APIs
// grant only view/launch rights; the existing disposable administrator performs reversible demotion.
// limitKubernetesInitiatorToExecutionPermissions сохраняет active original USER/root/personal identity,
// но удаляет management-bearing bindings в isolated fixture. Public role APIs дают лишь view/launch;
// existing disposable administrator выполняет обратимое понижение прав.
func limitKubernetesInitiatorToExecutionPermissions(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture, service *platformservice.Service, connectionRef string) func() {
	t.Helper()
	execute := newComponentCommandExecutor(t, ctx, service, "kube-effect-employee-")
	role := execute(command.CreateAccessRole, fixture.transport, "role", nil, command.AccessRoleInput{Name: "Kubernetes assigned employee", PermissionKeys: []string{"integration.view", "agent.launch", "agent.view", "project.view", "run.view"}, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable execution-only authority"}).AccessRole
	if role == nil {
		t.Fatal("create execution-only role")
	}
	bound := execute(command.CreateAccessBinding, fixture.transport, "binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: fixture.current.actorRef, RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}}).AccessBinding
	if bound == nil {
		t.Fatal("bind execution-only role")
	}
	admin := evidenceRetentionAdministrator(t, ctx, fixture.pool.Config().ConnConfig)
	rows, err := admin.Query(ctx, `UPDATE control_plane.access_bindings binding SET state='REVOKED',version=binding.version+1
 FROM control_plane.application_role_versions version
 WHERE binding.organization_id=$1::uuid AND binding.role_version_id=version.id AND binding.state='ACTIVE'
 AND 'integration.manage'=ANY(version.permission_keys)
 AND(binding.subject_id=$2::uuid OR binding.oidc_group_id IN(SELECT membership.group_id FROM control_plane.oidc_group_memberships membership WHERE membership.organization_id=$1::uuid AND membership.subject_id=$2::uuid))
 RETURNING binding.id::text`, fixture.current.organizationID, fixture.current.actorID)
	if err != nil {
		t.Fatal("demote disposable initiator", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("capture demoted binding")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) == 0 {
		t.Fatal("no management binding was removed")
	}
	restored := false
	// restore returns management rights only for later public mutation fixtures, never for execution.
	// restore возвращает management rights лишь для последующих public mutation fixtures, не execution.
	restore := func() {
		if restored {
			return
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, `UPDATE control_plane.access_bindings SET state='ACTIVE',version=version+1 WHERE organization_id=$1::uuid AND id=ANY($2::uuid[])`, fixture.current.organizationID, ids); err != nil {
			t.Fatal("restore disposable initiator", err)
		}
		restored = true
	}
	t.Cleanup(restore)
	tx, err := fixture.repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}
	if denied := fixture.repository.requireAccess(ctx, tx, fixture.current, "integration.manage", target); !errors.Is(denied, errs.ErrNotFound) {
		t.Fatal("execution fixture retained management authority", denied)
	}
	if fixture.repository.requireAccess(ctx, tx, fixture.current, "integration.view", target) != nil {
		t.Fatal("execution fixture lost explicit view authority")
	}
	return restore
}
