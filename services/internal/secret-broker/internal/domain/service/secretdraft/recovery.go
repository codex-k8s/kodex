package secretdraft

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// recoveryFailure keeps a diagnostic stage and unwrap-able cause behind a fixed Error message; stage
// values originate in reconciliation callsites.
// recoveryFailure сохраняет diagnostic stage и разворачиваемую cause за фиксированным Error message; stage
// values задаются reconciliation callsites.
type recoveryFailure struct {
	stage string
	cause error
}

// Error returns a fixed recovery diagnostic without interpolating the cause, descriptors or stage.
// Error возвращает фиксированную recovery diagnostic без подстановки cause, descriptors и stage.
func (failure recoveryFailure) Error() string { return "secret draft recovery failed" }

// Unwrap preserves cause identity for errors.Is/As and classification while Error remains bounded.
// Unwrap сохраняет identity cause для errors.Is/As и классификации при ограниченном Error.
func (failure recoveryFailure) Unwrap() error { return failure.cause }

// RecoveryFailureStage extracts the first recovery wrapper's stage, qualifying cleanup acknowledgement
// failures with their nested Stage; unwrapped errors return unknown.
// RecoveryFailureStage извлекает stage первого recovery wrapper, дополняя cleanup acknowledgement failure
// вложенным Stage; ошибки без wrapper дают unknown.
func RecoveryFailureStage(err error) string {
	var failure recoveryFailure
	if errors.As(err, &failure) {
		var cleanup secretdrafts.CleanupAckError
		if failure.stage == "cleanup_ack" && errors.As(failure.cause, &cleanup) {
			return "cleanup_ack_" + cleanup.Stage
		}
		return failure.stage
	}
	return "unknown"
}

// RecoveryFailureClass applies ordered errors.Is checks for conflict, invalid, not_found and unavailable,
// using unknown otherwise; joined errors keep that precedence.
// RecoveryFailureClass применяет упорядоченные errors.Is для conflict, invalid, not_found и unavailable,
// иначе unknown; для joined errors сохраняется этот приоритет.
func RecoveryFailureClass(err error) string {
	switch {
	case errors.Is(err, secretdrafts.ErrConflict):
		return "conflict"
	case errors.Is(err, secretdrafts.ErrInvalid):
		return "invalid"
	case errors.Is(err, secretdrafts.ErrNotFound):
		return "not_found"
	case errors.Is(err, secretdrafts.ErrUnavailable):
		return "unavailable"
	default:
		return "unknown"
	}
}

