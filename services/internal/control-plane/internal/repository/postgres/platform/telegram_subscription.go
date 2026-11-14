package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryTelegramProjectOwner resolves and locks a currently eligible project before subscription receipt or version checks.
	// queryTelegramProjectOwner разрешает и блокирует текущий доступный проект до проверки квитанции либо версии подписки.
	//go:embed sql/telegram_project_owner.sql
	queryTelegramProjectOwner string
	// queryTelegramSubscriptionLock serializes subscription mutation and admission on the stable project owner row.
	// queryTelegramSubscriptionLock упорядочивает изменение подписки и допуск по устойчивой строке владельца проекта.
	//go:embed sql/telegram_subscription_lock.sql
	queryTelegramSubscriptionLock string
	// queryTelegramConnectionPin requires the exact active service credential revision without reading its secret value.
	// queryTelegramConnectionPin требует точную активную служебную ревизию реквизитов без чтения значения секрета.
	//go:embed sql/telegram_connection_pin.sql
	queryTelegramConnectionPin string
	// queryTelegramSubscriptionUpsert assigns the database expiry and monotonically advances only the selected project subscription.
	// queryTelegramSubscriptionUpsert назначает срок по часам БД и монотонно продвигает только подписку выбранного проекта.
	//go:embed sql/telegram_subscription_upsert.sql
	queryTelegramSubscriptionUpsert string
	// queryTelegramSubscriptionRevoke closes the previous grant and distinguishes unsent work from possible started effects atomically.
	// queryTelegramSubscriptionRevoke атомарно закрывает прежнее разрешение и различает неотправленную работу и возможные начатые эффекты.
	//go:embed sql/telegram_subscription_revoke.sql
	queryTelegramSubscriptionRevoke string
	// queryTelegramSubscriptionBindGrant links only the server-assigned exact subscription grant in the same owner transaction.
	// queryTelegramSubscriptionBindGrant связывает лишь назначенное сервером точное разрешение подписки в той же транзакции владельца.
	//go:embed sql/telegram_subscription_bind_grant.sql
	queryTelegramSubscriptionBindGrant string
)

// telegramSubscriptionRow separates internal identifiers and immutable configuration from the public masked receipt.
// telegramSubscriptionRow отделяет внутренние идентификаторы и неизменяемую настройку от публичной маскированной квитанции.
type telegramSubscriptionRow struct {
	view          entity.TelegramSubscription
	id, grantID   string
	configuration map[string]string
}

// telegramProjectOwnerTx requires one current project permission under the verified tenant and optional signed project boundary.
// telegramProjectOwnerTx требует текущее право проекта внутри проверенного tenant и необязательной подписанной границы проекта.
func telegramProjectOwnerTx(ctx context.Context, tx pgx.Tx, current scope, projectRef, permission string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, queryTelegramProjectOwner, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"actor_id": current.actorID, "authority_project": current.authorityProjectID, "project_ref": projectRef, "permission": permission}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrNotFound
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	return id, nil
}

// readTelegramSubscriptionTx locks the exact project subscription and returns only masked coordinates to its safe view.
// readTelegramSubscriptionTx блокирует точную подписку проекта и возвращает в безопасном представлении только маскированные координаты.
func readTelegramSubscriptionTx(ctx context.Context, tx pgx.Tx, current scope, projectID, projectRef string) (telegramSubscriptionRow, error) {
	var row telegramSubscriptionRow
	var raw []byte
	err := tx.QueryRow(ctx, queryTelegramSubscriptionLock, current.organizationID, projectID).Scan(&row.id, &row.view.Ref,
		&row.view.Version, &row.view.ConnectionRef, &row.view.Enabled, &row.view.EventTypes, &row.view.ScopeDigest, &raw,
		&row.view.ExpiresAt, &row.view.MaxEffects, &row.view.UsedEffects, &row.view.UpdatedAt, &row.grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(raw, &row.configuration) != nil {
		return row, errs.ErrUnavailable
	}
	configuration, err := integrationpackage.ParseTelegramConfiguration(row.configuration)
	if err != nil {
		return row, errs.ErrUnavailable
	}
	row.view.ProjectRef = projectRef
	row.view.BotID = configuration.BotID
	chat := strconv.FormatInt(configuration.ChatID, 10)
	if len(chat) > 4 {
		chat = chat[len(chat)-4:]
	}
	row.view.ChatDisplay = "…" + chat
	row.view.TopicDisplay = row.configuration["topic"]
	row.view.State = "ACTIVE"
	if !row.view.Enabled {
		row.view.State = "DISABLED"
	} else if !row.view.ExpiresAt.After(time.Now().UTC()) {
		row.view.State = "EXPIRED"
	} else if row.view.UsedEffects >= row.view.MaxEffects {
		row.view.State = "EXHAUSTED"
	}
	return row, nil
}

// authorizeTelegramSubscriptionTx checks both project management and exact connection management before an idempotency receipt can be replayed.
// authorizeTelegramSubscriptionTx проверяет управление проектом и точным подключением до повтора квитанции идемпотентности.
func (repository *Repository) authorizeTelegramSubscriptionTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(port.TelegramSubscriptionConfigure)
	if !ok || input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != "platform.command.telegram-subscription.configure" {
		return errs.ErrForbidden
	}
	projectID, err := telegramProjectOwnerTx(ctx, tx, current, payload.ProjectRef, "project.manage")
	if err != nil {
		return err
	}
	connectionRef := payload.ConnectionRef
	if !payload.Enabled {
		row, readErr := readTelegramSubscriptionTx(ctx, tx, current, projectID, payload.ProjectRef)
		if readErr != nil {
			return readErr
		}
		connectionRef = row.view.ConnectionRef
	}
	return repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
}

