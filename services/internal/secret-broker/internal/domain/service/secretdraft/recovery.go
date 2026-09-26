package secretdraft

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

type recoveryFailure struct {
	stage string
	cause error
}

func (failure recoveryFailure) Error() string { return "secret draft recovery failed" }
func (failure recoveryFailure) Unwrap() error { return failure.cause }

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

// ReconcileOnce не расшифровывает и не повторяет публикацию. Раздельный owner
// readback решает судьбу каждого фактического внешнего эффекта.
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

// Worker принадлежит общему cancel/join после startup barrier. Цикл не держит
// plaintext и не завершается из-за временной недоступности владельца.
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
