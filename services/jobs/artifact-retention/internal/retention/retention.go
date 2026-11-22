// Package retention executes fenced, exact-version irreversible artifact deletion.
// RU: Package retention исполняет защищённое поколением необратимое удаление точных версий артефактов.
package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// ErrLostClaim means the repository no longer owns the fenced artifact claim.
// RU: ErrLostClaim означает, что repository больше не владеет защищённым claim артефакта.
var ErrLostClaim = errors.New("artifact retention claim is lost")

// Claim identifies one artifact revision, exact object version and ownership generation.
// RU: Claim обозначает одну версию артефакта, точную версию объекта и поколение владения.
type Claim struct {
	ArtifactID, ArtifactRef, RevisionID, ObjectKey, ObjectVersion string
	Generation                                                    int64
	Manual, ReconcileOnly                                         bool
	AuthorizedUntil                                               time.Time
}

// Repository owns bounded lease acquisition and transactional artifact tombstone finalization.
// RU: Repository владеет получением ограниченных аренд и транзакционным завершением tombstone артефакта.
type Repository interface {
	// Claim leases due revisions to an owner for a bounded interval.
	// RU: Claim арендует подлежащие очистке версии владельцу на ограниченный срок.
	Claim(context.Context, string, int, int64) ([]Claim, error)
	// Finalize records one exact fenced claim after object deletion is confirmed.
	// RU: Finalize фиксирует точный защищённый claim после подтверждения удаления объекта.
	Finalize(context.Context, Claim, string) error
}

// ObjectStore permits only exact-version deletion and readback for a claimed artifact.
// RU: ObjectStore разрешает только удаление и проверку точной версии заявленного артефакта.
type ObjectStore interface {
	// Delete removes a named object version, never an unqualified key.
	// RU: Delete удаляет именованную версию объекта, а не ключ без версии.
	Delete(context.Context, string, string) error
	// Head checks whether that exact object version remains visible.
	// RU: Head проверяет, видна ли ещё именно эта версия объекта.
	Head(context.Context, string, string) (objectstorage.Receipt, error)
}

// Processor coordinates external deletion with repository-owned fenced finalization.
// RU: Processor координирует внешнее удаление с защищённой финализацией в repository.
type Processor struct {
	repository Repository
	objects    ObjectStore
}

// NewProcessor binds the owner repository and exact-version object store without starting work.
// RU: NewProcessor связывает repository-владельца и хранилище точных версий, не запуская работу.
func NewProcessor(repository Repository, objects ObjectStore) *Processor {
	return &Processor{repository: repository, objects: objects}
}

// Process deletes each claimed exact version, requires an absent readback and only then
// finalizes its tombstone; partial successes are counted even when another claim fails.
// RU: Process удаляет точную версию каждого claim, требует подтверждённого отсутствия и лишь
// затем завершает tombstone; частичные успехи считаются и при отказе другого claim.
func (processor *Processor) Process(
	ctx context.Context,
	owner string,
	batchSize int,
	leaseSeconds int64,
) (int, error) {
	if deleter, ok := processor.objects.(objectstorage.SingleAttemptDeleter); !ok || deleter.CheckDeleteOnce(ctx) != nil {
		return 0, errors.New("artifact retention single-attempt deletion is unavailable")
	}
	claims, err := processor.repository.Claim(ctx, owner, batchSize, leaseSeconds)
	if err != nil {
		return 0, err
	}
	processed := 0
	var resultErr error
	for _, claim := range claims {
		if claim.ArtifactID == "" || claim.RevisionID == "" || claim.ObjectKey == "" || claim.ObjectVersion == "" || claim.Generation < 1 {
			resultErr = errors.Join(resultErr, errors.New("artifact retention claim is incomplete"))
			continue
		}
		if claim.AuthorizedUntil.IsZero() || !time.Now().Before(claim.AuthorizedUntil) {
			resultErr = errors.Join(resultErr, ErrLostClaim)
			continue
		}
		operation, cancel := context.WithDeadline(ctx, claim.AuthorizedUntil)
		err := processor.processClaim(operation, claim, owner)
		cancel()
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		processed++
	}
	return processed, resultErr
}

// processClaim preserves scheduled retries while manual custody admits one DeleteOnce or observation.
// It never substitutes an ordinary Delete after uncertain manual IO or a lost admission ACK.
// processClaim сохраняет scheduled retries, а manual custody допускает один DeleteOnce либо
// наблюдение. Обычный Delete не заменяет uncertain manual IO или lost admission ACK.
func (processor *Processor) processClaim(ctx context.Context, claim Claim, owner string) error {
	acknowledged := false
	if !claim.ReconcileOnly {
		var err error
		if claim.Manual {
			err = processor.objects.(objectstorage.SingleAttemptDeleter).DeleteOnce(ctx, claim.ObjectKey, claim.ObjectVersion)
		} else {
			err = processor.objects.Delete(ctx, claim.ObjectKey, claim.ObjectVersion)
		}
		if err != nil && (claim.Manual || !errors.Is(err, objectstorage.ErrNotFound)) {
			return fmt.Errorf("delete claimed artifact object version: %w", err)
		}
		acknowledged = err == nil
	}
	if _, err := processor.objects.Head(ctx, claim.ObjectKey, claim.ObjectVersion); err == nil {
		return errors.New("deleted artifact object version is still present")
	} else if !errors.Is(err, objectstorage.ErrNotFound) {
		return fmt.Errorf("read back deleted artifact object version: %w", err)
	}
	if claim.Manual && !acknowledged {
		if err := objectstorage.VerifyVersionAbsent(ctx, processor.objects, claim.ObjectKey, claim.ObjectVersion); err != nil {
			return err
		}
	}
	if ctx.Err() != nil || !time.Now().Before(claim.AuthorizedUntil) {
		return ErrLostClaim
	}
	return processor.repository.Finalize(ctx, claim, owner)
}
