package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Поля привязки назначает вызывающая canonical owner boundary. Контекст и
// locator здесь только закрепляются и никогда не становятся полномочиями.
type preparedContentBinding struct {
	ActorID, OrganizationID, OrganizationRef, ProjectID, ProjectRef      string
	SourceProfile, SourceProfileRef, SourceContextDigest, SourceLeaseRef string
	SourceFenceDigest, SourceRunRef, OperationKey                        string
	IntentOperation, IdempotencyKey, IntentDigest                        string
	TargetArtifactID, TargetArtifactRef, SourceRevisionRef               string
	SourceProfileVersion, SourceLeaseGeneration, ExpectedArtifactVersion int64
}

type preparedContentRequest struct {
	Binding                                              preparedContentBinding
	FileName, MediaType, Digest, ScanState, PreviewState string
	SizeBytes                                            int64
	Body                                                 io.Reader
}

type preparedContent struct {
	LedgerID, ContentRef, ArtifactRef, RevisionRef, FileName string
	Generation                                               int64
	Prepared                                                 command.PreparedArtifact
}

var (
	//go:embed sql/prepared_content_insert.sql
	queryPreparedContentInsert string
	//go:embed sql/prepared_content_replay.sql
	queryPreparedContentReplay string
	//go:embed sql/prepared_content_writer_lock.sql
	queryPreparedContentWriterLock string
	//go:embed sql/prepared_content_stage.sql
	queryPreparedContentStage string
	//go:embed sql/prepared_content_resolve.sql
	queryPreparedContentResolve string
	//go:embed sql/prepared_content_link.sql
	queryPreparedContentLink string
	//go:embed sql/prepared_content_adopt.sql
	queryPreparedContentAdopt string
	//go:embed sql/prepared_content_abandon.sql
	queryPreparedContentAbandon string
	//go:embed sql/prepared_content_binding.sql
	queryPreparedContentBinding string
	//go:embed sql/prepared_content_bind_revision.sql
	queryPreparedContentBindRevision string
)

