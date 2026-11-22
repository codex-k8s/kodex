package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesFileCleanupRepairOperation isolates closure receipts from preparation, source reads and native execution.
// kubernetesFileCleanupRepairOperation отделяет closure receipts от preparation, source reads и native execution.
const kubernetesFileCleanupRepairOperation = "platform.integrations.kubernetes.file.cleanup.repair"

var (
	// queryKubernetesFileCleanupOrigin joins only original-root ownership and safe project scope.
	// queryKubernetesFileCleanupOrigin связывает лишь original-root ownership и safe project scope.
	//go:embed sql/kubernetes_file__cleanup_origin.sql
	queryKubernetesFileCleanupOrigin string
	// queryKubernetesFileCleanupSource binds the requested opaque source to its original invocation and actor.
	// queryKubernetesFileCleanupSource связывает opaque source с original invocation и actor.
	//go:embed sql/kubernetes_file__cleanup_source.sql
	queryKubernetesFileCleanupSource string
	// queryKubernetesFileCleanupStatus counts the one physical reservation independently of metadata frames.
	// queryKubernetesFileCleanupStatus считает одну physical reservation отдельно от metadata frames.
	//go:embed sql/kubernetes_file__cleanup_status.sql
	queryKubernetesFileCleanupStatus string
	// queryKubernetesFileCleanupLock serializes a single source after root and optional stream locks.
	// queryKubernetesFileCleanupLock сериализует source после root и optional stream locks.
	//go:embed sql/kubernetes_file__cleanup_lock.sql
	queryKubernetesFileCleanupLock string
	// queryKubernetesFileCleanupRepair delegates only the bounded counter transition to the owner function.
	// queryKubernetesFileCleanupRepair передаёт owner function лишь bounded counter transition.
	//go:embed sql/kubernetes_file__cleanup_repair.sql
	queryKubernetesFileCleanupRepair string
)

// kubernetesFileCleanupScope keeps immutable root lineage and the earliest required permission bound private.
// Neither source content nor integration credentials are part of closure authority.
// kubernetesFileCleanupScope хранит immutable root lineage и earliest permission bound privately.
// Source content и integration credentials не входят в closure authority.
type kubernetesFileCleanupScope struct {
	invocationID, rootID, rootRef, rootProjectRef, projectID, projectRef string
	until                                                                time.Time
}

// fileCleanupAccess requires the original root and current run.view, plus upload Until only for mutation.
// Every later call retains the first finite bound and refuses changed owner/project lineage.
// fileCleanupAccess требует original root и current run.view, upload Until лишь для mutation.
// Повторный вызов сохраняет first finite bound и отклоняет смену owner/project lineage.
func (repository *Repository) fileCleanupAccess(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, inv string, previous *kubernetesFileCleanupScope, write bool) (kubernetesFileCleanupScope, error) {
	var v kubernetesFileCleanupScope
	err := tx.QueryRow(ctx, queryKubernetesFileCleanupOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": inv, "project_ref": p.ProjectRef}).Scan(&v.invocationID, &v.rootID, &v.rootRef, &v.rootProjectRef, &v.projectID, &v.projectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, errs.ErrNotFound
	}
	if err != nil {
		return v, errs.ErrUnavailable
	}
	if previous != nil {
		if v.invocationID != previous.invocationID || v.rootID != previous.rootID || v.rootRef != previous.rootRef || v.rootProjectRef != previous.rootProjectRef || v.projectID != previous.projectID || v.projectRef != previous.projectRef {
			return v, errs.ErrNotFound
		}
		v.until = previous.until
	}
	until, err := repository.requireAccessUntil(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: v.rootRef, ProjectRef: v.rootProjectRef})
	if err != nil {
		return v, concealAccessDenial(err, errs.ErrNotFound)
	}
	v.until = earlierAccessDeadline(v.until, until)
	if write {
		upload, err := repository.requireArtifactUploadAccessUntil(ctx, tx, current, v.projectRef)
		if err != nil {
			return v, err
		}
		v.until = earlierAccessDeadline(v.until, upload)
	}
	if ctx.Err() != nil {
		return v, errs.ErrUnavailable
	}
	if accessDeadlineExpired(v.until) {
		return v, errs.ErrNotFound
	}
	return v, nil
}

