package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

var (
	// queryTelegramCompleteResolve locks only an exact live Telegram lease; Mattermost completion fields cannot select this path.
	// queryTelegramCompleteResolve блокирует лишь точную действующую Telegram lease; поля Mattermost completion не выбирают этот путь.
	//go:embed sql/telegram_complete_resolve.sql
	queryTelegramCompleteResolve string
	// queryTelegramCompleteUpdate commits receipt, no-effect refund and bounded retry together, preserving possible-effect history for unknown outcomes.
	// queryTelegramCompleteUpdate фиксирует receipt, возврат no-effect quota и bounded retry вместе, сохраняя историю возможного эффекта для unknown outcomes.
	//go:embed sql/telegram_complete_update.sql
	queryTelegramCompleteUpdate string
)

// telegramDeliveryCode admits only adapter-defined safe classifications; arbitrary provider error strings cannot enter durable or public state.
// telegramDeliveryCode допускает лишь определённые адаптером безопасные классификации; произвольные provider error strings не попадают в durable либо public state.
func telegramDeliveryCode(code string) bool {
	switch code {
	case "TELEGRAM_CONFIGURATION_INVALID", "TELEGRAM_PREFLIGHT_UNAVAILABLE", "TELEGRAM_PROVIDER_REJECTED", "TELEGRAM_RATE_LIMITED", "TELEGRAM_OUTCOME_UNKNOWN":
		return true
	default:
		return false
	}
}

// completeTelegramDeliveryTx validates actual coordinates and fenced input, records an immutable receipt through the existing owner command lifecycle, and never retries a possible effect.
// completeTelegramDeliveryTx проверяет фактические координаты и fenced input, записывает immutable receipt прежним owner command lifecycle и никогда не повторяет возможный эффект.
func (repository *Repository) completeTelegramDeliveryTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.InteractionDeliveryInput) (commandOutcome, error) {
	if input.Principal.CallerWorkload != "interaction-gateway" || input.Principal.Permission != "platform.interactions.deliveries.complete" {
		return commandOutcome{}, errs.ErrForbidden
	}
	if payload.DeliveryRef == "" || payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 || !lowerHexDigest(payload.InputDigest) ||
		payload.ExternalTeamRef != "" || payload.ExternalChannelRef != "" || payload.ExternalPostRef != "" || payload.ExternalThreadRef != "" ||
		payload.RetryAfterSeconds < 0 || payload.RetryAfterSeconds > 3600 || payload.Success && (payload.UnknownOutcome || payload.ConfirmedNoEffect) ||
		payload.UnknownOutcome && payload.ConfirmedNoEffect {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success && (payload.TelegramReceipt == nil || payload.SafeErrorCode != "" || payload.RetryAfterSeconds != 0) ||
		!payload.Success && (payload.TelegramReceipt != nil || !telegramDeliveryCode(payload.SafeErrorCode)) ||
		payload.RetryAfterSeconds > 0 && (!payload.ConfirmedNoEffect || payload.SafeErrorCode != "TELEGRAM_RATE_LIMITED") {
		return commandOutcome{}, errs.ErrInvalid
	}
	var id, projectID, projectRef, digest, subscriptionID string
	var raw []byte
	var started *time.Time
	var subscriptionVersion int64
	var attempt, maximum int
	var expires, now time.Time
	err := tx.QueryRow(ctx, queryTelegramCompleteResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"delivery_ref": payload.DeliveryRef, "lease_ref": payload.LeaseRef, "fence": payload.Fence, "generation": payload.Generation}).Scan(
		&id, &projectID, &projectRef, &raw, &digest, &started, &subscriptionID, &subscriptionVersion, &attempt, &maximum, &expires, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var snapshot integrationpackage.TelegramDeliverySnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	actual, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	if err != nil || actual != digest || digest != payload.InputDigest {
		return commandOutcome{}, errs.ErrForbidden
	}
	configuration, err := integrationpackage.ParseTelegramConfiguration(snapshot.Configuration)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	state, code := "UNKNOWN_OUTCOME", "TELEGRAM_OUTCOME_UNKNOWN"
	var receipt any
	if payload.Success {
		observed := payload.TelegramReceipt
		if started == nil || observed.BotID != configuration.BotID || observed.ChatID != configuration.ChatID || observed.TopicID != configuration.TopicID ||
			observed.MessageID < 1 || observed.MessageID > integrationpackage.TelegramMaximumIdentifier {
			return commandOutcome{}, errs.ErrForbidden
		}
		state, code = "SUCCEEDED", ""
		receipt = observed
	} else if payload.ConfirmedNoEffect {
		state, code = "CANCELLED", payload.SafeErrorCode
	}
	retry := payload.ConfirmedNoEffect && attempt < maximum && (payload.RetryAfterSeconds > 0 || code == "TELEGRAM_PREFLIGHT_UNAVAILABLE")
	delay := time.Duration(payload.RetryAfterSeconds) * time.Second
	if delay == 0 {
		delay = time.Duration(15*attempt) * time.Second
	}
	next := now.Add(delay)
	if !next.Before(expires) {
		retry = false
	}
	if retry {
		state = "FAILED"
	}
	if _, err := tx.Exec(ctx, queryTelegramCompleteUpdate, pgx.StrictNamedArgs{"subscription_id": subscriptionID, "subscription_version": subscriptionVersion,
		"refund": started != nil && payload.ConfirmedNoEffect, "delivery_id": id, "state": state, "safe_error_code": code,
		"receipt": asJSON(receipt), "retry": retry, "available_at": next}); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"deliveryRef": payload.DeliveryRef, "state": state, "coreRunAffected": false}},
		projectID: projectID, projectRef: projectRef, resourceKind: "PROJECT", resourceRef: projectRef, summary: "i18n:TELEGRAM_DELIVERY_UPDATED"}, nil
}
