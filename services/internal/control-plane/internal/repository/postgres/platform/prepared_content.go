package platform

import (
	"bytes"
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

// preparedContentBinding pins owner-assigned actor, tenant, project, source lease and
// intent/target versions; locator fields alone never establish authority.
// preparedContentBinding закрепляет назначенные владельцем actor, tenant, project, source lease и
// версии intent/target; поля locator сами по себе не доказывают полномочия.
type preparedContentBinding struct {
	ActorID, OrganizationID, OrganizationRef, ProjectID, ProjectRef      string
	SourceProfile, SourceProfileRef, SourceContextDigest, SourceLeaseRef string
	SourceFenceDigest, SourceRunRef, OperationKey                        string
	IntentOperation, IdempotencyKey, IntentDigest                        string
	TargetArtifactID, TargetArtifactRef, SourceRevisionRef               string
	SourceProfileVersion, SourceLeaseGeneration, ExpectedArtifactVersion int64
}

// preparedContentRequest combines immutable binding and inspected metadata with the input reader;
// the reader itself is excluded from the request commitment.
// preparedContentRequest объединяет неизменяемую привязку и проверенные метаданные с входным
// reader; сам reader не включается в commitment запроса.
type preparedContentRequest struct {
	Binding                                              preparedContentBinding
	FileName, MediaType, Digest, ScanState, PreviewState string
	SizeBytes                                            int64
	Body                                                 io.Reader
}

// preparedContent holds ledger identity, prepared artifact/revision references, generation and the
// accepted object receipt plus antivirus result.
// preparedContent хранит идентификатор ledger, ссылки подготовленных artifact/revision, поколение
// и принятую квитанцию объекта с результатом антивируса.
type preparedContent struct {
	LedgerID, ContentRef, ArtifactRef, RevisionRef, FileName string
	Generation                                               int64
	Prepared                                                 command.PreparedArtifact
}

var (
	// queryPreparedContentInsert inserts the durable unique intent before object Put, doing nothing on
	// the exact organization/actor/operation idempotency conflict.
	// queryPreparedContentInsert добавляет durable unique intent до object Put, ничего не меняя при
	// точном конфликте organization/actor/operation idempotency.
	//go:embed sql/prepared_content_insert.sql
	queryPreparedContentInsert string
	// queryPreparedContentReplay reads the existing unexpired intent by its original tenant, actor,
	// intent operation, idempotency key and operation key.
	// queryPreparedContentReplay читает прежний неистёкший intent по исходным tenant, actor, intent
	// operation, idempotency key и operation key.
	//go:embed sql/prepared_content_replay.sql
	queryPreparedContentReplay string
	// queryPreparedContentWriterLock locks only an IN_FLIGHT generation-one row whose writer deadline
	// has not expired before the single external Put.
	// queryPreparedContentWriterLock блокирует только IN_FLIGHT строку первого поколения с неистёкшим
	// writer deadline перед единственным внешним Put.
	//go:embed sql/prepared_content_writer_lock.sql
	queryPreparedContentWriterLock string
	// queryPreparedContentStage records STAGED or UNKNOWN and accepted object pins on the original
	// IN_FLIGHT generation-one ledger row.
	// queryPreparedContentStage записывает STAGED либо UNKNOWN и принятые object pins в исходную
	// IN_FLIGHT строку ledger первого поколения.
	//go:embed sql/prepared_content_stage.sql
	queryPreparedContentStage string
	// queryPreparedContentResolve locks unexpired STAGED clean content bound to the exact current plan
	// revision and operation digest/size tuple before adoption.
	// queryPreparedContentResolve блокирует неистёкшее чистое STAGED содержимое, привязанное к точным
	// текущим plan revision и operation digest/size, до принятия.
	//go:embed sql/prepared_content_resolve.sql
	queryPreparedContentResolve string
	// queryPreparedContentLink links STAGED content to an exact current plan operation while
	// preserving first origin references and forward revision binding.
	// queryPreparedContentLink привязывает STAGED содержимое к точной операции текущего плана,
	// сохраняя первые origin references и направленную вперёд привязку ревизии.
	//go:embed sql/prepared_content_link.sql
	queryPreparedContentLink string
	// queryPreparedContentAdopt moves STAGED content to ADOPTED only when tenant/project/target, exact
	// object receipt and clean antivirus evidence match the saved artifact revision.
	// queryPreparedContentAdopt переводит STAGED содержимое в ADOPTED только при совпадении
	// tenant/project/target, точной квитанции объекта и чистого antivirus evidence сохранённой ревизии
	// артефакта.
	//go:embed sql/prepared_content_adopt.sql
	queryPreparedContentAdopt string
	// queryPreparedContentAbandon expires unadopted STAGED or UNKNOWN content, advances generation and
	// records ABANDONED within the supplied transaction.
	// queryPreparedContentAbandon завершает срок не принятого STAGED либо UNKNOWN содержимого,
	// увеличивает поколение и записывает ABANDONED в переданной транзакции.
	//go:embed sql/prepared_content_abandon.sql
	queryPreparedContentAbandon string
	// queryPreparedContentBinding reads retained origin pins for an exact current draft/stale plan
	// operation under a shared lock, without returning an object receipt or new authority.
	// queryPreparedContentBinding читает сохранённые origin pins точной операции текущего draft/stale
	// плана под shared lock, не выдавая квитанцию объекта или новые полномочия.
	//go:embed sql/prepared_content_binding.sql
	queryPreparedContentBinding string
	// queryPreparedContentBindRevision persists the exact ledger-to-plan-revision binding after
	// linking STAGED content and ignores an already identical binding.
	// queryPreparedContentBindRevision сохраняет точную привязку ledger к plan revision после
	// связывания STAGED содержимого и игнорирует уже существующую такую привязку.
	//go:embed sql/prepared_content_bind_revision.sql
	queryPreparedContentBindRevision string
)

// preparedContentRequestDigest hashes the existing JSON metadata and binding representation,
// excluding the reader because inspected size and digest commit its body.
// preparedContentRequestDigest хеширует существующее JSON-представление метаданных и привязки без
// reader: проверенные размер и дайджест закрепляют его тело.
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

// validPreparedContentRequest checks UUIDs, lowercase digests, bounded source references,
// version/target consistency and a nonnil reader with declared size from zero to one MiB, clean
// scan and closed preview states; it does not authorize the caller.
// validPreparedContentRequest проверяет UUID, строчные дайджесты, ограниченные ссылки источника,
// согласованность версий/target и ненулевой reader с размером от нуля до одного MiB, CLEAN и
// закрытым набором preview states; не авторизует caller.
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

// scanPreparedContent scans the exact ledger/receipt columns and validates saved antivirus
// evidence against digest and size, rejecting infected or inconsistent evidence as conflict.
// scanPreparedContent сканирует точные колонки ledger/receipt и проверяет сохранённое antivirus
// evidence относительно digest и size, отклоняя заражённое либо несогласованное evidence как
// conflict.
func scanPreparedContent(row pgx.Row) (preparedContent, string, string, error) {
	var content preparedContent
	var state, requestDigest string
	var scanJSON []byte
	err := row.Scan(&content.LedgerID, &content.ContentRef, &content.Generation, &content.ArtifactRef, &content.RevisionRef,
		&content.FileName, &content.Prepared.MediaType, &content.Prepared.Digest, &content.Prepared.SizeBytes,
		&content.Prepared.ScanState, &content.Prepared.PreviewState, &content.Prepared.ObjectKey,
		&content.Prepared.ObjectVersion, &content.Prepared.ObjectETag, &state, &requestDigest, &scanJSON)
	content.Prepared.Ref = content.ArtifactRef
	if err != nil {
		return preparedContent{}, "", "", err
	}
	content.Prepared.Antivirus, err = decodeArtifactAntivirus(scanJSON, content.Prepared.Digest, content.Prepared.SizeBytes)
	if err != nil || content.Prepared.Antivirus.Infected {
		return preparedContent{}, "", "", errs.ErrConflict
	}
	return content, state, requestDigest, nil
}

// preparedContentSeekableBody reads at most declared size plus one byte, verifies exact size and
// SHA-256 and returns an in-memory seekable reader required by signed object Put.
// preparedContentSeekableBody читает не более заявленного размера плюс один байт, проверяет точные
// размер и SHA-256 и возвращает seekable reader в памяти для подписанного object Put.
func preparedContentSeekableBody(request preparedContentRequest) (*bytes.Reader, error) {
	if request.Body == nil || request.SizeBytes < 0 || request.SizeBytes > 1<<20 {
		return nil, errs.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, request.SizeBytes+1))
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	digest := sha256.Sum256(body)
	if int64(len(body)) != request.SizeBytes || "sha256:"+hex.EncodeToString(digest[:]) != request.Digest {
		return nil, errs.ErrInvalid
	}
	return bytes.NewReader(body), nil
}