// readKubernetesFileCleanupStatus exposes bounded physical counters without source provenance or bytes.
// Repair-only version is independent of material source version and worker progress.
// readKubernetesFileCleanupStatus раскрывает bounded physical counters без source provenance либо bytes.
// Repair-only version независима от material source version и worker progress.
func readKubernetesFileCleanupStatus(ctx context.Context, tx pgx.Tx, org, id string) (entity.KubernetesStagingCleanupStatus, error) {
	var v entity.KubernetesStagingCleanupStatus
	err := tx.QueryRow(ctx, queryKubernetesFileCleanupStatus, pgx.StrictNamedArgs{"organization_id": org, "source_id": id}).Scan(&v.Version, &v.RepairGeneration, &v.OutstandingCount, &v.OutstandingBytes, &v.ExhaustedCount, &v.EligibleExhaustedCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, errs.ErrNotFound
	}
	if err != nil || v.Version < 1 || v.Version > 9007199254740991 || v.RepairGeneration < 0 || v.RepairGeneration > 9007199254740991 || v.OutstandingCount < 0 || v.OutstandingCount > 1 || v.OutstandingBytes < 0 || v.OutstandingBytes > 16<<20 || v.ExhaustedCount < 0 || v.ExhaustedCount > v.OutstandingCount || v.EligibleExhaustedCount < 0 || v.EligibleExhaustedCount > v.ExhaustedCount {
		return v, errs.ErrUnavailable
	}
	return v, nil
}

// fileCleanupView verifies the exact source boundary before optional stream/source locking and minimal projection.
// The locked path follows root -> stream -> source; list reads never accumulate multiple stream locks.
// fileCleanupView проверяет exact source boundary до optional stream/source locks и minimal projection.
// Locked path следует root -> stream -> source; List не накапливает multiple stream locks.
func (repository *Repository) fileCleanupView(ctx context.Context, tx pgx.Tx, current scope, owner kubernetesFileCleanupScope, inv, ref string, lock bool) (entity.KubernetesFileCleanupView, string, error) {
	v := entity.KubernetesFileCleanupView{SourceRef: ref, InvocationRef: inv}
	var id, stream string
	err := tx.QueryRow(ctx, queryKubernetesFileCleanupSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.invocationID, "actor_id": current.actorID, "project_id": owner.projectID, "source_ref": ref}).Scan(&id, &stream)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, "", errs.ErrNotFound
	}
	if err != nil {
		return v, "", errs.ErrUnavailable
	}
	if lock {
		if stream != "" {
			var locked string
			if tx.QueryRow(ctx, queryKubernetesCaptureStreamLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": stream}).Scan(&locked) != nil || locked != stream {
				return v, "", errs.ErrUnavailable
			}
		}
		var locked string
		if tx.QueryRow(ctx, queryKubernetesFileCleanupLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "source_id": id}).Scan(&locked) != nil || locked != id {
			return v, "", errs.ErrUnavailable
		}
		var freshID, freshStream string
		if tx.QueryRow(ctx, queryKubernetesFileCleanupSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.invocationID, "actor_id": current.actorID, "project_id": owner.projectID, "source_ref": ref}).Scan(&freshID, &freshStream) != nil || freshID != id || freshStream != stream {
			return v, "", errs.ErrUnavailable
		}
	}
	v.Cleanup, err = readKubernetesFileCleanupStatus(ctx, tx, current.organizationID, id)
	return v, id, err
}

