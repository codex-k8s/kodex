package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// projectInteractionIncident Projects optional delivery state into bounded incident
// category/severity/state/i18n guidance while preserving identity/timestamps and marking the core
// run unaffected. Unknown outcome takes priority over exhausted attempts, and success resolves a
// previous failure.
// projectInteractionIncident Проецирует состояние необязательной доставки в фиксированные
// категорию, тяжесть, состояние и i18n-подсказки, сохраняя идентификаторы и время, помечая
// основной запуск незатронутым. Неопределённый исход имеет приоритет над исчерпанием попыток;
// успех разрешает прежний сбой.
func projectInteractionIncident(incident entity.Incident, deliveryState string, attempt, maximumAttempts int) entity.Incident {
	incident.Category = "OPTIONAL_INTERACTION_DELIVERY"
	incident.CoreAffected = false
	switch {
	case deliveryState == "UNKNOWN_OUTCOME":
		incident.Severity = "ERROR"
		incident.State = "OPEN"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_OUTCOME_UNKNOWN"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RECONCILIATION_REQUIRED"
	case deliveryState == "SUCCEEDED":
		incident.Severity = "INFO"
		incident.State = "RESOLVED"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_RECOVERED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RECOVERY_COMPLETE"
	case attempt >= maximumAttempts:
		incident.Severity = "ERROR"
		incident.State = "OPEN"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_FAILED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RETRY_EXHAUSTED"
	default:
		incident.Severity = "WARNING"
		incident.State = "RECOVERING"
		incident.SafeSummary = "i18n:INTERACTION_DELIVERY_FAILED"
		incident.SafeNextStep = "i18n:INTERACTION_DELIVERY_RETRYING"
	}
	return incident
}