// stagePreparedContent validates and scans the body, reserves a durable unique intent before one
// Put and exact Head, and accepts only matching STAGED replay; uncertainty never permits another
// Put even if recording UNKNOWN or commit fails.
// stagePreparedContent проверяет тело и антивирус, резервирует durable unique intent до одного Put
// и точного Head, принимает только совпадающий STAGED replay; неопределённость не разрешает новый
// Put даже при отказе записи UNKNOWN либо commit.
func (repository *Repository) stagePreparedContent(ctx context.Context, request preparedContentRequest) (preparedContent, error) {
	if !validPreparedContentRequest(request) {
		return preparedContent{}, errs.ErrInvalid
	}
	body, err := preparedContentSeekableBody(request)
	if err != nil {
		return preparedContent{}, err
	}
	scan, err := repository.scanArtifact(ctx, body, request.Digest, request.SizeBytes)
	if err != nil {
		return preparedContent{}, err
	}
	if scan.Infected {
		return preparedContent{}, errs.ErrInvalid
	}
	scanJSON, err := artifactAntivirusJSON(scan)
	if err != nil {
		return preparedContent{}, err
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
	err = repository.pool.QueryRow(ctx, queryPreparedContentInsert, pgx.StrictNamedArgs{
		"ref": contentRef, "organization_id": b.OrganizationID, "project_id": b.ProjectID, "origin_project_ref": b.ProjectRef, "actor_id": b.ActorID,
		"intent_operation": b.IntentOperation, "idempotency_key": b.IdempotencyKey, "intent_digest": b.IntentDigest, "request_digest": requestDigest,
		"operation_key": b.OperationKey, "source_profile": b.SourceProfile, "source_profile_ref": b.SourceProfileRef,
		"source_profile_version": b.SourceProfileVersion, "source_context_digest": b.SourceContextDigest,
		"source_lease_ref": b.SourceLeaseRef, "source_lease_generation": b.SourceLeaseGeneration, "source_fence_digest": b.SourceFenceDigest,
		"source_run_ref": b.SourceRunRef, "target_artifact_id": targetID, "target_artifact_ref": b.TargetArtifactRef,
		"source_revision_ref": b.SourceRevisionRef, "expected_artifact_version": b.ExpectedArtifactVersion,
		"prepared_artifact_ref": artifactRef, "prepared_revision_ref": revisionRef, "file_name": request.FileName, "media_type": request.MediaType,
		"digest": request.Digest, "size_bytes": request.SizeBytes, "scan_state": request.ScanState, "preview_state": request.PreviewState, "object_key": key,
		"antivirus_report": scanJSON,
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
		Digest: request.Digest, SizeBytes: request.SizeBytes, Body: body})
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
			ScanState: request.ScanState, PreviewState: request.PreviewState, Antivirus: scan}}, nil
}

