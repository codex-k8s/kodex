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
	"github.com/jackc/pgx/v5"
)

var (
	// queryTelegramSourceCandidate rechecks current project, Run and connection eligibility of the authenticated subscription approver under the owner transaction.
	// queryTelegramSourceCandidate повторно проверяет доступ подписавшего подписку к проекту, Run и подключению внутри owner transaction.
	//go:embed sql/telegram_source_candidate.sql
	queryTelegramSourceCandidate string
	// queryTelegramEnqueueDelivery records one immutable server intent and suppresses repeated source events independently of subscription renewal.
	// queryTelegramEnqueueDelivery записывает одно неизменяемое серверное намерение и подавляет повтор события независимо от продления подписки.
	//go:embed sql/telegram_enqueue_delivery.sql
	queryTelegramEnqueueDelivery string
)

// enqueueTelegramDeliveryTx adds bounded optional work only for a current explicit subscription; stale or absent optional configuration cannot fail the core Run.
// enqueueTelegramDeliveryTx добавляет ограниченную необязательную работу лишь для текущей явной подписки; устаревшая либо отсутствующая настройка не приводит к сбою основного Run.
func (repository *Repository) enqueueTelegramDeliveryTx(ctx context.Context, tx pgx.Tx, current scope, projectID, rootRunID, gateID string) error {
	if projectID == "" || integrationpackage.ValidateTelegramPublicOrigin(repository.interactionPublicOrigin) != nil {
		return nil
	}
	var snapshot integrationpackage.TelegramDeliverySnapshot
	var id, connectionID, grantID, state, locale string
	var raw []byte
	var expiresAt, now time.Time
	var runVersion, pending int64
	err := tx.QueryRow(ctx, queryTelegramSourceCandidate, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"project_id": projectID, "root_run_id": rootRunID, "gate_id": gateID}).Scan(&id, &snapshot.SubscriptionRef,
		&snapshot.SubscriptionVersion, &snapshot.ScopeDigest, &raw, &expiresAt, &snapshot.ConnectionVersion, &snapshot.DefinitionDigest,
		&snapshot.CredentialRevisionRef, &connectionID, &grantID, &snapshot.RunRef, &runVersion, &state, &snapshot.ProjectRef, &locale,
		&snapshot.GateRef, &snapshot.GateVersion, &now, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &snapshot.Configuration) != nil {
		return errs.ErrUnavailable
	}
	configuration, err := integrationpackage.ParseTelegramConfiguration(snapshot.Configuration)
	if err != nil {
		return nil
	}
	if gateID != "" {
		snapshot.EventType = "HUMAN_GATE"
		snapshot.TemplateKey = "TELEGRAM_HUMAN_GATE_V1"
	} else if state == "SUCCEEDED" {
		snapshot.EventType = "FINISHED"
		snapshot.TemplateKey = "TELEGRAM_FINISHED_V1"
	} else {
		snapshot.EventType = "FAILED"
		snapshot.TemplateKey = "TELEGRAM_FAILED_V1"
	}
	// Тип события проверяется также текущим owner query, без пользовательского template input.
	if pending >= int64(configuration.QueueLimit) {
		return nil
	}
	snapshot.PublicOrigin = repository.interactionPublicOrigin
	snapshot.ExpiresAt = now.UTC().Add(time.Duration(configuration.DeliveryTTLSeconds) * time.Second)
	if expiresAt.Before(snapshot.ExpiresAt) {
		snapshot.ExpiresAt = expiresAt.UTC()
	}
	availableAt, err := integrationpackage.NextAllowedTelegramDelivery(configuration, now.UTC())
	if err != nil || !availableAt.Before(snapshot.ExpiresAt) {
		return nil
	}
	digest, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	if err != nil {
		return nil
	}
	sourceKey := "run:" + snapshot.RunRef + ":" + snapshot.EventType
	if gateID != "" {
		sourceKey = "gate:" + snapshot.GateRef + ":" + strconv.FormatInt(snapshot.GateVersion, 10)
	}
	ref, err := newRef("idl")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, queryTelegramEnqueueDelivery, pgx.StrictNamedArgs{"ref": ref, "subscription_id": id,
		"organization_id": current.organizationID, "root_run_id": rootRunID, "template_key": snapshot.TemplateKey,
		"snapshot": asJSON(snapshot), "input_digest": digest, "source_key": sourceKey, "gate_id": gateID,
		"expires_at": snapshot.ExpiresAt, "available_at": availableAt, "subscription_version": snapshot.SubscriptionVersion,
		"event_type": snapshot.EventType})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
