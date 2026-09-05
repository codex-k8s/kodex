package secretdraft

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

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
		return err
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
		return err
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
			return secretdrafts.ErrConflict
		}
		encrypted = &actual
	case errors.Is(err, secretdrafts.ErrNotFound):
		// Сохраняем exact descriptor для durable ACK после прежнего delete.
		encrypted = expectedEncrypted
	default:
		return err
	}
	var materialization *value.DraftMaterialization
	if work.ClaimGeneration == 0 && work.RecoveryMaterialization != nil {
		return secretdrafts.ErrConflict
	}
	if work.Kind == value.DraftPublish && work.ClaimGeneration > 0 {
		actual, err := service.runtime.Lookup(ctx, work)
		if err == nil {
			if work.RecoveryMaterialization != nil && *work.RecoveryMaterialization != actual {
				return secretdrafts.ErrConflict
			}
			materialization = &actual
		} else if !errors.Is(err, secretdrafts.ErrNotFound) {
			return err
		} else {
			materialization = work.RecoveryMaterialization
		}
	}
	decision, err := service.owner.Recover(ctx, work, encrypted, materialization)
	if err != nil {
		return err
	}
	for _, action := range []value.DraftRecoveryAction{decision.EncryptedAction, decision.MaterializationAction} {
		if action != value.DraftRecoveryKeep && action != value.DraftRecoveryDelete {
			return secretdrafts.ErrConflict
		}
	}
	if decision.EncryptedAction == value.DraftRecoveryDelete && encrypted != nil {
		if err := service.staged.Delete(ctx, work, *encrypted); err != nil {
			return err
		}
		if service.observer != nil {
			service.observer.EncryptedDeleted()
		}
	}
	if decision.MaterializationAction == value.DraftRecoveryDelete && materialization != nil {
		if err := service.runtime.Delete(ctx, work, *materialization); err != nil {
			return err
		}
		if service.observer != nil {
			service.observer.RuntimeDeleted()
		}
	}
	if decision.EncryptedAction == value.DraftRecoveryDelete || decision.MaterializationAction == value.DraftRecoveryDelete {
		return service.owner.CompleteCleanup(ctx, work, encrypted, materialization)
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