// exactPreparedReceipt requires the exact key, digest and size plus nonempty version and ETag;
// callers separately compare the complete authoritative Head receipt.
// exactPreparedReceipt требует точные key, digest и size и непустые version и ETag; caller
// отдельно сравнивает полную авторитетную квитанцию Head.
func exactPreparedReceipt(receipt objectstorage.Receipt, key, digest string, size int64) bool {
	return receipt.Key == key && receipt.VersionID != "" && receipt.ETag != "" && receipt.Digest == digest && receipt.SizeBytes == size
}

// resolvePreparedContentTx resolves an exact plan-bound clean prepared object after caller-owned
// fresh authority checks, mapping missing rows to conflict and other read failures to unavailable.
// resolvePreparedContentTx разрешает точный чистый подготовленный объект с привязкой к плану после
// свежих проверок полномочий caller; отсутствие строки даёт conflict, прочий отказ чтения —
// unavailable.
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

// preparedContentTransition executes a supplied prepared-content transition and requires exactly
// one affected row, keeping database failures distinct from a state mismatch.
// preparedContentTransition выполняет переданный переход prepared content и требует ровно одну
// изменённую строку, различая отказ БД и несовпадение состояния.
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

// linkPreparedContentTx links the ledger to the exact plan operation using the one-row transition
// and then inserts its revision binding in the same caller transaction.
// linkPreparedContentTx связывает ledger с точной операцией плана через переход одной строки и
// затем добавляет revision binding в той же транзакции caller.
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

// adoptPreparedContentTx applies the dedicated receipt-checked adoption SQL within the caller
// transaction and requires one matching ledger row.
// adoptPreparedContentTx применяет отдельный SQL принятия с проверкой receipt в транзакции caller
// и требует одну подходящую строку ledger.
func (repository *Repository) adoptPreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, ledgerID, revisionID string) error {
	return preparedContentTransition(ctx, tx, queryPreparedContentAdopt, pgx.StrictNamedArgs{
		"id": ledgerID, "organization_id": current.organizationID, "revision_id": revisionID,
	})
}

// abandonPreparedContentTx uses the dedicated owner-scoped abandonment transition and requires one
// eligible unadopted ledger row.
// abandonPreparedContentTx использует отдельный переход abandonment в области владельца и требует
// одну подходящую непринятую строку ledger.
func (repository *Repository) abandonPreparedContentTx(ctx context.Context, tx pgx.Tx, current scope, ledgerID string) error {
	return preparedContentTransition(ctx, tx, queryPreparedContentAbandon, pgx.StrictNamedArgs{"id": ledgerID, "organization_id": current.organizationID})
}

// readPreparedContentBindingTx reads exact retained source/intent pins for a plan revision and
// operation, including after TTL until terminal scrub; lookup does not grant authority or return
// receipt.
// readPreparedContentBindingTx читает точные сохранённые source/intent pins ревизии и операции
// плана, в том числе после TTL до terminal scrub; lookup не выдаёт полномочия или receipt.
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