// ConfigureTelegramSubscription uses the existing atomic owner receipt and audit path; no synthetic Gate decision or generic grant command is issued.
// ConfigureTelegramSubscription использует прежний атомарный путь owner receipt и аудита; фиктивного решения Gate либо generic grant command нет.
func (repository *Repository) ConfigureTelegramSubscription(ctx context.Context, p value.Principal, mutation value.Mutation, input port.TelegramSubscriptionConfigure) (entity.TelegramSubscription, error) {
	mutation.Operation = "platform.command.telegram-subscription.configure"
	mutation.IntentDigest = interactionDigest(mutation.Operation, string(asJSON(struct {
		Input    port.TelegramSubscriptionConfigure
		Expected *int64
	}{Input: input, Expected: mutation.ExpectedVersion})))
	if mutation.Validate() != nil {
		return entity.TelegramSubscription{}, errs.ErrInvalid
	}
	result, err := repository.Execute(ctx, command.Command{Kind: command.ConfigureTelegramSubscription, Principal: p, Mutation: mutation, Payload: input})
	if err != nil {
		return entity.TelegramSubscription{}, err
	}
	if result.TelegramSubscription == nil {
		return entity.TelegramSubscription{}, errs.ErrUnavailable
	}
	return *result.TelegramSubscription, nil
}