// readKubernetesFileCleanupReceipt recovers only the exact original actor/source/key acknowledgement.
// Missing receipt is not permission to replay a repair or create another physical source.
// readKubernetesFileCleanupReceipt восстанавливает лишь exact original actor/source/key acknowledgement.
// Missing receipt не разрешает replay repair либо создание нового physical source.
func readKubernetesFileCleanupReceipt(ctx context.Context, tx pgx.Tx, current scope, source, key string) (*entity.KubernetesFileCleanupRepairReceipt, error) {
	if key == "" {
		return nil, nil
	}
	var digest string
	var raw []byte
	err := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesFileCleanupRepairOperation, key).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var r entity.KubernetesFileCleanupRepairReceipt
	if len(raw) > 4096 || !managedkubernetes.ValidDigest(digest) || internalrpcauth.DecodeStrictJSON(raw, &r) != nil || r.Version < 2 || r.RepairGeneration < 1 || r.RepairedCount != 1 || r.RemainingExhaustedCount < 0 || r.RemainingExhaustedCount > 1 || r.AcceptedAt.IsZero() {
		return nil, errs.ErrUnavailable
	}
	if r.SourceRef != source {
		return nil, errs.ErrNotFound
	}
	return &r, nil
}

// ListKubernetesFileCleanup discovers original-invocation closure metadata after source artifact access was lost.
// Every failure discards the entire list; no content, artifact, path, hash or storage coordinate is read.
// ListKubernetesFileCleanup раскрывает original-invocation closure metadata после потери source artifact access.
// Любая ошибка отбрасывает List; content/artifact/path/hash/storage coordinates не читаются.
func (repository *Repository) ListKubernetesFileCleanup(ctx context.Context, p value.Principal, inv string) (result []entity.KubernetesFileCleanupView, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.file.list"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.fileCleanupAccess(ctx, tx, current, p, inv, nil, false)
	if err != nil {
		return nil, err
	}
	if _, err := lockOwnerGateRootTx(ctx, tx, owner.rootID); err != nil {
		return nil, err
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, inv, &owner, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryKubernetesFileList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.invocationID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := make([]string, 0)
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil || len(refs) > 256 {
		return nil, errs.ErrUnavailable
	}
	items := make([]entity.KubernetesFileCleanupView, 0, len(refs))
	for _, ref := range refs {
		v, _, err := repository.fileCleanupView(ctx, tx, current, owner, inv, ref, false)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, inv, &owner, false)
	if err != nil {
		return nil, err
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if accessDeadlineExpired(owner.until) {
		return nil, errs.ErrNotFound
	}
	return items, nil
}

// GetKubernetesFileCleanup reads exact current minimal counters and an optional original-key receipt.
// Source visibility and execution grants are deliberately not borrowed for this closure-only read.
// GetKubernetesFileCleanup читает exact current minimal counters и optional original-key receipt.
// Source visibility и execution grants не заимствуются для closure-only read.
func (repository *Repository) GetKubernetesFileCleanup(ctx context.Context, p value.Principal, input query.KubernetesFileCleanupRead) (result entity.KubernetesFileCleanupReadback, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.file.get"); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, nil, false)
	if err != nil {
		return result, err
	}
	if _, err := lockOwnerGateRootTx(ctx, tx, owner.rootID); err != nil {
		return result, err
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, &owner, false)
	if err != nil {
		return result, err
	}
	v, _, err := repository.fileCleanupView(ctx, tx, current, owner, input.InvocationRef, input.SourceRef, true)
	if err != nil {
		return result, err
	}
	receipt, err := readKubernetesFileCleanupReceipt(ctx, tx, current, input.SourceRef, input.CleanupIdempotencyKey)
	if err != nil {
		return result, err
	}
	if receipt != nil && (receipt.Version > v.Cleanup.Version || receipt.RepairGeneration > v.Cleanup.RepairGeneration) {
		return result, errs.ErrUnavailable
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, &owner, false)
	if err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if accessDeadlineExpired(owner.until) {
		return result, errs.ErrNotFound
	}
	return entity.KubernetesFileCleanupReadback{View: v, Receipt: receipt}, nil
}

// RepairKubernetesFileCleanup opens one terminal cleanup cycle under original-root/upload authority before idempotency or OCC.
// Its receipt creates no bytes, prepared intent, native work, permission renewal or deletion-success assertion.
// RepairKubernetesFileCleanup открывает terminal cleanup cycle под original-root/upload authority до idempotency либо OCC.
// Receipt не создаёт bytes/prepared intent/native work, не продлевает permission и не утверждает deletion success.
func (repository *Repository) RepairKubernetesFileCleanup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesFileCleanupRepair) (result entity.KubernetesFileCleanupReadback, resultErr error) {
	if err := kubernetesInputAPICaller(p, kubernetesFileCleanupRepairOperation); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || mutation.IdempotencyKey == "" {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, nil, true)
	if err != nil {
		return result, err
	}
	if _, err := lockOwnerGateRootTx(ctx, tx, owner.rootID); err != nil {
		return result, err
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, &owner, true)
	if err != nil {
		return result, err
	}
	view, id, err := repository.fileCleanupView(ctx, tx, current, owner, input.InvocationRef, input.SourceRef, true)
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID, kubernetesFileCleanupRepairOperation, mutation.IdempotencyKey); err != nil {
		return result, errs.ErrUnavailable
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, &owner, true)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(struct {
		InvocationRef, SourceRef string
		Version                  int64
	}{input.InvocationRef, input.SourceRef, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	intent := hex.EncodeToString(sum[:])
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesFileCleanupRepairOperation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	var receipt *entity.KubernetesFileCleanupRepairReceipt
	if err == nil {
		if storedDigest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		receipt, err = readKubernetesFileCleanupReceipt(ctx, tx, current, input.SourceRef, mutation.IdempotencyKey)
		if err != nil {
			return result, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	} else {
		if view.Cleanup.Version != *mutation.ExpectedVersion {
			return result, errs.ErrVersionMismatch
		}
		if view.Cleanup.EligibleExhaustedCount != 1 {
			return result, errs.ErrConflict
		}
		receipt = &entity.KubernetesFileCleanupRepairReceipt{SourceRef: input.SourceRef}
		err = tx.QueryRow(ctx, queryKubernetesFileCleanupRepair, pgx.StrictNamedArgs{"organization_id": current.organizationID, "source_id": id, "expected_version": *mutation.ExpectedVersion}).Scan(&receipt.Version, &receipt.RepairGeneration, &receipt.RepairedCount, &receipt.AcceptedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrConflict
		}
		if err != nil {
			return result, errs.ErrUnavailable
		}
		view, _, err = repository.fileCleanupView(ctx, tx, current, owner, input.InvocationRef, input.SourceRef, false)
		if err != nil {
			return result, err
		}
		receipt.RemainingExhaustedCount = view.Cleanup.ExhaustedCount
		stored, err = json.Marshal(receipt)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, kubernetesFileCleanupRepairOperation, mutation.IdempotencyKey, intent, "KUBERNETES_FILE_SOURCE", stored); err != nil {
			return result, errs.ErrUnavailable
		}
		audit, err := newRef("aud")
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, audit, current.organizationID, owner.projectID, current.actorID, kubernetesFileCleanupRepairOperation, "KUBERNETES_FILE_SOURCE", input.SourceRef, "i18n:KUBERNETES_STAGING_CLEANUP_REPAIR_QUEUED", current.correlationRef); err != nil {
			return result, errs.ErrUnavailable
		}
	}
	if receipt == nil || receipt.Version > view.Cleanup.Version || receipt.RepairGeneration > view.Cleanup.RepairGeneration {
		return result, errs.ErrUnavailable
	}
	owner, err = repository.fileCleanupAccess(ctx, tx, current, p, input.InvocationRef, &owner, true)
	if err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if accessDeadlineExpired(owner.until) {
		return result, errs.ErrNotFound
	}
	return entity.KubernetesFileCleanupReadback{View: view, Receipt: receipt}, nil
}
