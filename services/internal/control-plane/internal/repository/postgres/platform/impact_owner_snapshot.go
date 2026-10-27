package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryImpactConsumerOwner embeds authoritative active agent/workflow/schedule owner lookup within
// one organization, recognizing only the organization system assistant or project-scoped
// consumers. Project results require an active project; payload owner fields are not consulted.
// queryImpactConsumerOwner встраивает авторитетный поиск владельца активного агента, workflow либо
// расписания внутри организации, различая системного ассистента организации и потребителей
// проекта. Проектные результаты требуют активного проекта; поля владельца из payload не
// используются.
//
//go:embed sql/impact_consumer_owner.sql
var queryImpactConsumerOwner string

// validRuntimeOwnerSnapshot checks the structural owner tuple: a nonempty organization plus
// ORGANIZATION with no project or PROJECT with a nonempty project. It performs no lookup,
// authority validation or permission check.
// validRuntimeOwnerSnapshot проверяет форму кортежа владельца: непустая организация и ORGANIZATION
// без проекта либо PROJECT с непустым проектом. Не обращается к хранилищу и не проверяет
// полномочия или доступ.
func validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef string) bool {
	return organizationRef != "" && (scopeKind == "ORGANIZATION" && projectRef == "" || scopeKind == "PROJECT" && projectRef != "")
}

// matchRuntimeOwnerSnapshot rejects a malformed expected tuple as Invalid and a malformed or
// different actual tuple as NotFound. Exact equality binds an immutable snapshot to current owner
// data; the expected tuple is not an authority source.
// matchRuntimeOwnerSnapshot отклоняет неверную форму ожидаемого кортежа как Invalid, неверный или
// отличающийся фактический кортеж — как NotFound. Точное равенство связывает неизменяемый снимок с
// текущими данными владельца; ожидаемый кортеж не является источником полномочий.
func matchRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef, expectedScope, expectedOrganization, expectedProject string) error {
	if !validRuntimeOwnerSnapshot(expectedScope, expectedOrganization, expectedProject) {
		return errs.ErrInvalid
	}
	if !validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef) || scopeKind != expectedScope || organizationRef != expectedOrganization || projectRef != expectedProject {
		return errs.ErrNotFound
	}
	return nil
}

// impactConsumerOwner maps AGENT_CONTINUATION to AGENT, loads the authoritative tenant owner tuple
// and rejects missing or corrupt cross-organization results. Organization-scoped consumers
// additionally require environment-owner access; project permissions remain with specialized
// callers.
// impactConsumerOwner преобразует AGENT_CONTINUATION в AGENT, загружает авторитетный кортеж
// владельца организации и отклоняет отсутствие либо повреждённый результат другой организации. Для
// потребителей организации дополнительно требует доступ владельца окружения; проектные разрешения
// проверяет специализированный вызывающий код.
func (r *Repository) impactConsumerOwner(ctx context.Context, tx pgx.Tx, s scope, kind, ref string) (entity.RuntimeEnvironmentConsumer, error) {
	var owner entity.RuntimeEnvironmentConsumer
	if kind == "AGENT_CONTINUATION" {
		kind = "AGENT"
	}
	err := tx.QueryRow(ctx, queryImpactConsumerOwner, s.organizationID, kind, ref).Scan(&owner.ScopeKind, &owner.OrganizationRef, &owner.ProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, errs.ErrNotFound
	}
	if err != nil || !validRuntimeOwnerSnapshot(owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef) || owner.OrganizationRef != s.organizationRef {
		return owner, errs.ErrUnavailable
	}
	if owner.ScopeKind == "ORGANIZATION" {
		if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, owner.ScopeKind, owner.ProjectRef); err != nil {
			return owner, err
		}
	}
	return owner, nil
}

// authorizeRuntimeEnvironmentConsumers requires environment-owner access, then fresh agent.manage
// for every selected consumer and exact equality between submitted, authoritative consumer and
// environment owner tuples. It stops on the first error without writing or granting authority from
// selection fields.
// authorizeRuntimeEnvironmentConsumers требует доступ владельца окружения, затем свежий
// agent.manage для каждого выбранного потребителя и точное совпадение переданного, авторитетного
// потребительского и владельческого кортежей окружения. Останавливается на первой ошибке без
// записи и без вывода полномочий из полей выбора.
func (r *Repository) authorizeRuntimeEnvironmentConsumers(ctx context.Context, tx pgx.Tx, s scope, environment entity.RuntimeEnvironmentSet, consumers []entity.RuntimeEnvironmentConsumer) error {
	if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, environment.ScopeKind, environment.ProjectRef); err != nil {
		return err
	}
	for _, consumer := range consumers {
		owner, err := r.impactConsumerOwner(ctx, tx, s, "AGENT", consumer.AgentRef)
		if err != nil {
			return err
		}
		permission, target, err := r.resolveRuntimeConfigurationTarget(ctx, tx, s, "agent.manage", consumer.AgentRef)
		if err != nil {
			return err
		}
		if err := r.requireAccess(ctx, tx, s, permission, target); err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef, consumer.ScopeKind, consumer.OrganizationRef, consumer.ProjectRef); err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef); err != nil {
			return err
		}
	}
	return nil
}

