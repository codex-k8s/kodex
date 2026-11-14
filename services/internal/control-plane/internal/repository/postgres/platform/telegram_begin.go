package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryTelegramBeginResolve locks the exact fenced delivery and all current subscription grant and connection owners before effect admission.
	// queryTelegramBeginResolve блокирует точную fenced delivery и всех текущих владельцев subscription, grant и connection до effect admission.
	//go:embed sql/telegram_begin_resolve.sql
	queryTelegramBeginResolve string
	// queryTelegramBeginAdmit atomically reserves bounded quota and records the irreversible possible-send marker once.
	// queryTelegramBeginAdmit атомарно резервирует ограниченную quota и однократно записывает необратимый маркер возможной отправки.
	//go:embed sql/telegram_begin_admit.sql
	queryTelegramBeginAdmit string
	// queryTelegramBeginDefer closes only proven pre-send claims, scheduling quiet/rate work or terminating revoked scope without an external effect.
	// queryTelegramBeginDefer закрывает лишь доказанные pre-send claims, планируя quiet/rate работу либо завершая отозванную scope без внешнего эффекта.
	//go:embed sql/telegram_begin_defer.sql
	queryTelegramBeginDefer string
)

// BeginInteractionDelivery issues one committed permission for one immutable fenced input; lost ACK or repeated Begin never produces permission for another send.
// BeginInteractionDelivery выдаёт одно зафиксированное разрешение для одного immutable fenced input; lost ACK либо повтор Begin не создаёт права ещё одной отправки.
func (repository *Repository) BeginInteractionDelivery(ctx context.Context, p value.Principal, input port.InteractionDeliveryBeginInput) (entity.InteractionDeliveryBegin, error) {
	result := entity.InteractionDeliveryBegin{DeliveryRef: input.DeliveryRef, InputDigest: input.InputDigest}
	if p.CallerWorkload != "interaction-gateway" || p.Permission != "platform.interactions.deliveries.begin" {
		return result, errs.ErrForbidden
	}
	current, tx, err := repository.beginScopedTransaction(ctx, p, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deliveryID, subscriptionID, storedRef, digest string
	var raw, configurationRaw []byte
	var started *time.Time
	var expires, now, window time.Time
	var effects int
	var eligible bool
	err = tx.QueryRow(ctx, queryTelegramBeginResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"delivery_ref": input.DeliveryRef, "lease_ref": input.LeaseRef, "fence": input.Fence, "generation": input.Generation}).Scan(
		&deliveryID, &subscriptionID, &storedRef, &raw, &digest, &started, &expires, &now, &window, &effects, &configurationRaw, &eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrConflict
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var snapshot integrationpackage.TelegramDeliverySnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return result, errs.ErrUnavailable
	}
	actual, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	if err != nil || digest != input.InputDigest || actual != digest || storedRef != input.DeliveryRef || snapshot.PublicOrigin != repository.interactionPublicOrigin {
		return result, errs.ErrForbidden
	}
	if started != nil {
		result.AlreadyStarted = true
		result.SafeErrorCode = "TELEGRAM_ALREADY_STARTED"
	} else {
		var configuration map[string]string
		if json.Unmarshal(configurationRaw, &configuration) != nil {
			return result, errs.ErrUnavailable
		}
		settings, err := integrationpackage.ParseTelegramConfiguration(configuration)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		next, err := integrationpackage.NextAllowedTelegramDelivery(settings, now.UTC())
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if effects >= settings.RatePerMinute && window.Add(time.Minute).After(next) {
			next = window.Add(time.Minute).UTC()
		}
		cancel := !eligible || !expires.After(now) || !next.Before(expires)
		if cancel || next.After(now) {
			result.SafeErrorCode = "TELEGRAM_ADMISSION_DEFERRED"
			if cancel {
				result.SafeErrorCode = "TELEGRAM_SCOPE_REVOKED"
			}
			if _, err := tx.Exec(ctx, queryTelegramBeginDefer, pgx.StrictNamedArgs{"delivery_id": deliveryID, "cancel": cancel,
				"safe_error_code": result.SafeErrorCode, "available_at": next}); err != nil {
				return result, errs.ErrUnavailable
			}
		} else {
			tag, err := tx.Exec(ctx, queryTelegramBeginAdmit, pgx.StrictNamedArgs{"subscription_id": subscriptionID, "delivery_id": deliveryID, "now": now})
			if err != nil || tag.RowsAffected() != 1 {
				return result, errs.ErrConflict
			}
			result.Allowed = true
		}
	}
	if err := tx.Commit(ctx); err != nil {
		result.Allowed = false
		return result, errs.ErrConflict
	}
	return result, nil
}
