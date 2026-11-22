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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// isolateIdentitySetupPermission bounds a real actor's permission in a disposable database while leaving its other authority and the setup ledger unchanged.
// The previous permissions are restored for the fixture after this scope completes.
// isolateIdentitySetupPermission ограничивает реальное разрешение actor в одноразовой БД, сохраняя остальные полномочия и setup ledger.
// По окончании проверки прежние разрешения фикстуры восстанавливаются.
func isolateIdentitySetupPermission(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *platformservice.Service, owner value.Principal, current scope, permissions []string, isolatedPermission string, validFor time.Duration) (time.Time, func()) {
	t.Helper()
	key, err := newRef("acl")
	if err != nil {
		t.Fatal("allocate finite setup fixture reference", err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, key)
	role := execute(command.CreateAccessRole, owner, "role", nil, command.AccessRoleInput{
		Name: key, PermissionKeys: permissions, AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "Disposable identity setup deadline",
	}).AccessRole
	until := time.Now().UTC().Add(validFor).Truncate(time.Microsecond)
	bound := execute(command.CreateAccessBinding, owner, "binding", nil, command.AccessBindingInput{
		SubjectKind: "USER", SubjectRef: current.actorRef, RoleVersionRef: role.CurrentVersion.Ref,
		Scope: entity.AccessScope{Kind: "ORGANIZATION"}, Conditions: entity.AccessConditions{ValidUntil: &until},
	}).AccessBinding
	if bound == nil {
		t.Fatal("allocate finite setup permission")
	}
	admin := evidenceRetentionAdministrator(t, ctx, pool.Config().ConnConfig)
	rows, err := admin.Query(ctx, queryFixtureIsolateAccess, current.organizationID, current.actorID, bound.Ref, isolatedPermission)
	if err != nil {
		t.Fatal("isolate finite identity-setup permission", err)
	}
	var demoted []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("read competing identity-setup permission")
		}
		demoted = append(demoted, id)
	}
	rows.Close()
	if rows.Err() != nil || len(demoted) == 0 {
		t.Fatal("expected competing permission to be disabled", rows.Err())
	}
	return until, func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, querySSHExecutionTestRestoreACL, current.organizationID, demoted); err != nil {
			t.Error("restore disposable identity-setup authority", err)
		}
	}
}

// delayedIdentitySetupCommit implements only the commit surface needed to test a late database acknowledgement.
// delayedIdentitySetupCommit реализует только commit для проверки позднего подтверждения от БД.
type delayedIdentitySetupCommit struct {
	pgx.Tx
	until  time.Time
	joined bool
}

// Commit models successful acknowledgement after the authority deadline without silently extending it.
// Commit моделирует успешное подтверждение БД после истечения полномочия, сохраняя первоначальный срок.
func (transaction *delayedIdentitySetupCommit) Commit(ctx context.Context) error {
	timer := time.NewTimer(time.Until(transaction.until) + time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		transaction.joined = true
		return nil
	}
}

// TestIdentitySetupAdmissionDeniesLateCommit checks that a successful but late COMMIT never yields a new broker admission.
// TestIdentitySetupAdmissionDeniesLateCommit проверяет, что успешный поздний COMMIT не выдаёт новый допуск брокеру.
func TestIdentitySetupAdmissionDeniesLateCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	permissionUntil := time.Now().Add(150 * time.Millisecond)
	transaction := &delayedIdentitySetupCommit{until: permissionUntil}
	if err := commitIdentitySetupAdmission(ctx, transaction, permissionUntil, time.Now().Add(time.Second)); !errors.Is(err, errs.ErrForbidden) || !transaction.joined {
		t.Fatal("late commit acknowledged a new broker operation", err)
	}
}