// authorizeRuntimeSecretSelections locks the runtime secret, authorizes its impact target and
// checks every selected environment owner snapshot and shared secret scope before authorizing its
// consumers. It may return the locked secret alongside an error; the caller must not treat that as
// successful admission or commit.
// authorizeRuntimeSecretSelections блокирует runtime-секрет, проверяет доступ к цели влияния и
// снимок владельца каждого выбранного окружения и общую область секрета, затем проверяет
// потребителей. Может вернуть заблокированный секрет вместе с ошибкой; это не означает успешный
// допуск или завершение транзакции.
func (r *Repository) authorizeRuntimeSecretSelections(ctx context.Context, tx pgx.Tx, s scope, secretRef string, revision int64, selections []entity.RuntimeSecretRebindSelection) (lockedRuntimeSecret, error) {
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, secretRef)
	if err != nil {
		return secret, err
	}
	if _, _, err := r.secretImpactTarget(ctx, tx, s, secretRef, revision); err != nil {
		return secret, err
	}
	for _, selection := range selections {
		environment, err := r.getRuntimeEnvironmentTx(ctx, tx, s, selection.EnvironmentRef)
		if err != nil {
			return secret, err
		}
		if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, environment.ScopeKind, environment.ProjectRef); err != nil {
			return secret, err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, selection.ScopeKind, selection.OrganizationRef, selection.ProjectRef); err != nil {
			return secret, err
		}
		if err := matchRuntimeOwnerSnapshot(secret.scopeKind, secret.organizationRef, secret.projectRef, environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef); err != nil {
			return secret, err
		}
		if err := r.authorizeRuntimeEnvironmentConsumers(ctx, tx, s, environment, selection.Consumers); err != nil {
			return secret, err
		}
	}
	return secret, nil
}

// validateImpactOwnerReceipt reloads and authorizes referenced revision and role-image impact
// plans, checks returned item owner tuple shapes and rereads each result environment owner against
// its receipt snapshot. It neither trusts receipt ownership as authority nor mutates result data;
// failures prevent replay issuance.
// validateImpactOwnerReceipt заново загружает и авторизует указанные планы влияния ревизий и
// образов ролей, проверяет форму кортежей владельцев элементов и сверяет владельца каждого
// окружения результата со снимком квитанции. Не доверяет владению из квитанции как полномочиям и
// не изменяет результат; ошибки препятствуют повторной выдаче.
func (r *Repository) validateImpactOwnerReceipt(ctx context.Context, tx pgx.Tx, s scope, input command.Command, result command.Result) error {
	if result.RevisionImpactPlan != nil {
		row, err := r.revisionImpact(ctx, tx, s, result.RevisionImpactPlan.Ref)
		if err != nil {
			return err
		}
		if err := r.revisionImpactAccess(ctx, tx, s, row); err != nil {
			return err
		}
		items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !validRuntimeOwnerSnapshot(item.ScopeKind, item.OrganizationRef, item.ProjectRef) {
				return errs.ErrNotFound
			}
		}
	}
	if result.RoleImageImpactPlan != nil {
		row, err := r.roleImageImpact(ctx, tx, s, result.RoleImageImpactPlan.Ref)
		if err != nil {
			return err
		}
		if _, _, err := r.roleImageImpactAccess(ctx, tx, s, row); err != nil {
			return err
		}
		items, err := r.roleImageImpactItems(ctx, tx, row.id)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !validRuntimeOwnerSnapshot(item.Consumer.ScopeKind, item.Consumer.OrganizationRef, item.Consumer.ProjectRef) {
				return errs.ErrNotFound
			}
		}
	}
	environments := append([]entity.RuntimeEnvironmentSet{}, result.RuntimeEnvironments...)
	if result.RuntimeEnvironment != nil {
		environments = append(environments, *result.RuntimeEnvironment)
	}
	for _, item := range environments {
		environment, err := r.getRuntimeEnvironmentTx(ctx, tx, s, item.Ref)
		if err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, item.ScopeKind, item.OrganizationRef, item.ProjectRef); err != nil {
			return errs.ErrNotFound
		}
	}
	return nil
}
