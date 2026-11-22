package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCapturePreparedReconcile locks only a finished or expired one-PUT intent.
// queryKubernetesCapturePreparedReconcile блокирует только finished либо expired one-PUT intent.
//
//go:embed sql/kubernetes_capture__prepared_reconcile.sql
var queryKubernetesCapturePreparedReconcile string

// queryKubernetesCapturePreparedRecord records exact positive object readback without a second PUT.
// queryKubernetesCapturePreparedRecord сохраняет exact positive object readback без второго PUT.
//
//go:embed sql/kubernetes_capture__prepared_record.sql
var queryKubernetesCapturePreparedRecord string

// reconcileKubernetesPreparedContent resolves only the one owner-assigned immutable object key.
// Missing/uncertain metadata remains unknown; no payload write or blind mutation retry occurs.
// Positive readback pins and verifies the exact provider version before recording STAGED.
// reconcileKubernetesPreparedContent разрешает только один owner-assigned immutable object key.
// Missing/uncertain metadata остаются unknown; payload write либо blind mutation retry отсутствуют.
// Positive readback закрепляет и проверяет exact provider version до записи STAGED.
func (repository *Repository) reconcileKubernetesPreparedContent(ctx context.Context, content preparedContent, requestDigest, sourceProfile string) (result preparedContent, resultErr error) {
	if sourceProfile != "KUBERNETES_OUTPUT" && sourceProfile != "KUBERNETES_FILE_SOURCE" {
		return result, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var key, digest string
	var size int64
	if err := tx.QueryRow(ctx, queryKubernetesCapturePreparedReconcile, pgx.StrictNamedArgs{"id": content.LedgerID, "request_digest": requestDigest, "source_profile": sourceProfile}).Scan(&key, &digest, &size); err != nil {
		return result, errs.ErrUnavailable
	}
	head, err := repository.objects.Head(ctx, key, "")
	if err != nil || !validKubernetesOutputObject(key, strings.TrimPrefix(digest, "sha256:"), uint32(size), head) {
		return result, errs.ErrUnavailable
	}
	observed, err := repository.objects.Get(ctx, key, head.VersionID)
	if err != nil {
		if observed.Body != nil {
			_ = observed.Body.Close()
		}
		return result, errs.ErrUnavailable
	}
	if observed.Body == nil {
		return result, errs.ErrUnavailable
	}
	if observed.Receipt != head {
		_ = observed.Body.Close()
		return result, errs.ErrUnavailable
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, io.LimitReader(observed.Body, size+1))
	closeErr := observed.Body.Close()
	if readErr != nil || closeErr != nil || count != size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest || ctx.Err() != nil {
		return result, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryKubernetesCapturePreparedRecord, pgx.StrictNamedArgs{"id": content.LedgerID, "request_digest": requestDigest, "object_version": head.VersionID, "object_etag": head.ETag, "source_profile": sourceProfile})
	if err != nil || tag.RowsAffected() != 1 {
		return result, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	content.Prepared.ObjectKey = key
	content.Prepared.ObjectVersion = head.VersionID
	content.Prepared.ObjectETag = head.ETag
	if !exactPreparedReceipt(objectstorage.Receipt{Key: content.Prepared.ObjectKey, VersionID: content.Prepared.ObjectVersion, ETag: content.Prepared.ObjectETag, Digest: content.Prepared.Digest, SizeBytes: content.Prepared.SizeBytes}, key, digest, size) {
		return result, errs.ErrUnavailable
	}
	return content, nil
}
