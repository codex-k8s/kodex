package platform

import (
	"bytes"
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// repositoryBundleContentPolicy names the exact native per-decoded-file admission rule; outer container CLEAN is never substituted for these independent receipts.
// repositoryBundleContentPolicy обозначает exact native per-decoded-file admission; outer container CLEAN не заменяет отдельные receipts.
const repositoryBundleContentPolicy = "native-decoded-format-clamav-v1"

// queryRepositoryBundleSourcePin resolves exact immutable source revision and S3 receipt while retaining its ACTIVE head through owner commit, before any retention consumer is inserted.
// queryRepositoryBundleSourcePin разрешает exact immutable source revision и S3 receipt, удерживая ACTIVE head до owner commit перед вставкой retention consumer.
//
//go:embed sql/repository_bundle__source_pin.sql
var queryRepositoryBundleSourcePin string

// repositoryBundleSourcePin is private owner storage metadata for an existing immutable artifact; no new object is uploaded or cleaned up by bundle preparation.
// repositoryBundleSourcePin — private owner storage metadata существующего immutable artifact; bundle preparation не загружает и не очищает новые objects.
type repositoryBundleSourcePin struct {
	RevisionRef string
	Receipt     objectstorage.Receipt
}

// repositoryBundleFileReceipt binds native format verdict and mandatory antivirus evidence to the exact approved path, mode, bytes and SHA256; it contains no file body.
// repositoryBundleFileReceipt связывает native format verdict и обязательный antivirus evidence с exact approved path, mode, bytes и SHA256; тела файла в ней нет.
type repositoryBundleFileReceipt struct {
	Entry     repositorybundle.Entry
	MediaType string
	Scan      value.ArtifactScan
}

// inspectRepositoryBundleFiles applies the existing format policy using each real filename, then scans every exact decoded byte stream. Unsupported, quarantined, infected or unscannable members stop preparation before native gate creation.
// inspectRepositoryBundleFiles применяет прежнюю format policy с настоящим именем каждого файла, затем сканирует exact decoded bytes. Unsupported, quarantined, infected либо unscannable member останавливает preparation до native Gate creation.
func (repository *Repository) inspectRepositoryBundleFiles(ctx context.Context, files []repositorybundle.File) ([]repositoryBundleFileReceipt, error) {
	receipts := make([]repositoryBundleFileReceipt, 0, len(files))
	for _, file := range files {
		reader := bytes.NewReader(file.Bytes)
		verdict, err := artifactpolicy.InspectReader(file.Entry.Path, "application/octet-stream", reader, int64(len(file.Bytes)))
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if verdict.ScanState != artifactpolicy.ScanClean {
			return nil, errs.ErrForbidden
		}
		scan, err := repository.scanArtifact(ctx, reader, "sha256:"+file.Entry.SHA256, file.Entry.Bytes)
		if err != nil {
			return nil, err
		}
		if scan.Infected {
			return nil, errs.ErrForbidden
		}
		receipts = append(receipts, repositoryBundleFileReceipt{Entry: file.Entry, MediaType: verdict.MediaType, Scan: scan})
	}
	return receipts, nil
}

// resolveRepositoryBundleSource captures one exact already existing S3 receipt under owner locks; stale or foreign locators never become storage authority.
// resolveRepositoryBundleSource получает exact existing S3 receipt под owner locks; stale либо foreign locators не становятся storage authority.
func resolveRepositoryBundleSource(ctx context.Context, tx pgx.Tx, current scope, projectID string, pin entity.RepositoryBundleArtifactPin) (repositoryBundleSourcePin, error) {
	var source repositoryBundleSourcePin
	err := tx.QueryRow(ctx, queryRepositoryBundleSourcePin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "artifact_ref": pin.Ref, "revision": pin.Revision, "sha256": pin.SHA256, "size_bytes": pin.Size}).Scan(
		&source.RevisionRef, &source.Receipt.Key, &source.Receipt.VersionID, &source.Receipt.ETag, &source.Receipt.Digest, &source.Receipt.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, errs.ErrNotFound
	}
	if err != nil {
		return source, errs.ErrUnavailable
	}
	if source.RevisionRef == "" || !objectstorage.ValidKey(source.Receipt.Key) || source.Receipt.VersionID == "" || source.Receipt.ETag == "" || source.Receipt.Digest != "sha256:"+pin.SHA256 || source.Receipt.SizeBytes != pin.Size {
		return source, errs.ErrUnavailable
	}
	return source, nil
}
