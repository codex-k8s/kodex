package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryRuntimeFilesReadCatalog locks the exact live claimed lease with fence/generation and frozen
	// catalog pins, checks run/root/actor/resource eligibility and returns the authoritative catalog
	// actor/project.
	// queryRuntimeFilesReadCatalog блокирует точный живой claimed lease с fence/generation и pins
	// замороженного каталога, проверяет допустимость run/root/actor/resources и возвращает
	// авторитетные actor/project каталога.
	//go:embed sql/runtime_files_read_catalog.sql
	queryRuntimeFilesReadCatalog string
	// queryRuntimeFilesSearch reads ordered eligible file descriptors for the exact catalog/purpose
	// with literal case-insensitive filename search, after-ref cursor and bounded page limit.
	// queryRuntimeFilesSearch читает упорядоченные допустимые file descriptors точного catalog/purpose
	// с буквальным поиском имени без учёта регистра, after-ref cursor и ограничением страницы.
	//go:embed sql/runtime_files_search.sql
	queryRuntimeFilesSearch string
	// queryRuntimeFilesCount counts the same eligible catalog/purpose/name-filter entries used by
	// search without applying pagination.
	// queryRuntimeFilesCount считает те же допустимые entries по catalog/purpose/name filter, что и
	// поиск, без применения пагинации.
	//go:embed sql/runtime_files_count.sql
	queryRuntimeFilesCount string
	// queryRuntimeFilesMetadata joins exact visible entry and artifact-history/content pins before
	// returning descriptor and object receipt metadata for the requested revision/digest.
	// queryRuntimeFilesMetadata связывает точные visible entry и artifact-history/content pins до
	// возврата descriptor и метаданных object receipt запрошенных revision/digest.
	//go:embed sql/runtime_files_metadata.sql
	queryRuntimeFilesMetadata string
	// queryRuntimeFilesAudit inserts a successful catalog-read audit only while the associated lease
	// remains claimed, unexpired and before the execution deadline.
	// queryRuntimeFilesAudit добавляет audit успешного чтения каталога только пока связанный lease
	// остаётся claimed, неистёкшим и до execution deadline.
	//go:embed sql/runtime_files_audit.sql
	queryRuntimeFilesAudit string
	// queryRuntimeFilesBodyAudit records artifact body-read audit using the exact live
	// lease/fence/generation and authoritative root initiator; the SQL does not itself read object
	// storage.
	// queryRuntimeFilesBodyAudit фиксирует audit чтения тела артефакта по точным живым
	// lease/fence/generation и авторитетному root initiator; сам SQL не читает object storage.
	//go:embed sql/runtime_files_body_audit.sql
	queryRuntimeFilesBodyAudit string
)

// runtimeFilesRead carries the resolved scope and frozen catalog identity; actor and authority
// project are replaced with the authoritative catalog values after the lease read.
// runtimeFilesRead переносит разрешённую область и идентификатор замороженного каталога; actor и
// authority project заменяются авторитетными значениями каталога после чтения lease.
type runtimeFilesRead struct {
	current scope
	id      string
	catalog runtimecontract.RuntimeFileCatalog
}

// readRuntimeFiles retries the complete owner read through the existing locked-read policy while
// preserving immutable execution pins and the operation-specific fetch callback.
// readRuntimeFiles повторяет полное чтение владельца через прежнюю locked-read policy, сохраняя
// неизменяемые execution pins и callback конкретной операции.
func readRuntimeFiles[T any](ctx context.Context, repository *Repository, principal value.Principal, execution query.ExecutionFileContext, operation string,
	fetch func(context.Context, pgx.Tx, runtimeFilesRead) (T, int, error),
) (T, error) {
	return retryAssistantLockedRead(ctx, func(attemptCtx context.Context) (T, error) {
		return readRuntimeFilesOnce(attemptCtx, repository, principal, execution, operation, fetch)
	})
}