// ListInteractionSources Resolves scope in RepeatableRead, scans credential metadata and
// capabilities, closes rows before package reads, filters source capabilities and projects only
// eligible sources. Commits before returning an allocated empty/list result; read/commit failures
// are unavailable and deferred request-context rollback errors remain ignored.
// ListInteractionSources Разрешает область в RepeatableRead, читает метаданные credential и
// возможности, закрывает rows до чтения пакетов, фильтрует входящие возможности и проецирует
// пригодные источники. Возвращает созданный список после commit; сбои чтения или commit дают
// Unavailable, ошибка отложенного rollback с контекстом запроса игнорируется.
func (repository *Repository) ListInteractionSources(ctx context.Context, principal value.Principal) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryInteractionListSources, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var connectionRef, credentialRef, baseURL, teamName, channelName, locale string
		var credentialRevisionRef string
		var connectionVersion, credentialRevision int64
		var credential entity.IntegrationCredentialRevision
		var capabilities []string
		if err := rows.Scan(&connectionRef, &credentialRef, &baseURL, &teamName, &channelName, &locale, &capabilities,
			&connectionVersion, &credentialRevisionRef, &credentialRevision, &credential.SecretRef, &credential.SecretUID,
			&credential.SecretResourceVersion, &credential.ContentSHA256, &credential.CreatedAt); err != nil {
			return nil, errs.ErrUnavailable
		}
		credential.Ref, credential.Revision = credentialRevisionRef, credentialRevision
		result = append(result, map[string]any{
			"credential":    credential,
			"connectionRef": connectionRef, "credentialRef": credentialRef,
			"baseURL": baseURL, "teamName": teamName, "channelName": channelName,
			"locale": locale, "capabilities": capabilities,
			"connectionVersion": connectionVersion, "credentialRevisionRef": credentialRevisionRef, "credentialRevision": credentialRevision,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows.Close()
	eligible := make([]map[string]any, 0, len(result))
	for _, item := range result {
		connection, definition, err := repository.interactionConnectionPackage(ctx, tx, scope.organizationID, stringMap(item, "connectionRef"))
		if err != nil {
			return nil, err
		}
		capabilities := []string{}
		for _, key := range item["capabilities"].([]string) {
			if interactionSourceCapability(definition, key) {
				capabilities = append(capabilities, key)
			}
		}
		if len(capabilities) == 0 {
			continue
		}
		item["capabilities"] = capabilities
		if err := projectInteractionPackage(item, connection, definition); err != nil {
			return nil, err
		}
		eligible = append(eligible, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrUnavailable
	}
	return eligible, nil
}

// ClaimInteractionDeliveries Claims deliveries in a caller-scope Serializable transaction, decodes
// template JSON and validates current package source or approved WRITE/HUMAN_EACH_EFFECT
// capability after closing rows. Commit failure maps to conflict; no messages are sent here.
// Limit/workload are forwarded unchanged and SQL owns expiry/fence creation.
// ClaimInteractionDeliveries Захватывает доставки в Serializable транзакции разрешённой области,
// декодирует JSON шаблона и после закрытия rows проверяет текущую входящую возможность либо
// одобренную WRITE/HUMAN_EACH_EFFECT возможность пакета. Ошибка commit даёт Conflict; сообщения
// здесь не отправляются. Limit/workload передаются без изменения, сроки и fence назначает SQL.
func (repository *Repository) ClaimInteractionDeliveries(ctx context.Context, principal value.Principal, workloadInstance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryInteractionClaimDeliveries, pgx.StrictNamedArgs{
		"organization_id":   scope.organizationID,
		"workload_instance": workloadInstance,
		"claim_limit":       limit,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var deliveryRef, connectionRef, credentialRef, baseURL, teamName, channelName, locale string
		var capabilityKey, messageKey, leaseRef, fence string
		var templateRaw []byte
		var generation int64
		var gateRef, runRef string
		var externalTeam, externalChannel, externalRoot, receiptRef string
		var credential entity.IntegrationCredentialRevision
		var gateVersion int64
		var sourceCapabilityKey, approvalGateRef string
		var approvalGateVersion int64
		var expiresAt any
		if err := rows.Scan(
			&deliveryRef, &connectionRef, &credentialRef, &baseURL, &teamName, &channelName, &locale,
			&capabilityKey, &messageKey, &templateRaw, &leaseRef, &fence, &generation, &expiresAt,
			&gateRef, &gateVersion, &runRef,
			&externalTeam, &externalChannel, &externalRoot, &receiptRef,
			&credential.Ref, &credential.Revision, &credential.SecretRef, &credential.SecretUID, &credential.SecretResourceVersion,
			&credential.ContentSHA256, &credential.CreatedAt,
			&sourceCapabilityKey, &approvalGateRef, &approvalGateVersion,
		); err != nil {
			return nil, errs.ErrUnavailable
		}
		templateData := map[string]any{}
		if json.Unmarshal(templateRaw, &templateData) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, map[string]any{
			"deliveryRef": deliveryRef, "connectionRef": connectionRef, "credentialRef": credentialRef,
			"credential": credential,
			"baseURL":    baseURL, "teamName": teamName, "channelName": channelName, "locale": locale,
			"capabilityKey": capabilityKey, "messageKey": messageKey, "templateData": templateData,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
			"gateRef": gateRef, "gateVersion": gateVersion, "runRef": runRef,
			"externalTeamRef": externalTeam, "externalChannelRef": externalChannel, "externalRootPostRef": externalRoot, "acceptanceReceiptRef": receiptRef,
			"sourceCapabilityKey": sourceCapabilityKey, "approvalGateRef": approvalGateRef, "approvalGateVersion": approvalGateVersion,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows.Close()
	for _, item := range result {
		connection, definition, err := repository.interactionConnectionPackage(ctx, tx, scope.organizationID, stringMap(item, "connectionRef"))
		if err != nil {
			return nil, err
		}
		key := stringMap(item, "sourceCapabilityKey")
		if stringMap(item, "capabilityKey") == "mattermost.acknowledgements" || key == "mattermost.gate_decisions" {
			if !interactionSourceCapability(definition, key) {
				return nil, errs.ErrForbidden
			}
		} else if capability, ok := definition.Capability(key); !ok || capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || stringMap(item, "approvalGateRef") == "" {
			return nil, errs.ErrForbidden
		}
		if err := projectInteractionPackage(item, connection, definition); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// completeInteractionDelivery Validates mutually consistent success/unknown/no-effect reports and
// bounded external refs/safe codes, locks the exact live lease/fence/generation, verifies
// successful target coordinates and updates delivery state. Emits an optional non-core incident
// for failure or recovered retry; caller command owns audit/idempotency/commit.
// completeInteractionDelivery Проверяет согласованность success/unknown/no-effect, пределы внешних
// ссылок и безопасные коды, блокирует точные действующие lease/fence/generation, сверяет
// назначение успешной отправки и обновляет состояние. При сбое или успехе после повтора создаёт
// необязательный инцидент без влияния на основной запуск; аудит, идемпотентность и commit
// принадлежат внешней команде.
func (repository *Repository) completeInteractionDelivery(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.InteractionDeliveryInput)
	if !ok || payload.DeliveryRef == "" || payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if (payload.Success && (payload.UnknownOutcome || payload.ConfirmedNoEffect)) || (payload.UnknownOutcome && payload.ConfirmedNoEffect) {
		return commandOutcome{}, errs.ErrInvalid
	}
	if !payload.Success && !payload.ConfirmedNoEffect && payload.SafeErrorCode == "" {
		payload.SafeErrorCode = "INTERACTION_OUTCOME_UNKNOWN"
	}
	if payload.Success {
		if payload.ExternalPostRef == "" || len(payload.ExternalPostRef) > 128 || len(payload.ExternalThreadRef) > 128 ||
			payload.ExternalTeamRef == "" || len(payload.ExternalTeamRef) > 128 || payload.ExternalChannelRef == "" || len(payload.ExternalChannelRef) > 128 {
			return commandOutcome{}, errs.ErrInvalid
		}
	} else if !validInteractionErrorCode(payload.SafeErrorCode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var deliveryID, projectID, projectRef, rootRunID, runRef, gateID, capabilityKey string
	var targetTeam, targetChannel, targetRoot string
	var attempt, maximumAttempts int
	var createdAt time.Time
	err := tx.QueryRow(ctx, queryInteractionCompleteDeliveryResolve, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"delivery_ref":    payload.DeliveryRef,
		"lease_ref":       payload.LeaseRef,
		"fence":           payload.Fence,
		"generation":      payload.Generation,
	}).Scan(&deliveryID, &projectID, &projectRef, &rootRunID, &runRef, &gateID, &capabilityKey, &attempt, &maximumAttempts, &createdAt, &targetTeam, &targetChannel, &targetRoot)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var deliveryRef, state string
	if payload.Success && ((targetTeam != "" && payload.ExternalTeamRef != targetTeam) ||
		(targetChannel != "" && payload.ExternalChannelRef != targetChannel) ||
		(targetRoot != "" && payload.ExternalThreadRef != targetRoot)) {
		return commandOutcome{}, errs.ErrForbidden
	}
	err = tx.QueryRow(ctx, queryInteractionCompleteDeliveryUpdate, pgx.StrictNamedArgs{
		"external_team_ref":    payload.ExternalTeamRef,
		"external_channel_ref": payload.ExternalChannelRef,
		"delivery_id":          deliveryID,
		"success":              payload.Success,
		"confirmed_no_effect":  payload.ConfirmedNoEffect,
		"external_post_ref":    payload.ExternalPostRef,
		"external_thread_ref":  payload.ExternalThreadRef,
		"safe_error_code":      payload.SafeErrorCode,
		"attempt":              attempt,
	}).Scan(&deliveryRef, &state)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	messageKey := "i18n:INTERACTION_DELIVERY_FAILED"
	if payload.Success {
		messageKey = "i18n:INTERACTION_DELIVERY_SUCCEEDED"
	}
	var event *entity.RunEvent
	if !payload.Success || attempt > 1 {
		incident := projectInteractionIncident(entity.Incident{
			Ref: deliveryRef, ProjectRef: projectRef, RunRef: runRef, CreatedAt: createdAt,
		}, state, attempt, maximumAttempts)
		emitted, emitErr := repository.emitRunEventWithIncident(
			ctx, tx, scope, projectID, rootRunID, deliveryRef, "INCIDENT_LINKED",
			"", "", "", "", &incident, incident.SafeSummary, "", "",
		)
		if emitErr != nil {
			return commandOutcome{}, emitErr
		}
		event = &emitted
	}
	return commandOutcome{
		result: command.Result{Runtime: map[string]any{
			"deliveryRef": deliveryRef, "state": state, "coreRunAffected": false,
		}, Event: event},
		projectID: projectID, projectRef: projectRef, resourceKind: "INTERACTION_DELIVERY",
		resourceRef: payload.DeliveryRef, summary: messageKey,
	}, nil
}

// acceptInteractionMessage Validates bounded event/coordinate/message fields and lowercase user
// digest, resolves current package capability and linked human identity before actor-scoped
// receipt replay. Routes matched gate replies first; explicit unmatched gate data cannot fall back
// to launch. New inbound work uses the resolved human scope in the same caller transaction.
// acceptInteractionMessage Проверяет пределы события, координат и сообщения, нижний регистр хеша
// пользователя, разрешает текущую возможность пакета и связанную человеческую личность до поиска
// её квитанции. Сначала обрабатывает ответ шлюзу; явные несовпавшие данные шлюза не переходят в
// запуск. Новая входящая работа использует область человека в той же транзакции.
func (repository *Repository) acceptInteractionMessage(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.InteractionMessageInput)
	if !ok || payload.ConnectionRef == "" || payload.ExternalEventRef == "" || payload.ExternalPostRef == "" ||
		payload.ExternalTeamRef == "" || len(payload.ExternalTeamRef) > 128 || payload.ExternalChannelRef == "" || !lowerHexDigest(payload.ExternalUserDigest) ||
		len(payload.ExternalEventRef) > 256 || len(payload.ExternalPostRef) > 128 || len(payload.ExternalRootPostRef) > 128 ||
		len(payload.ExternalChannelRef) > 128 || len(payload.Message) > 16<<10 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var connectionID string
	if err := tx.QueryRow(ctx, queryInteractionResolveConnection, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"connection_ref":  payload.ConnectionRef,
	}).Scan(&connectionID); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, definition, err := repository.interactionConnectionPackage(ctx, tx, scope.organizationID, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	capabilityKey := "mattermost.inbound"
	if payload.GateRef != "" || payload.Decision != "" {
		capabilityKey = "mattermost.gate_decisions"
	}
	if err := validateInteractionSourceInput(definition, capabilityKey, payload.GateRef, payload.Decision); err != nil {
		return commandOutcome{}, err
	}
	human, err := repository.resolveInteractionIdentity(ctx, tx, scope, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	scope = human
	eventDigest := interactionDigest(payload.ConnectionRef, payload.ExternalEventRef)
	var previousOutcome, previousRunRef, previousGateRef string
	err = tx.QueryRow(ctx, queryInteractionFindMessageReceipt, pgx.StrictNamedArgs{
		"subject_id": scope.actorID, "identity_id": scope.interactionIdentityID,
		"organization_id":       scope.organizationID,
		"connection_id":         connectionID,
		"external_event_digest": eventDigest,
	}).Scan(&previousOutcome, &previousRunRef, &previousGateRef)
	if err == nil {
		acceptedRef := previousRunRef
		if acceptedRef == "" {
			acceptedRef = previousGateRef
		}
		return interactionMessageOutcome(payload.ConnectionRef, "DUPLICATE", "i18n:MATTERMOST_EVENT_ALREADY_PROCESSED", acceptedRef, "", ""), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	gateOutcome, gateMatched, err := repository.acceptInteractionGateDecision(ctx, tx, scope, input, connectionID, eventDigest)
	if err != nil || gateMatched {
		return gateOutcome, err
	}
	if payload.Decision != "" || payload.GateRef != "" || payload.ExpectedGateVersion != 0 {
		return commandOutcome{}, errs.ErrNotFound
	}
	return repository.acceptInteractionInbound(ctx, tx, scope, input, connectionID, eventDigest)
}

// acceptInteractionGateDecision Locks the gate matched by current source/external coordinates,
// checks explicit gate/run/version and gate.resolve, then records ignored/stale replies or
// delegates a valid OPEN decision to the canonical gate resolver. Conflict/version mismatch
// becomes a stale receipt; receipt/acknowledgement queue and gate changes share the caller
// transaction without external sending.
// acceptInteractionGateDecision Блокирует шлюз текущего источника и внешних координат, проверяет
// явные ссылки шлюза и запуска, версию и gate.resolve, затем фиксирует проигнорированный или
// устаревший ответ либо передаёт допустимое OPEN решение каноническому resolver.
// Conflict/VersionMismatch превращается в устаревшую квитанцию; она, очередь подтверждений и
// изменение шлюза принадлежат одной транзакции без внешней отправки.
func (repository *Repository) acceptInteractionGateDecision(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, connectionID, eventDigest string) (commandOutcome, bool, error) {
	payload := input.Payload.(command.InteractionMessageInput)
	var deliveryID, grantID, gateID, gateRef, gateState, projectID, projectRef, rootRunID, runRef string
	var gateVersion int64
	var allowed []string
	err := tx.QueryRow(ctx, queryInteractionFindGateDelivery, pgx.StrictNamedArgs{
		"external_team_ref":      payload.ExternalTeamRef,
		"external_channel_ref":   payload.ExternalChannelRef,
		"organization_id":        scope.organizationID,
		"connection_ref":         payload.ConnectionRef,
		"external_root_post_ref": payload.ExternalRootPostRef,
		"external_post_ref":      payload.ExternalPostRef,
	}).Scan(&deliveryID, &grantID, &gateID, &gateRef, &gateVersion, &gateState, &allowed, &projectID, &projectRef, &rootRunID, &runRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, false, nil
	}
	if err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	if payload.Decision != "" && (payload.GateRef != gateRef || payload.RunRef != runRef || payload.ExpectedGateVersion != gateVersion) {
		return commandOutcome{}, true, errs.ErrVersionMismatch
	}
	if err := repository.requireAccess(ctx, tx, scope, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: gateRef}); err != nil {
		return commandOutcome{}, true, err
	}
	receipt := interactionReceipt{
		connectionID: connectionID, grantID: grantID, gateID: gateID, rootRunRef: runRef,
		projectID: projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
	}
	decision := payload.Decision
	if decision == "" || !contains([]string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}, decision) || !contains(allowed, decision) {
		receipt.outcome = "IGNORED"
		if err := repository.insertInteractionReceipt(ctx, tx, scope, payload, receipt); err != nil {
			return commandOutcome{}, true, err
		}
		return interactionMessageOutcome(gateRef, "IGNORED", "i18n:MATTERMOST_GATE_COMMAND_HELP", gateRef, projectID, projectRef), true, nil
	}
	receipt.decision = decision
	if gateState != "OPEN" {
		return repository.staleInteractionGateOutcome(ctx, tx, scope, payload, receipt, gateRef, projectRef)
	}
	nested := input
	nested.Kind = command.ResolveOwnerGate
	nested.Mutation.ExpectedVersion = &gateVersion
	nested.Payload = command.GateResolutionInput{GateRef: gateRef, Decision: decision, Comment: truncate(strings.TrimSpace(payload.Message), 2000)}
	outcome, err := repository.resolveGate(ctx, tx, scope, nested)
	if errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrVersionMismatch) {
		return repository.staleInteractionGateOutcome(ctx, tx, scope, payload, receipt, gateRef, projectRef)
	}
	if err != nil {
		return commandOutcome{}, true, err
	}
	receipt.outcome = "GATE_RESOLVED"
	if err := repository.insertInteractionReceipt(ctx, tx, scope, payload, receipt); err != nil {
		return commandOutcome{}, true, err
	}
	outcome.result.Runtime = map[string]any{"outcome": "GATE_RESOLVED", "messageKey": "i18n:MATTERMOST_GATE_RESOLVED", "acceptedResourceRef": gateRef}
	outcome.resourceKind = "INTERACTION_DECISION"
	outcome.summary = "i18n:MATTERMOST_GATE_RESOLVED"
	return outcome, true, nil
}

// acceptInteractionInbound Reads and closes at most two current inbound routes; ambiguity or blank
// text records an ignored receipt. A unique route requires exact agent/workflow launch access and
// delegates to canonical launch with the resolved human scope, then records the run receipt
// atomically. It does not accept caller-supplied target authority.
// acceptInteractionInbound Читает и закрывает до двух текущих входящих маршрутов; неоднозначность
// или пустой текст дают квитанцию IGNORED. Единственный маршрут требует точного права запуска
// агента или workflow и передаётся каноническому запуску с областью человека, после чего атомарно
// пишется квитанция. Цель из произвольных данных клиента не назначает полномочия.
func (repository *Repository) acceptInteractionInbound(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, connectionID, eventDigest string) (commandOutcome, error) {
	payload := input.Payload.(command.InteractionMessageInput)
	rows, err := tx.Query(ctx, queryInteractionListInboundGrants, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"connection_ref":  payload.ConnectionRef,
	})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	// inboundRoute Keeps the resolved grant and target/project identifiers from one locked inbound SQL
	// row. Two rows deliberately signal ambiguous routing; these internal IDs are not selected from
	// the external message.
	// inboundRoute Хранит разрешённые идентификаторы разрешения, цели и проекта из заблокированной
	// строки входящей выборки. Две строки намеренно выявляют неоднозначность; внутренние ID не
	// выбираются из внешнего сообщения.
	type inboundRoute struct{ grantID, grantRef, targetKind, targetRef, projectID, projectRef string }
	routes := []inboundRoute{}
	for rows.Next() {
		var item inboundRoute
		if err := rows.Scan(&item.grantID, &item.grantRef, &item.targetKind, &item.targetRef, &item.projectID, &item.projectRef); err != nil {
			rows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		routes = append(routes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return commandOutcome{}, errs.ErrUnavailable
	}
	rows.Close()
	if len(routes) != 1 || strings.TrimSpace(payload.Message) == "" {
		if err := repository.insertInteractionReceipt(ctx, tx, scope, payload, interactionReceipt{
			connectionID: connectionID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest, outcome: "IGNORED",
		}); err != nil {
			return commandOutcome{}, err
		}
		return interactionMessageOutcome(payload.ConnectionRef, "IGNORED", "i18n:MATTERMOST_INBOUND_ROUTE_UNAVAILABLE", payload.ConnectionRef, "", ""), nil
	}
	selected := routes[0]
	permission := "agent.launch"
	if selected.targetKind == "WORKFLOW" {
		permission = "workflow.launch"
	}
	if err := repository.requireAccess(ctx, tx, scope, permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: selected.targetKind, ResourceRef: selected.targetRef}); err != nil {
		return commandOutcome{}, err
	}
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{
		ProjectRef: selected.projectRef,
		Title:      "i18n:MATTERMOST_INBOUND_RUN",
		Task:       strings.TrimSpace(payload.Message),
		Source:     "MATTERMOST",
		Target:     entity.RunTarget{Type: selected.targetKind, Ref: selected.targetRef},
	}
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Run == nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := repository.insertInteractionReceipt(ctx, tx, scope, payload, interactionReceipt{
		connectionID: connectionID, grantID: selected.grantID, rootRunRef: outcome.result.Run.Ref,
		projectID: selected.projectID, eventDigest: eventDigest, userDigest: payload.ExternalUserDigest,
		outcome: "RUN_STARTED",
	}); err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Runtime = map[string]any{"outcome": "RUN_STARTED", "messageKey": "i18n:MATTERMOST_RUN_ACCEPTED", "acceptedResourceRef": outcome.result.Run.Ref}
	outcome.resourceKind = "INTERACTION_MESSAGE"
	outcome.summary = "i18n:MATTERMOST_RUN_ACCEPTED"
	return outcome, nil
}

// interactionReceipt Carries resolved connection/grant/gate/run/project links, event/user digests
// and the fixed processing outcome/decision for transactional receipt insertion. External
// plaintext message and credential values are excluded; identity/subject come separately from the
// trusted scope.
// interactionReceipt Хранит разрешённые связи соединения, разрешения, шлюза, запуска и проекта,
// хеши события и пользователя, исход и решение для транзакционной вставки квитанции. Текст
// сообщения и значения credential не включены; личность и субъект берутся отдельно из доверенной
// области.
type interactionReceipt struct {
	connectionID, grantID, gateID, rootRunRef, projectID string
	eventDigest, userDigest, outcome, decision           string
}

// insertInteractionReceipt Generates a receipt ref, chooses the explicit root post or original
// post fallback and executes the owner-scoped receipt/acknowledgement CTE with resolved
// subject/identity and metadata. Reference generation errors propagate, SQL errors become
// unavailable; no commit or external send occurs.
// insertInteractionReceipt Создаёт ссылку квитанции, выбирает явное корневое либо исходное
// сообщение и исполняет CTE квитанции с очередью подтверждений, разрешёнными субъектом, личностью
// и метаданными. Ошибка генерации ссылки возвращается, SQL-сбой даёт Unavailable; commit и внешней
// отправки нет.
func (repository *Repository) insertInteractionReceipt(ctx context.Context, tx pgx.Tx, scope scope, message command.InteractionMessageInput, receipt interactionReceipt) error {
	ref, err := newRef("irc")
	if err != nil {
		return err
	}
	rootPost := message.ExternalRootPostRef
	if rootPost == "" {
		rootPost = message.ExternalPostRef
	}
	if _, err := tx.Exec(ctx, queryInteractionInsertMessageReceipt, pgx.StrictNamedArgs{
		"external_team_ref":      message.ExternalTeamRef,
		"external_channel_ref":   message.ExternalChannelRef,
		"external_root_post_ref": rootPost,
		"receipt_ref":            ref,
		"organization_id":        scope.organizationID,
		"project_id":             receipt.projectID,
		"connection_id":          receipt.connectionID,
		"grant_id":               receipt.grantID,
		"root_run_ref":           receipt.rootRunRef,
		"gate_id":                receipt.gateID,
		"external_event_digest":  receipt.eventDigest,
		"external_user_digest":   receipt.userDigest,
		"outcome":                receipt.outcome,
		"decision":               receipt.decision,
		"identity_id":            scope.interactionIdentityID,
		"subject_id":             scope.actorID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// interactionMessageOutcome Builds the stable interaction-message command result with outcome/i18n
// key/accepted resource and caller-resolved project metadata. It performs no write, authorization
// or event emission.
// interactionMessageOutcome Строит стабильный результат команды взаимодействия с исходом,
// i18n-ключом, принятой ссылкой и разрешёнными вызывающим данными проекта. Не пишет состояние, не
// авторизует и не публикует событие.
func interactionMessageOutcome(resourceRef, outcome, messageKey, acceptedRef, projectID, projectRef string) commandOutcome {
	return commandOutcome{
		result: command.Result{Runtime: map[string]any{
			"outcome": outcome, "messageKey": messageKey, "acceptedResourceRef": acceptedRef,
		}},
		projectID: projectID, projectRef: projectRef, resourceKind: "INTERACTION_MESSAGE",
		resourceRef: resourceRef, summary: messageKey,
	}
}

// enqueueGateInteractionDeliveries Executes gate-delivery enqueue SQL in the supplied transaction
// with already resolved organization/project/root/gate IDs, translating SQL failure to
// unavailable. Enqueue is local durable intent, not an external effect or approval.
// enqueueGateInteractionDeliveries Ставит доставки шлюза в очередь переданной транзакции по уже
// разрешённым ID организации, проекта, корневого запуска и шлюза. SQL-сбой даёт Unavailable;
// очередь фиксирует локальное намерение, а не внешний эффект или одобрение.
func (repository *Repository) enqueueGateInteractionDeliveries(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, gateID string) error {
	if _, err := tx.Exec(ctx, queryInteractionEnqueueGateDeliveries, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"project_id":      projectID,
		"root_run_id":     rootRunID,
		"gate_id":         gateID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// enqueueTerminalInteractionDeliveries Skips projectless runs and otherwise delegates terminal
// notifications to the approval-backed interaction path in the same transaction. It neither
// directly sends a result nor bypasses per-effect human approval.
// enqueueTerminalInteractionDeliveries Пропускает запуски без проекта, остальные передаёт пути
// завершённых уведомлений с одобрением в той же транзакции. Результат напрямую не отправляет и
// одобрение каждого эффекта человеком не обходит.
func (repository *Repository) enqueueTerminalInteractionDeliveries(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID string) error {
	if projectID == "" {
		return nil
	}
	return repository.enqueueApprovedTerminalInteraction(ctx, tx, scope, projectID, rootRunID)
}

// interactionDigest Returns lowercase SHA-256 hex over connection ref, one NUL separator and
// external event ref for deterministic per-connection event deduplication. It is an unkeyed
// locator digest, not authentication or a signature.
// interactionDigest Возвращает SHA-256 в нижнем hex-регистре от ссылки соединения, разделителя NUL
// и ссылки внешнего события для детерминированной дедупликации. Это хеш поиска без ключа, а не
// аутентификация или подпись.
func interactionDigest(connectionRef, externalEventRef string) string {
	digest := sha256.Sum256([]byte(connectionRef + "\x00" + externalEventRef))
	return hex.EncodeToString(digest[:])
}

// lowerHexDigest Accepts exactly 64 ASCII lowercase hexadecimal characters for a SHA-256
// representation; rejects uppercase, prefixes, whitespace and other lengths without decoding or
// checking provenance.
// lowerHexDigest Допускает ровно 64 ASCII-символа нижнего hex-регистра для представления SHA-256.
// Отклоняет верхний регистр, префиксы, пробелы и иную длину; происхождение хеша не проверяет.
func lowerHexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

// validInteractionErrorCode Accepts only the closed interaction delivery failure-code set,
// rejecting arbitrary adapter text before persistence. This shape check does not determine whether
// an external effect occurred.
// validInteractionErrorCode Допускает только закрытый набор кодов ошибок доставки взаимодействия,
// отклоняя произвольный текст адаптера перед сохранением. Проверка формы не определяет, произошёл
// ли внешний эффект.
func validInteractionErrorCode(value string) bool {
	switch value {
	case "INTERACTION_CONFIGURATION_INVALID", "INTERACTION_CREDENTIAL_UNAVAILABLE", "INTERACTION_FORBIDDEN",
		"INTERACTION_RATE_LIMITED", "INTERACTION_UNAVAILABLE", "INTERACTION_RESPONSE_INVALID",
		"INTERACTION_LEASE_EXPIRED", "INTERACTION_OUTCOME_UNKNOWN":
		return true
	default:
		return false
	}
}

// staleInteractionGateOutcome stores a STALE receipt from caller-resolved owner/identity links
// and builds the existing matched-gate response only after insertion succeeds. The receipt is
// copied before its outcome is set; decision and external-root fallback remain unchanged. Caller
// paths retain gate.resolve/version checks, gate locking and transaction commit; the SQL may
// enqueue an acknowledgement but this helper never sends externally or resolves the gate.
// staleInteractionGateOutcome сохраняет квитанцию STALE по разрешённым вызывающим связям владельца
// и личности, формирует прежний ответ найденного шлюза только после успешной вставки. Исход
// задаётся в копии квитанции; решение и выбор корневого сообщения сохраняются. Проверки
// gate.resolve и версии, блокировка шлюза и commit остаются у вызывающих. SQL может поставить
// подтверждение в очередь, но функция не отправляет его и не разрешает шлюз.
func (repository *Repository) staleInteractionGateOutcome(ctx context.Context, tx pgx.Tx, current scope, message command.InteractionMessageInput, receipt interactionReceipt, gateRef, projectRef string) (commandOutcome, bool, error) {
	receipt.outcome = "STALE"
	if err := repository.insertInteractionReceipt(ctx, tx, current, message, receipt); err != nil {
		return commandOutcome{}, true, err
	}
	return interactionMessageOutcome(gateRef, "STALE", "i18n:MATTERMOST_GATE_STALE", gateRef, receipt.projectID, projectRef), true, nil
}