func preparedContentRequestDigest(request preparedContentRequest) string {
	// io.Reader исключён из commitment; digest/size связывают только inspected body.
	raw, _ := json.Marshal(struct {
		Binding                                              preparedContentBinding
		FileName, MediaType, Digest, ScanState, PreviewState string
		SizeBytes                                            int64
	}{request.Binding, request.FileName, request.MediaType, request.Digest, request.ScanState, request.PreviewState, request.SizeBytes})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validPreparedContentRequest(request preparedContentRequest) bool {
	b := request.Binding
	for _, id := range []string{b.ActorID, b.OrganizationID, b.ProjectID} {
		if uuid.Validate(id) != nil {
			return false
		}
	}
	if b.TargetArtifactID != "" && uuid.Validate(b.TargetArtifactID) != nil {
		return false
	}
	for _, digest := range []string{b.IntentDigest, b.SourceContextDigest, b.SourceFenceDigest} {
		if !objectstorage.ValidDigest("sha256:"+digest) || digest != strings.ToLower(digest) {
			return false
		}
	}
	for _, ref := range []string{b.OrganizationRef, b.ProjectRef, b.SourceProfileRef, b.SourceLeaseRef, b.SourceRunRef, b.OperationKey} {
		if len(ref) < 1 || len(ref) > 96 || strings.ContainsAny(ref, "/\\\x00") {
			return false
		}
	}
	if b.SourceProfile != "SYSTEM" && b.SourceProfile != "PROJECT" {
		return false
	}
	if b.SourceProfileVersion < 1 || b.SourceLeaseGeneration < 1 || b.ExpectedArtifactVersion < 0 {
		return false
	}
	if (b.TargetArtifactID == "") != (b.TargetArtifactRef == "") || (b.TargetArtifactID != "" &&
		(b.SourceRevisionRef == "" || b.ExpectedArtifactVersion < 1)) {
		return false
	}
	if len(b.IntentOperation) < 1 || len(b.IntentOperation) > 128 || len(b.IdempotencyKey) < 1 || len(b.IdempotencyKey) > 200 {
		return false
	}
	return request.Body != nil && request.SizeBytes >= 0 && request.SizeBytes <= 1<<20 &&
		objectstorage.ValidDigest(request.Digest) && request.Digest == strings.ToLower(request.Digest) &&
		utf8.ValidString(request.FileName) && utf8.RuneCountInString(request.FileName) >= 1 && utf8.RuneCountInString(request.FileName) <= 255 &&
		len(request.MediaType) >= 1 && len(request.MediaType) <= 160 && request.ScanState == "CLEAN" &&
		(request.PreviewState == "AVAILABLE" || request.PreviewState == "UNAVAILABLE" || request.PreviewState == "BLOCKED")
}

func scanPreparedContent(row pgx.Row) (preparedContent, string, string, error) {
	var content preparedContent
	var state, requestDigest string
	err := row.Scan(&content.LedgerID, &content.ContentRef, &content.Generation, &content.ArtifactRef, &content.RevisionRef,
		&content.FileName, &content.Prepared.MediaType, &content.Prepared.Digest, &content.Prepared.SizeBytes,
		&content.Prepared.ScanState, &content.Prepared.PreviewState, &content.Prepared.ObjectKey,
		&content.Prepared.ObjectVersion, &content.Prepared.ObjectETag, &state, &requestDigest)
	content.Prepared.Ref = content.ArtifactRef
	if err != nil {
		return preparedContent{}, "", "", err
	}
	return content, state, requestDigest, nil
}

// Durable unique intent фиксируется до одного Put. Повтор RPC читает ledger;
// никакой исход, включая rollback/timeout/HEAD404, не разрешает второй Put.
func (repository *Repository) stagePreparedContent(ctx context.Context, request preparedContentRequest) (preparedContent, error) {
	if !validPreparedContentRequest(request) {
		return preparedContent{}, errs.ErrInvalid
	}
	b := request.Binding
	requestDigest := preparedContentRequestDigest(request)
	artifactRef := b.TargetArtifactRef
	if artifactRef == "" {
		artifactRef = "art_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	revisionRef := "arv_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	contentRef := "pfcnt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	key := "organizations/" + b.OrganizationRef + "/projects/" + b.ProjectRef + "/artifacts/" + artifactRef + "/" + revisionRef + "/" + strings.TrimPrefix(request.Digest, "sha256:")
	var targetID any
	if b.TargetArtifactID != "" {
		targetID = b.TargetArtifactID
	}
	var id string
	err := repository.pool.QueryRow(ctx, queryPreparedContentInsert, pgx.StrictNamedArgs{
		"ref": contentRef, "organization_id": b.OrganizationID, "project_id": b.ProjectID, "origin_project_ref": b.ProjectRef, "actor_id": b.ActorID,
		"intent_operation": b.IntentOperation, "idempotency_key": b.IdempotencyKey, "intent_digest": b.IntentDigest, "request_digest": requestDigest,
		"operation_key": b.OperationKey, "source_profile": b.SourceProfile, "source_profile_ref": b.SourceProfileRef,
		"source_profile_version": b.SourceProfileVersion, "source_context_digest": b.SourceContextDigest,
		"source_lease_ref": b.SourceLeaseRef, "source_lease_generation": b.SourceLeaseGeneration, "source_fence_digest": b.SourceFenceDigest,
		"source_run_ref": b.SourceRunRef, "target_artifact_id": targetID, "target_artifact_ref": b.TargetArtifactRef,
		"source_revision_ref": b.SourceRevisionRef, "expected_artifact_version": b.ExpectedArtifactVersion,
		"prepared_artifact_ref": artifactRef, "prepared_revision_ref": revisionRef, "file_name": request.FileName, "media_type": request.MediaType,
		"digest": request.Digest, "size_bytes": request.SizeBytes, "scan_state": request.ScanState, "preview_state": request.PreviewState, "object_key": key,
	}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		content, state, storedDigest, readErr := scanPreparedContent(repository.pool.QueryRow(ctx, queryPreparedContentReplay, pgx.StrictNamedArgs{
			"organization_id": b.OrganizationID, "actor_id": b.ActorID, "intent_operation": b.IntentOperation,
			"idempotency_key": b.IdempotencyKey, "operation_key": b.OperationKey,
		}))
		if readErr != nil {
			return preparedContent{}, errs.ErrConflict
		}
		if storedDigest != requestDigest {
			return preparedContent{}, errs.ErrIdempotencyReuse
		}
		if state != "STAGED" {
			return preparedContent{}, errs.ErrConflict
		}
		return content, nil
	}
	if err != nil {
		return preparedContent{}, errs.ErrUnavailable
	}
	writer, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, err := repository.pool.BeginTx(writer, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return preparedContent{}, errs.ErrUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if err := tx.QueryRow(writer, queryPreparedContentWriterLock, pgx.StrictNamedArgs{"id": id}).Scan(&id); err != nil {
		return preparedContent{}, errs.ErrConflict
	}
	putContext, stopPut := context.WithTimeout(writer, 30*time.Second)
	receipt, putErr := repository.objects.Put(putContext, objectstorage.PutInput{Key: key, MediaType: request.MediaType,
		Digest: request.Digest, SizeBytes: request.SizeBytes, Body: io.LimitReader(request.Body, request.SizeBytes+1)})
	stopPut()
	state := "UNKNOWN"
	if putErr == nil && exactPreparedReceipt(receipt, key, request.Digest, request.SizeBytes) {
		head, headErr := repository.objects.Head(writer, key, receipt.VersionID)
		if headErr == nil && head == receipt {
			state = "STAGED"
		}
	}
	// Любое несовпадение хранит UNKNOWN без принятого receipt; cleanup сверит
	// точный серверный key/digest/size, не удаляя случайную чужую version.
	version, etag := "", ""
	if state == "STAGED" {
		version, etag = receipt.VersionID, receipt.ETag
	}
	if _, err := tx.Exec(writer, queryPreparedContentStage, pgx.StrictNamedArgs{
		"id": id, "state": state, "object_version": version, "object_etag": etag,
	}); err != nil {
		return preparedContent{}, errs.ErrUnavailable
	}
	if err := tx.Commit(writer); err != nil {
		return preparedContent{}, errs.ErrUnavailable
	}
	if state != "STAGED" {
		return preparedContent{}, errs.ErrUnavailable
	}
	return preparedContent{LedgerID: id, ContentRef: contentRef, ArtifactRef: artifactRef, RevisionRef: revisionRef,
		FileName: request.FileName, Generation: 1, Prepared: command.PreparedArtifact{Ref: artifactRef, ObjectKey: key,
			ObjectVersion: version, ObjectETag: etag, MediaType: request.MediaType, Digest: request.Digest, SizeBytes: request.SizeBytes,
			ScanState: request.ScanState, PreviewState: request.PreviewState}}, nil
}

func exactPreparedReceipt(receipt objectstorage.Receipt, key, digest string, size int64) bool {
	return receipt.Key == key && receipt.VersionID != "" && receipt.ETag != "" && receipt.Digest == digest && receipt.SizeBytes == size
}

// Вызывается только после fresh plan/target authority. Создатель ledger
// неизменен; текущий actor вправе Apply по authority плана, не совпадению IDs.
func (repository *Repository) resolvePreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, planID string, planRevision int64, operationKey, contentRef, digest string, sizeBytes int64) (preparedContent, error) {
	content, _, _, err := scanPreparedContent(tx.QueryRow(ctx, queryPreparedContentResolve, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "plan_id": planID, "plan_revision": planRevision,
		"operation_key": operationKey, "ref": contentRef, "digest": digest, "size_bytes": sizeBytes,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return preparedContent{}, errs.ErrConflict
	}
	if err != nil {
		return preparedContent{}, errs.ErrUnavailable
	}
	return content, nil
}

func preparedContentTransition(ctx context.Context, tx pgx.Tx, query string, args pgx.StrictNamedArgs) error {
	result, err := tx.Exec(ctx, query, args)
	if err != nil {
		return errs.ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) linkPreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, ledgerID, planID string, planRevision int64, operationKey string) error {
	err := preparedContentTransition(ctx, tx, queryPreparedContentLink, pgx.StrictNamedArgs{
		"id": ledgerID, "organization_id": current.organizationID, "plan_id": planID, "plan_revision": planRevision, "operation_key": operationKey,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryPreparedContentBindRevision, pgx.StrictNamedArgs{"id": ledgerID, "organization_id": current.organizationID}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) adoptPreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, ledgerID, revisionID string) error {
	return preparedContentTransition(ctx, tx, queryPreparedContentAdopt, pgx.StrictNamedArgs{
		"id": ledgerID, "organization_id": current.organizationID, "revision_id": revisionID,
	})
}

func (repository *Repository) abandonPreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, ledgerID string) error {
	return preparedContentTransition(ctx, tx, queryPreparedContentAbandon, pgx.StrictNamedArgs{"id": ledgerID, "organization_id": current.organizationID})
}

func (repository *Repository) readPreparedContentBindingTx(ctx context.Context, tx pgx.Tx, current scope, planID string, planRevision int64, operationKey, contentRef, digest string, sizeBytes int64) (preparedContentBinding, error) {
	// До подтверждённого cleanup origin доступен для owner draft после TTL.
	// Terminal scrub закрывает lookup; это не выдаёт receipt либо полномочия.
	var b preparedContentBinding
	err := tx.QueryRow(ctx, queryPreparedContentBinding, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "plan_id": planID, "plan_revision": planRevision,
		"operation_key": operationKey, "ref": contentRef, "digest": digest, "size_bytes": sizeBytes,
	}).Scan(
		&b.ActorID, &b.OrganizationID, &b.OrganizationRef, &b.ProjectID, &b.ProjectRef, &b.SourceProfile, &b.SourceProfileRef,
		&b.SourceContextDigest, &b.SourceLeaseRef, &b.SourceFenceDigest, &b.SourceRunRef, &b.OperationKey, &b.IntentOperation, &b.IdempotencyKey, &b.IntentDigest,
		&b.TargetArtifactID, &b.TargetArtifactRef, &b.SourceRevisionRef, &b.SourceProfileVersion, &b.SourceLeaseGeneration, &b.ExpectedArtifactVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return preparedContentBinding{}, errs.ErrConflict
	}
	if err != nil {
		return preparedContentBinding{}, errs.ErrUnavailable
	}
	return b, nil
}
