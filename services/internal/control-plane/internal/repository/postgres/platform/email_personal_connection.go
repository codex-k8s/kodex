package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailPersonalActor resolves only the current active user; self setup does not accept service, tenant or another user's identity selectors.
	// queryEmailPersonalActor разрешает только текущего активного пользователя; собственная настройка не принимает selectors service, tenant либо другого пользователя.
	//go:embed sql/email_personal__actor.sql
	queryEmailPersonalActor string
	// queryEmailPersonalRoleLock serializes lazy creation of one closed metadata role per organization; the lock does not confer any access by itself.
	// queryEmailPersonalRoleLock сериализует отложенное создание одной закрытой metadata role на организацию; сама блокировка не предоставляет доступ.
	//go:embed sql/email_personal__role_lock.sql
	queryEmailPersonalRoleLock string
	// queryEmailPersonalRole reads the immutable current role version for strict permission/scope comparison before binding it to a new personal connection.
	// queryEmailPersonalRole читает текущую неизменную версию роли для строгого сравнения permission/scope перед привязкой к новому личному подключению.
	//go:embed sql/email_personal__role.sql
	queryEmailPersonalRole string
)

// authorizePersonalEmailConnectionTx permits only the dedicated active-user self-setup operation with no preexisting resource or mutable owner selector.
// authorizePersonalEmailConnectionTx допускает только выделенную active-user self-setup operation без прежнего ресурса либо изменяемого selector владельца.
func authorizePersonalEmailConnectionTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.PersonalEmailMailboxConnectionInput)
	_, validName := normalizedIntegrationConnectionName(payload.Name)
	if !ok || input.Mutation.ExpectedVersion != nil || !validName || !api.Address(payload.Sender) {
		return errs.ErrInvalid
	}
	if current.authorityProjectID != "" || input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != "platform.email-mailboxes.mine.create" {
		return errs.ErrForbidden
	}
	var actorID string
	if err := tx.QueryRow(ctx, queryEmailPersonalActor, pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID}).Scan(&actorID); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if actorID != current.actorID {
		return errs.ErrForbidden
	}
	return nil
}

// personalEmailMetadataRoleTx creates or verifies a closed system role whose only permissions are integration.view/manage at RESOURCE_INSTANCE; it cannot be used as organization, project, agent or grant administration.
// personalEmailMetadataRoleTx создаёт либо проверяет закрытую system role только с integration.view/manage в RESOURCE_INSTANCE; она не даёт управления организацией, проектом, агентом либо grants.
func (repository *Repository) personalEmailMetadataRoleTx(ctx context.Context, tx pgx.Tx, current scope) (string, error) {
	if _, err := tx.Exec(ctx, queryEmailPersonalRoleLock, pgx.StrictNamedArgs{"organization_id": current.organizationID}); err != nil {
		return "", errs.ErrUnavailable
	}
	var roleID, roleRef, kind, state, versionID string
	var revision int64
	var permissions, scopes []string
	err := tx.QueryRow(ctx, queryEmailPersonalRole, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&roleID, &roleRef, &kind, &state, &versionID, &revision, &permissions, &scopes)
	if err == nil {
		if kind != "SYSTEM" || state != "ACTIVE" || revision != 1 || !slices.Equal(permissions, []string{"integration.manage", "integration.view"}) || !slices.Equal(scopes, []string{"RESOURCE_INSTANCE"}) {
			return "", errs.ErrUnavailable
		}
		return versionID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrUnavailable
	}
	roleRef, err = newRef("arole")
	if err != nil {
		return "", err
	}
	var roleVersion int64
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, queryAccessInsertRole, pgx.NamedArgs{"ref": roleRef, "organization_id": current.organizationID, "stable_key": "PERSONAL_EMAIL_MAILBOX_METADATA", "kind": "SYSTEM", "created_by": current.actorID}).Scan(&roleID, &roleRef, &kind, &state, &roleVersion, &updatedAt); err != nil {
		return "", mapWriteError(err)
	}
	versionID, _, err = repository.insertAccessRoleVersion(ctx, tx, current, roleID, roleRef, 1, 0, command.AccessRoleInput{
		Name: "Настройка собственной почты", Description: "Только просмотр и настройка собственного почтового подключения; не права проекта, агента либо служебной identity.",
		PermissionKeys: []string{"integration.manage", "integration.view"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Назначено mailbox owner при создании личного подключения"})
	return versionID, err
}

// createPersonalEmailConnectionTx reuses the fixed email creator and atomically assigns only its new Connection's metadata ACL to the same user; package, target IDs and access permissions never come from payload.
// createPersonalEmailConnectionTx переиспользует фиксированное создание email и атомарно назначает тому же пользователю metadata ACL только нового Connection; package, target IDs и access permissions не поступают из payload.
func (repository *Repository) createPersonalEmailConnectionTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.PersonalEmailMailboxConnectionInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	mailboxRef, err := newMailboxRef()
	if err != nil {
		return commandOutcome{}, err
	}
	create := input
	create.Kind = command.CreateConnection
	create.Payload = command.ConnectionInput{DefinitionKey: "email", Name: payload.Name, PublicConfiguration: map[string]any{
		"base_url": "https://email-bridge.kodex-system.svc.cluster.local", "from_address": payload.Sender, "mailbox_id": mailboxRef}}
	outcome, err := repository.changeConnection(ctx, tx, current, create)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Connection == nil || outcome.result.Connection.DefinitionKey != "email" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	connection := outcome.result.Connection
	owner, err := requireEmailMailboxManagerTx(ctx, tx, current, connection.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if owner.owner.Policy != entity.IntegrationCredentialInitiatorRequired || owner.owner.SubjectID != current.actorID || owner.state != "ACTIVE" || owner.owner.Generation != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	roleVersionID, err := repository.personalEmailMetadataRoleTx(ctx, tx, current)
	if err != nil {
		return commandOutcome{}, err
	}
	bindingRef, err := newRef("abnd")
	if err != nil {
		return commandOutcome{}, err
	}
	var bindingID, bindingState string
	var bindingVersion int64
	var createdAt, updatedAt time.Time
	if err := tx.QueryRow(ctx, queryAccessInsertBinding, pgx.NamedArgs{"ref": bindingRef, "organization_id": current.organizationID, "subject_kind": "USER", "subject_id": current.actorID, "oidc_group_id": "",
		"role_version_id": roleVersionID, "scope_kind": "RESOURCE_INSTANCE", "project_id": "", "resource_kind": "INTEGRATION", "resource_id": owner.connectionID,
		"valid_from": nil, "valid_until": nil, "require_owner": false, "created_by": current.actorID}).Scan(&bindingID, &bindingRef, &bindingVersion, &bindingState, &createdAt, &updatedAt); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if bindingState != "ACTIVE" || bindingVersion != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := auditMailboxOwner(ctx, tx, current, connection.Ref, "platform.email-mailboxes.mine.create", "i18n:PERSONAL_EMAIL_MAILBOX_METADATA_ACCESS_ASSIGNED"); err != nil {
		return commandOutcome{}, err
	}
	view := emailMailboxOwnershipView(connection.Ref, owner)
	outcome.result.EmailMailboxOwnership = &view
	return outcome, nil
}