// readRuntimeFilesOnce opens repeatable read, resolves exact live lease/catalog authority,
// validates the catalog and fetches the result; requires one successful audit row and commit
// before returning data, with rollback-owned retry classification.
// readRuntimeFilesOnce открывает repeatable read, разрешает полномочия точных живых lease/catalog,
// проверяет каталог и читает результат; требует одну audit row и commit до выдачи данных, с
// классификацией повтора в rollback.
func readRuntimeFilesOnce[T any](ctx context.Context, repository *Repository, principal value.Principal, execution query.ExecutionFileContext, operation string,
	fetch func(context.Context, pgx.Tx, runtimeFilesRead) (T, int, error),
) (_ T, resultError error) {
	var zero T
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return zero, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	fence := sha256.Sum256([]byte(execution.Fence))
	read := runtimeFilesRead{current: current}
	err = tx.QueryRow(ctx, queryRuntimeFilesReadCatalog, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "authority_project": current.authorityProjectID,
		"lease_ref": execution.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "generation": execution.Generation,
		"catalog_ref": execution.CatalogRef, "catalog_digest": execution.CatalogDigest, "purpose": execution.Purpose,
	}).Scan(&read.id, &read.catalog.Ref, &read.catalog.Digest, &read.catalog.Total, &read.catalog.Purposes, &read.current.actorID, &read.current.authorityProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errs.ErrNotFound
	}
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if read.catalog.Validate() != nil {
		return zero, errs.ErrUnavailable
	}
	result, count, err := fetch(ctx, tx, read)
	if err != nil {
		return zero, err
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return zero, err
	}
	tag, err := tx.Exec(ctx, queryRuntimeFilesAudit, pgx.StrictNamedArgs{
		"audit_ref": auditRef, "catalog_id": read.id, "action": "runtime.files." + operation,
		"summary": fmt.Sprintf("purpose=%s count=%d", execution.Purpose, count), "correlation": current.correlationRef,
	})
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() != 1 {
		return zero, errs.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return result, nil
}

// executionFileDestinations returns the fixed fourteen-field descriptor scan order shared by file
// pages and exact metadata queries, without reading or validating object bodies.
// executionFileDestinations возвращает фиксированный порядок четырнадцати полей descriptor для
// Scan страниц и точных metadata queries, не читая и не проверяя тела объектов.
func executionFileDestinations(file *entity.ExecutionFileDescriptor) []any {
	return []any{&file.EntryRef, &file.ArtifactRef, &file.Revision, &file.Version, &file.Digest,
		&file.Name, &file.MediaType, &file.SizeBytes, &file.Purpose, &file.ProjectRef, &file.RunRef,
		&file.Source, &file.SourceRef, &file.SourceRevisionRef}
}

// SearchExecutionFiles routes filename search through the common exact-catalog read with a
// distinct search operation and cursor/audit identity.
// SearchExecutionFiles направляет поиск по имени через общее чтение точного каталога с отдельными
// search operation и cursor/audit identity.
func (repository *Repository) SearchExecutionFiles(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, search string, page query.Page) (entity.ExecutionFilePage, error) {
	return repository.listExecutionFiles(ctx, principal, execution, search, page, "search")
}

// GetExecutionFileManifest routes the unfiltered descriptor manifest through the shared read with
// its own manifest operation and pagination identity.
// GetExecutionFileManifest направляет нефильтрованный manifest descriptors через общее чтение с
// отдельными manifest operation и pagination identity.
func (repository *Repository) GetExecutionFileManifest(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, page query.Page) (entity.ExecutionFilePage, error) {
	return repository.listExecutionFiles(ctx, principal, execution, "", page, "manifest")
}