// ReconcileOnce bounds a returned batch to 1000, processes items serially and joins failures without
// decrypting or republishing. Every return updates readiness and the observer; cancellation stops
// remaining items, not earlier effects.
// ReconcileOnce ограничивает возвращённый batch до 1000, последовательно обрабатывает элементы и
// объединяет ошибки без расшифровки и повторной публикации. Каждый return обновляет readiness и observer;
// отмена останавливает остаток, не прежние эффекты.
func (service *Service) ReconcileOnce(ctx context.Context) (result error) {
	defer func() {
		service.recoveryMu.Lock()
		service.recoveryRan, service.recoveryReady = true, result == nil
		service.recoveryMu.Unlock()
		if service.observer != nil {
			service.observer.RecoveryCompleted(result == nil)
		}
	}()
	work, err := service.owner.ListRecovery(ctx)
	if err != nil {
		return recoveryFailure{stage: "list", cause: err}
	}
	if len(work) > 1000 {
		return secretdrafts.ErrConflict
	}
	for _, item := range work {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		if err := service.recover(ctx, item); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// recover matches observed/persisted effect descriptors, obtains an owner decision and validates both
// actions before deletion. It deletes ciphertext then runtime and acknowledges only DELETE descriptors;
// missing objects retain receipt pins, and partial effects are not rolled back.
// recover сверяет observed/persisted effect descriptors, получает owner decision и проверяет оба action до
// удаления. Удаляет ciphertext, затем runtime и подтверждает лишь DELETE descriptors; отсутствующие
// объекты сохраняют receipt pins, частичные эффекты не откатываются.
func (service *Service) recover(ctx context.Context, work value.DraftWork) error {
	if err := service.validateWork(work, false); err != nil {
		return recoveryFailure{stage: "validate", cause: err}
	}
	var encrypted *value.DraftEncryptedDescriptor
	expectedEncrypted := work.Encrypted
	if work.RecoveryEncrypted != nil {
		expectedEncrypted = work.RecoveryEncrypted
	}
	actual, err := service.staged.Lookup(ctx, work)
	switch {
	case err == nil:
		if expectedEncrypted != nil && *expectedEncrypted != actual {
			return recoveryFailure{stage: "encrypted_match", cause: secretdrafts.ErrConflict}
		}
		encrypted = &actual
	case errors.Is(err, secretdrafts.ErrNotFound):
		// Сохраняем exact descriptor для durable ACK после прежнего delete.
		encrypted = expectedEncrypted
	default:
		return recoveryFailure{stage: "encrypted_lookup", cause: err}
	}
	var materialization *value.DraftMaterialization
	if work.ClaimGeneration == 0 && work.RecoveryMaterialization != nil {
		return recoveryFailure{stage: "materialization_claim", cause: secretdrafts.ErrConflict}
	}
	if work.Kind == value.DraftPublish && work.ClaimGeneration > 0 {
		actual, err := service.runtime.Lookup(ctx, work)
		if err == nil {
			if work.RecoveryMaterialization != nil && *work.RecoveryMaterialization != actual {
				return recoveryFailure{stage: "materialization_match", cause: secretdrafts.ErrConflict}
			}
			materialization = &actual
		} else if !errors.Is(err, secretdrafts.ErrNotFound) {
			return recoveryFailure{stage: "materialization_lookup", cause: err}
		} else {
			materialization = work.RecoveryMaterialization
		}
	}
	decision, err := service.owner.Recover(ctx, work, encrypted, materialization)
	if err != nil {
		return recoveryFailure{stage: "owner_recover", cause: err}
	}
	for _, action := range []value.DraftRecoveryAction{decision.EncryptedAction, decision.MaterializationAction} {
		if action != value.DraftRecoveryKeep && action != value.DraftRecoveryDelete {
			return recoveryFailure{stage: "decision", cause: secretdrafts.ErrConflict}
		}
	}
	if decision.EncryptedAction == value.DraftRecoveryDelete && encrypted != nil {
		if err := service.staged.Delete(ctx, work, *encrypted); err != nil {
			return recoveryFailure{stage: "encrypted_delete", cause: err}
		}
		if service.observer != nil {
			service.observer.EncryptedDeleted()
		}
	}
	if decision.MaterializationAction == value.DraftRecoveryDelete && materialization != nil {
		if err := service.runtime.Delete(ctx, work, *materialization); err != nil {
			return recoveryFailure{stage: "materialization_delete", cause: err}
		}
		if service.observer != nil {
			service.observer.RuntimeDeleted()
		}
	}
	if decision.EncryptedAction == value.DraftRecoveryDelete || decision.MaterializationAction == value.DraftRecoveryDelete {
		// Подтверждаем только дескрипторы из устойчивого намерения DELETE.
		// Опубликованный Secret с решением KEEP не входит в очистку.
		var cleanupEncrypted *value.DraftEncryptedDescriptor
		var cleanupMaterialization *value.DraftMaterialization
		if decision.EncryptedAction == value.DraftRecoveryDelete {
			cleanupEncrypted = encrypted
		}
		if decision.MaterializationAction == value.DraftRecoveryDelete {
			cleanupMaterialization = materialization
		}
		if err := service.owner.CompleteCleanup(ctx, work, cleanupEncrypted, cleanupMaterialization); err != nil {
			return recoveryFailure{stage: "cleanup_ack", cause: err}
		}
	}
	return nil
}

// Worker returns a caller-owned cancel/join loop that waits for ticks, bounds each reconciliation and
// reports errors without exiting. Callers must supply a positive interval and meaningful timeout and run
// it only after the startup barrier; this method does not enforce that barrier.
// Worker возвращает caller-owned cancel/join loop, ожидающий ticks, ограничивающий каждый reconciliation и
// сообщающий ошибки без завершения. Caller задаёт положительный interval и осмысленный timeout и запускает
// после startup barrier; сам метод этот barrier не обеспечивает.
func (service *Service) Worker(interval, timeout time.Duration, observe func(error)) func(context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				bounded, cancel := context.WithTimeout(ctx, timeout)
				err := service.ReconcileOnce(bounded)
				cancel()
				if observe != nil {
					observe(err)
				}
			}
		}
	}
}
