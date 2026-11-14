package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryTelegramClaimDeliveries distinguishes pre-Begin expiry from possible sends and leases only exact current subscription work with fresh owner eligibility.
// queryTelegramClaimDeliveries отличает истечение до Begin от возможных отправок и арендует лишь точную текущую работу подписки со свежим доступом владельца.
//
//go:embed sql/telegram_claim_deliveries.sql
var queryTelegramClaimDeliveries string

// queryTelegramExpireSubscriptions durably closes a bounded tenant-scoped batch of expired subscriptions, grants and outstanding leases in the claim owner's transaction.
// queryTelegramExpireSubscriptions устойчиво закрывает bounded tenant-scoped batch истёкших подписок, grants и незавершённых leases в транзакции владельца claim.
//
//go:embed sql/telegram_expire_subscriptions.sql
var queryTelegramExpireSubscriptions string

// claimTelegramDeliveriesTx uses the existing delivery ledger and caller transaction, validates every immutable input, then projects exact package and credential pins.
// claimTelegramDeliveriesTx использует прежний delivery ledger и транзакцию вызывающего, проверяет каждый immutable input, затем проецирует точные package и credential pins.
func (repository *Repository) claimTelegramDeliveriesTx(ctx context.Context, tx pgx.Tx, current scope, instance string, limit int32) ([]map[string]any, error) {
	items := []map[string]any{}
	if limit < 1 {
		return items, nil
	}
	if _, err := tx.Exec(ctx, queryTelegramExpireSubscriptions, current.organizationID); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryTelegramClaimDeliveries, pgx.StrictNamedArgs{"organization_id": current.organizationID, "workload_instance": instance, "claim_limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var deliveryRef, connectionRef, digest, leaseRef, fence, locale string
		var raw []byte
		var generation int64
		var expiresAt time.Time
		var credential entity.IntegrationCredentialRevision
		if err := rows.Scan(&deliveryRef, &connectionRef, &raw, &digest, &leaseRef, &fence, &generation, &expiresAt, &locale,
			&credential.Ref, &credential.Revision, &credential.SecretRef, &credential.SecretUID, &credential.SecretResourceVersion, &credential.ContentSHA256, &credential.CreatedAt); err != nil {
			return nil, errs.ErrUnavailable
		}
		var snapshot integrationpackage.TelegramDeliverySnapshot
		if json.Unmarshal(raw, &snapshot) != nil {
			return nil, errs.ErrUnavailable
		}
		actual, err := integrationpackage.TelegramDeliveryDigest(snapshot)
		if err != nil || actual != digest || snapshot.PublicOrigin != repository.interactionPublicOrigin || snapshot.CredentialRevisionRef != credential.Ref {
			return nil, errs.ErrForbidden
		}
		items = append(items, map[string]any{"deliveryRef": deliveryRef, "connectionRef": connectionRef, "credentialRef": credential.Ref,
			"credential": credential, "telegram": snapshot, "inputDigest": digest, "baseURL": integrationpackage.TelegramAPIOrigin,
			"locale": locale, "capabilityKey": "telegram.notifications", "sourceCapabilityKey": "telegram.notifications",
			"messageKey": snapshot.TemplateKey, "templateData": map[string]any{}, "leaseRef": leaseRef, "fence": fence, "generation": generation,
			"expiresAt": expiresAt, "gateRef": snapshot.GateRef, "gateVersion": snapshot.GateVersion, "runRef": snapshot.RunRef})
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	rows.Close()
	for _, item := range items {
		connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, stringMap(item, "connectionRef"))
		if err != nil {
			return nil, err
		}
		snapshot := item["telegram"].(integrationpackage.TelegramDeliverySnapshot)
		if connection.definitionKey != "telegram" || connection.version != snapshot.ConnectionVersion || connection.definitionDigest != snapshot.DefinitionDigest {
			return nil, errs.ErrForbidden
		}
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, stringMap(item, "connectionRef"), connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
		if err != nil {
			return nil, err
		}
		if !definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction) {
			return nil, errs.ErrForbidden
		}
		if err := projectInteractionPackage(item, connection, definition); err != nil {
			return nil, err
		}
	}
	return items, nil
}