// listExecutionFiles counts and reads the same eligible entries, scopes the cursor to exact lease
// generation and operation, fetches limit plus one and emits only the bounded page.
// listExecutionFiles считает и читает одинаковые допустимые entries, связывает cursor с точными
// lease generation и operation, читает limit плюс один и выдаёт только ограниченную страницу.
func (repository *Repository) listExecutionFiles(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, search string, page query.Page, operation string) (entity.ExecutionFilePage, error) {
	return readRuntimeFiles(ctx, repository, principal, execution, operation, func(ctx context.Context, tx pgx.Tx, read runtimeFilesRead) (entity.ExecutionFilePage, int, error) {
		result := entity.ExecutionFilePage{Catalog: read.catalog, Items: []entity.ExecutionFileDescriptor{}}
		filter := query.Filter{ResourceRef: execution.CatalogRef, ExpectedCatalogDigest: execution.CatalogDigest, Query: search, SourceKind: execution.Purpose, Page: page}
		// Scope включает exact lease generation; cursor другого attempt не подходит.
		kind := fmt.Sprintf("RUNTIME_FILE:%s:%d:%s", execution.LeaseRef, execution.Generation, operation)
		after, err := decodeCatalogCursor(read.current, kind, filter)
		if err != nil {
			return result, 0, err
		}
		args := pgx.StrictNamedArgs{"catalog_id": read.id, "purpose": execution.Purpose, "query": search}
		if err := tx.QueryRow(ctx, queryRuntimeFilesCount, args).Scan(&result.Total); err != nil {
			return result, 0, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		limit := boundedPage(page)
		args["after_ref"], args["page_limit"] = after, limit+1
		rows, err := tx.Query(ctx, queryRuntimeFilesSearch, args)
		if err != nil {
			return result, 0, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		for rows.Next() {
			var file entity.ExecutionFileDescriptor
			if err := rows.Scan(executionFileDestinations(&file)...); err != nil {
				rows.Close()
				return result, 0, assistantLockedReadError(err, errs.ErrUnavailable)
			}
			result.Items = append(result.Items, file)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, 0, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		if len(result.Items) > int(limit) {
			result.Items = result.Items[:limit]
			result.Next = encodeCatalogCursor(read.current, kind, filter, result.Items[len(result.Items)-1].EntryRef)
		}
		return result, len(result.Items), nil
	})
}

// executionFileContent keeps a public file descriptor beside its exact object key, optional
// version/ETag, digest and size for metadata and bounded preview reads.
// executionFileContent хранит публичный file descriptor рядом с точными object key,
// необязательными version/ETag, digest и size для metadata и ограниченного preview.
type executionFileContent struct {
	file                       entity.ExecutionFileDescriptor
	key, version, etag, digest string
	size                       int64
}

// readExecutionFileMetadata reads an exact catalog entry/artifact/revision/digest tuple and
// rejects empty object keys or content-versus-descriptor digest/size mismatch before object
// access.
// readExecutionFileMetadata читает точный tuple catalog entry/artifact/revision/digest и отклоняет
// пустой object key либо несовпадение digest/size content и descriptor до доступа к объекту.
func readExecutionFileMetadata(ctx context.Context, tx pgx.Tx, read runtimeFilesRead, execution query.ExecutionFileContext, exact query.ExecutionFileRef) (executionFileContent, error) {
	var result executionFileContent
	destinations := append(executionFileDestinations(&result.file), &result.key, &result.version, &result.etag, &result.digest, &result.size)
	err := tx.QueryRow(ctx, queryRuntimeFilesMetadata, pgx.StrictNamedArgs{
		"catalog_id": read.id, "purpose": execution.Purpose, "entry_ref": exact.EntryRef,
		"artifact_ref": exact.ArtifactRef, "revision": exact.Revision, "digest": exact.Digest,
	}).Scan(destinations...)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if result.key == "" || result.digest != result.file.Digest || result.size != result.file.SizeBytes {
		return result, errs.ErrConflict
	}
	return result, nil
}

// GetExecutionFileMetadata returns catalog plus the resolved descriptor through the audited
// owner-read transaction; internal object storage coordinates remain private.
// GetExecutionFileMetadata возвращает каталог и разрешённый descriptor через аудируемую транзакцию
// чтения владельца; внутренние координаты object storage не выдаются.
func (repository *Repository) GetExecutionFileMetadata(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, exact query.ExecutionFileRef) (entity.ExecutionFileMetadata, error) {
	return readRuntimeFiles(ctx, repository, principal, execution, "metadata", func(ctx context.Context, tx pgx.Tx, read runtimeFilesRead) (entity.ExecutionFileMetadata, int, error) {
		content, err := readExecutionFileMetadata(ctx, tx, read, execution, exact)
		return entity.ExecutionFileMetadata{Catalog: read.catalog, File: content.file}, 1, err
	})
}

// PreviewExecutionFile accepts supported textual media and a one-to-16384 byte limit, verifies
// object metadata pins, reads only limit plus one bytes and closes the body; full previews verify
// content SHA-256, while truncated previews return a validated UTF-8 prefix and its own digest.
// PreviewExecutionFile принимает допустимый текстовый media type и лимит от одного до16384 байт,
// проверяет object metadata pins, читает только limit плюс один и закрывает тело; полный preview
// проверяет content SHA-256, усечённый выдаёт допустимый UTF-8 prefix с собственным дайджестом.
func (repository *Repository) PreviewExecutionFile(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, exact query.ExecutionFileRef, maximum int32) (entity.ExecutionFilePreview, error) {
	return readRuntimeFiles(ctx, repository, principal, execution, "preview", func(ctx context.Context, tx pgx.Tx, read runtimeFilesRead) (entity.ExecutionFilePreview, int, error) {
		var result entity.ExecutionFilePreview
		content, err := readExecutionFileMetadata(ctx, tx, read, execution, exact)
		if err != nil {
			return result, 0, err
		}
		if !executionPreviewMediaType(content.file.MediaType) || maximum < 1 || maximum > 16384 {
			return result, 0, errs.ErrInvalid
		}
		object, err := repository.objects.Get(ctx, content.key, content.version)
		if err != nil {
			return result, 0, mapObjectStorageError(err)
		}
		if object.Digest != content.digest || object.SizeBytes != content.size ||
			(content.version != "" && object.VersionID != content.version) || (content.etag != "" && object.ETag != content.etag) {
			_ = object.Body.Close()
			return result, 0, errs.ErrConflict
		}
		// Полная версия объекта закреплена metadata proof. Preview читает только
		// bounded prefix, поэтому большой файл не попадает целиком в память RPC.
		prefix, readErr := io.ReadAll(io.LimitReader(object.Body, int64(maximum)+1))
		closeErr := object.Body.Close()
		if readErr != nil || closeErr != nil {
			return result, 0, errs.ErrUnavailable
		}
		if int64(len(prefix)) != min(content.size, int64(maximum)+1) {
			return result, 0, errs.ErrConflict
		}
		result.Truncated = content.size > int64(maximum)
		if !result.Truncated {
			digest := sha256.Sum256(prefix)
			if "sha256:"+hex.EncodeToString(digest[:]) != content.digest {
				return result, 0, errs.ErrConflict
			}
		} else {
			prefix = prefix[:maximum]
			prefix = executionPreviewPrefix(prefix)
		}
		if !utf8.Valid(prefix) || strings.ContainsRune(string(prefix), '\x00') {
			return result, 0, errs.ErrInvalid
		}
		digest := sha256.Sum256(prefix)
		result.Metadata = entity.ExecutionFileMetadata{Catalog: read.catalog, File: content.file}
		result.Text, result.Digest = string(prefix), "sha256:"+hex.EncodeToString(digest[:])
		return result, 1, nil
	})
}

// executionPreviewPrefix removes only a trailing incomplete UTF-8 rune from a bounded prefix;
// invalid complete byte sequences remain so the caller can reject them.
// executionPreviewPrefix удаляет только незавершённый UTF-8 rune в конце ограниченного prefix;
// неверные полные последовательности сохраняются для отказа в caller.
func executionPreviewPrefix(prefix []byte) []byte {
	for offset := 0; offset < len(prefix); {
		if !utf8.FullRune(prefix[offset:]) {
			return prefix[:offset]
		}
		r, size := utf8.DecodeRune(prefix[offset:])
		if r == utf8.RuneError && size == 1 {
			return prefix
		}
		offset += size
	}
	return prefix
}

// executionPreviewMediaType parses the media type and accepts text/* plus JSON, YAML, TOML or XML
// application types; malformed or unregistered application types are rejected.
// executionPreviewMediaType разбирает media type и принимает text/* и application типы JSON, YAML,
// TOML либо XML; неверные либо незарегистрированные application types отклоняются.
func executionPreviewMediaType(value string) bool {
	base, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	if strings.HasPrefix(base, "text/") {
		return true
	}
	switch base {
	case "application/json", "application/yaml", "application/toml", "application/xml":
		return true
	default:
		return false
	}
}