// configureTelegramSubscriptionTx replaces only future subscription authority, revokes previous pending work, and pins current service credentials and package metadata.
// configureTelegramSubscriptionTx заменяет лишь будущие полномочия подписки, отзывает прежнюю ожидающую работу и закрепляет текущие служебные реквизиты и метаданные пакета.
func (repository *Repository) configureTelegramSubscriptionTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload := input.Payload.(port.TelegramSubscriptionConfigure)
	if payload.Enabled && integrationpackage.ValidateTelegramPublicOrigin(repository.interactionPublicOrigin) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	projectID, err := telegramProjectOwnerTx(ctx, tx, current, payload.ProjectRef, "project.manage")
	if err != nil {
		return commandOutcome{}, err
	}
	before, readErr := readTelegramSubscriptionTx(ctx, tx, current, projectID, payload.ProjectRef)
	if readErr != nil && !errors.Is(readErr, errs.ErrNotFound) {
		return commandOutcome{}, readErr
	}
	version := int64(1)
	if readErr == nil {
		if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != before.view.Version {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		version = before.view.Version + 1
	} else if input.Mutation.ExpectedVersion != nil || !payload.Enabled {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if before.id != "" {
		if _, err := tx.Exec(ctx, queryTelegramSubscriptionRevoke, pgx.StrictNamedArgs{"grant_id": before.grantID, "subscription_id": before.id, "disable": !payload.Enabled}); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if payload.Enabled {
		connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if connection.definitionKey != "telegram" || connection.credentialPolicy != entity.IntegrationCredentialProjectShared {
			return commandOutcome{}, errs.ErrForbidden
		}
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, payload.ConnectionRef, connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
		if err != nil || !definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction) {
			return commandOutcome{}, errs.ErrForbidden
		}
		public, err := readConnection(ctx, tx, current, payload.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		configuration, ok := integrationStringConfiguration(public.PublicConfiguration)
		if !ok {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := integrationpackage.ParseTelegramConfiguration(configuration); err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		var credentialID string
		if err := tx.QueryRow(ctx, queryTelegramConnectionPin, current.organizationID, connection.id).Scan(&credentialID); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrForbidden
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref := before.view.Ref
		if ref == "" {
			ref, err = newRef("tgs")
			if err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		scopeRaw := asJSON(map[string]any{"project_ref": payload.ProjectRef, "subscription_ref": ref, "subscription_version": version,
			"connection_ref": payload.ConnectionRef, "connection_version": connection.version, "credential_revision_id": credentialID,
			"definition_version": connection.definitionVersion, "definition_digest": connection.definitionDigest, "configuration": configuration,
			"events": payload.EventTypes, "duration_seconds": payload.DurationSeconds, "max_effects": payload.MaxEffects, "approved_by": current.actorID})
		digest := interactionDigest("telegram-subscription/v1", string(scopeRaw))
		var id, assignedRef string
		err = tx.QueryRow(ctx, queryTelegramSubscriptionUpsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
			"project_id": projectID, "connection_id": connection.id, "connection_version": connection.version, "credential_revision_id": credentialID,
			"definition_version": connection.definitionVersion, "definition_digest": connection.definitionDigest, "configuration": asJSON(configuration),
			"events": payload.EventTypes, "scope_digest": digest, "actor_id": current.actorID, "duration_seconds": payload.DurationSeconds, "max_effects": payload.MaxEffects}).Scan(&id, &assignedRef)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if assignedRef != ref {
			return commandOutcome{}, errs.ErrUnavailable
		}
		grantRef, err := newRef("igr")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		err = tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind,
			grantRef, current.organizationID, connection.id, "telegram.notifications", "TELEGRAM_SUBSCRIPTION", ref, "HUMAN_SCOPED", current.actorID,
			"WRITE", "HTTPS_RESOURCE", scopeRaw, digest, connection.definitionVersion, connection.definitionDigest, []string{}).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, err := tx.Exec(ctx, queryTelegramSubscriptionBindGrant, id, grantRef); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	after, err := readTelegramSubscriptionTx(ctx, tx, current, projectID, payload.ProjectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	after.view.CanManage = true
	return commandOutcome{result: command.Result{TelegramSubscription: &after.view}, projectID: projectID, projectRef: payload.ProjectRef,
		resourceKind: "PROJECT", resourceRef: payload.ProjectRef, summary: "i18n:TELEGRAM_SUBSCRIPTION_CONFIGURED"}, nil
}

// ConfigureInteractionPublicOrigin accepts only the canonical deployment origin; an absent optional projection leaves Telegram configuration unavailable without blocking web-only startup.
// ConfigureInteractionPublicOrigin принимает лишь канонический deployment origin; отсутствие необязательной проекции запрещает настройку Telegram, не блокируя web-only startup.
func (repository *Repository) ConfigureInteractionPublicOrigin(origin string) error {
	if origin != "" && integrationpackage.ValidateTelegramPublicOrigin(origin) != nil {
		return errs.ErrInvalid
	}
	repository.interactionPublicOrigin = origin
	return nil
}

// GetTelegramSubscription rechecks current project management eligibility and returns the authoritative safe state without changing expiry or consent.
// GetTelegramSubscription повторно проверяет текущее управление проектом и возвращает безопасное авторитетное состояние без изменения срока либо согласия.
func (repository *Repository) GetTelegramSubscription(ctx context.Context, p value.Principal, projectRef string) (*entity.TelegramSubscription, bool, error) {
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.query.telegram-subscription.get" {
		return nil, false, errs.ErrForbidden
	}
	current, tx, err := repository.beginScopedTransaction(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	projectID, err := telegramProjectOwnerTx(ctx, tx, current, projectRef, "project.manage")
	if err != nil {
		return nil, false, err
	}
	row, err := readTelegramSubscriptionTx(ctx, tx, current, projectID, projectRef)
	if errors.Is(err, errs.ErrNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: row.view.ConnectionRef}); err != nil {
		return nil, false, err
	}
	row.view.CanManage = true
	if err := tx.Commit(ctx); err != nil {
		return nil, false, errs.ErrUnavailable
	}
	return &row.view, true, nil
}
