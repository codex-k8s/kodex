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
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/runtime_files_read_catalog.sql
	queryRuntimeFilesReadCatalog string
	//go:embed sql/runtime_files_search.sql
	queryRuntimeFilesSearch string
	//go:embed sql/runtime_files_count.sql
	queryRuntimeFilesCount string
	//go:embed sql/runtime_files_metadata.sql
	queryRuntimeFilesMetadata string
	//go:embed sql/runtime_files_audit.sql
	queryRuntimeFilesAudit string
	//go:embed sql/runtime_files_body_audit.sql
	queryRuntimeFilesBodyAudit string
)

type runtimeFilesRead struct {
	current scope
	id      string
	catalog runtimecontract.RuntimeFileCatalog
}

func readRuntimeFiles[T any](ctx context.Context, repository *Repository, principal value.Principal, execution query.ExecutionFileContext, operation string,
	fetch func(context.Context, pgx.Tx, runtimeFilesRead) (T, int, error),
) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return zero, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return zero, errs.ErrUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
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
	if err != nil || read.catalog.Validate() != nil {
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
		return zero, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return zero, errs.ErrNotFound
	}
	if tx.Commit(ctx) != nil {
		return zero, errs.ErrUnavailable
	}
	return result, nil
}

func executionFileDestinations(file *entity.ExecutionFileDescriptor) []any {
	return []any{&file.EntryRef, &file.ArtifactRef, &file.Revision, &file.Version, &file.Digest,
		&file.Name, &file.MediaType, &file.SizeBytes, &file.Purpose, &file.ProjectRef, &file.RunRef,
		&file.Source, &file.SourceRef, &file.SourceRevisionRef}
}

func (repository *Repository) SearchExecutionFiles(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, search string, page query.Page) (entity.ExecutionFilePage, error) {
	return repository.listExecutionFiles(ctx, principal, execution, search, page, "search")
}

func (repository *Repository) GetExecutionFileManifest(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, page query.Page) (entity.ExecutionFilePage, error) {
	return repository.listExecutionFiles(ctx, principal, execution, "", page, "manifest")
}

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
		if tx.QueryRow(ctx, queryRuntimeFilesCount, args).Scan(&result.Total) != nil {
			return result, 0, errs.ErrUnavailable
		}
		limit := boundedPage(page)
		args["after_ref"], args["page_limit"] = after, limit+1
		rows, err := tx.Query(ctx, queryRuntimeFilesSearch, args)
		if err != nil {
			return result, 0, errs.ErrUnavailable
		}
		for rows.Next() {
			var file entity.ExecutionFileDescriptor
			if rows.Scan(executionFileDestinations(&file)...) != nil {
				rows.Close()
				return result, 0, errs.ErrUnavailable
			}
			result.Items = append(result.Items, file)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, 0, errs.ErrUnavailable
		}
		if len(result.Items) > int(limit) {
			result.Items = result.Items[:limit]
			result.Next = encodeCatalogCursor(read.current, kind, filter, result.Items[len(result.Items)-1].EntryRef)
		}
		return result, len(result.Items), nil
	})
}

type executionFileContent struct {
	file                       entity.ExecutionFileDescriptor
	key, version, etag, digest string
	size                       int64
}

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
		return result, errs.ErrUnavailable
	}
	if result.key == "" || result.digest != result.file.Digest || result.size != result.file.SizeBytes {
		return result, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) GetExecutionFileMetadata(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, exact query.ExecutionFileRef) (entity.ExecutionFileMetadata, error) {
	return readRuntimeFiles(ctx, repository, principal, execution, "metadata", func(ctx context.Context, tx pgx.Tx, read runtimeFilesRead) (entity.ExecutionFileMetadata, int, error) {
		content, err := readExecutionFileMetadata(ctx, tx, read, execution, exact)
		return entity.ExecutionFileMetadata{Catalog: read.catalog, File: content.file}, 1, err
	})
}

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

// Удаляет только незавершённый rune на границе prefix. Некорректные UTF-8
// последовательности сохраняются для закрытого отказа вызывающего кода.
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
