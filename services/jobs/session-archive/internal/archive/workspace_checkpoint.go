package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workspacePartReader opens only manifest-derived exact versions, retaining one response body and no part-sized buffer.
// workspacePartReader открывает только manifest-derived exact versions, удерживая один response body без part-sized buffer.
type workspacePartReader struct {
	ctx      context.Context
	store    objectstorage.Store
	manifest runtimecontract.WorkspaceArchiveManifest
	index    int
	body     io.ReadCloser
	digest   hash.Hash
	written  int64
	failed   bool
	closed   bool
}

// Read verifies each complete part before advancing; overlong, short or changed responses poison the reader.
// Read проверяет полную часть до следующей; лишние, усечённые или изменённые bytes закрывают reader.
func (reader *workspacePartReader) Read(raw []byte) (int, error) {
	if reader == nil || reader.failed || reader.closed || reader.ctx.Err() != nil {
		return 0, errWorkspaceObjectUnconfirmed
	}
	if len(raw) == 0 {
		return 0, nil
	}
	for reader.index < len(reader.manifest.Parts) {
		part := reader.manifest.Parts[reader.index]
		if reader.body == nil {
			key, err := reader.manifest.PartObjectKey(part.Index)
			if err != nil {
				reader.failed = true
				return 0, errWorkspaceObjectUnconfirmed
			}
			object, err := reader.store.Get(reader.ctx, key, part.Version)
			wanted := objectstorage.Receipt{Key: key, VersionID: part.Version, ETag: part.ETag, Digest: part.Digest, SizeBytes: part.SizeBytes}
			if err != nil || object.Body == nil || object.Receipt != wanted {
				if object.Body != nil {
					_ = object.Body.Close()
				}
				reader.failed = true
				return 0, errWorkspaceObjectUnconfirmed
			}
			reader.body, reader.digest, reader.written = object.Body, sha256.New(), 0
		}
		remaining := part.SizeBytes - reader.written
		if remaining > 0 {
			count, err := reader.body.Read(raw[:min(int64(len(raw)), remaining)])
			if count < 0 || int64(count) > min(int64(len(raw)), remaining) {
				reader.failed = true
				return 0, errWorkspaceObjectUnconfirmed
			}
			if count > 0 {
				_, _ = reader.digest.Write(raw[:count])
				reader.written += int64(count)
			}
			if err != nil && (!errors.Is(err, io.EOF) || reader.written != part.SizeBytes) || count == 0 && err == nil {
				reader.failed = true
				return count, errWorkspaceObjectUnconfirmed
			}
			if count > 0 {
				return count, nil
			}
		}
		var extra [1]byte
		count, err := reader.body.Read(extra[:])
		closeErr := reader.body.Close()
		reader.body = nil
		if count != 0 || !errors.Is(err, io.EOF) || closeErr != nil || reader.ctx.Err() != nil ||
			"sha256:"+hex.EncodeToString(reader.digest.Sum(nil)) != part.Digest {
			reader.failed = true
			return 0, errWorkspaceObjectUnconfirmed
		}
		reader.index++
	}
	return 0, io.EOF
}

// Close releases the current response even on partial TAR decoding; it never deletes or retries an object.
// Close освобождает текущий response даже при partial TAR decode; objects не удаляются и не перезаписываются.
func (reader *workspacePartReader) Close() error {
	if reader == nil || reader.closed {
		return nil
	}
	reader.closed = true
	if reader.body != nil {
		err := reader.body.Close()
		reader.body = nil
		return err
	}
	return nil
}

// VerifyWorkspaceCheckpoint verifies the bounded root and every part, including canonical TAR bytes and aggregate identity.
// It is read-only and confers neither restore authority nor CLEAN status; the caller supplies an owner-pinned checkpoint.
// VerifyWorkspaceCheckpoint проверяет bounded root и каждую часть, включая canonical TAR bytes и aggregate identity.
// Это read-only проверка без restore authority/CLEAN; caller передаёт owner-pinned checkpoint.
func VerifyWorkspaceCheckpoint(ctx context.Context, store objectstorage.Store, checkpoint runtimecontract.WorkspaceCheckpointBinding) (runtimecontract.WorkspaceArchiveManifest, error) {
	var empty runtimecontract.WorkspaceArchiveManifest
	if ctx == nil || ctx.Err() != nil || store == nil || checkpoint.Validate() != nil {
		return empty, errWorkspaceObjectUnconfirmed
	}
	manifest, err := readWorkspaceManifest(ctx, store, checkpoint)
	if err != nil {
		return empty, err
	}
	reader := &workspacePartReader{ctx: ctx, store: store, manifest: manifest}
	defer reader.Close()
	digest := sha256.New()
	source := io.TeeReader(&workspaceStreamReader{ctx: ctx, source: reader}, digest)
	tarReader := tar.NewReader(source)
	encoder, err := newWorkspaceStreamEncoder(ctx, io.Discard, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil {
		return empty, errWorkspaceObjectUnconfirmed
	}
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || !validWorkspaceArchiveHeader(header) {
			return empty, errWorkspaceObjectUnconfirmed
		}
		name := header.Name
		directory := header.Typeflag == tar.TypeDir
		var contents io.Reader = tarReader
		if directory {
			name = strings.TrimSuffix(name, "/")
			contents = nil
		}
		if encoder.appendOwnedEntry(name, directory, uint32(header.Mode), header.Size, contents, header.Uname) != nil {
			return empty, errWorkspaceObjectUnconfirmed
		}
	}
	// tar.Reader останавливается на двух нулевых блоках; следующий read подтверждает
	// последний part digest и запрещает любые дополнительные TAR/zero padding bytes.
	var extra [1]byte
	count, readErr := source.Read(extra[:])
	canonical, size, entries, streamBytes, finishErr := encoder.finish()
	if count != 0 || !errors.Is(readErr, io.EOF) || finishErr != nil || reader.Close() != nil || ctx.Err() != nil ||
		canonical != manifest.TreeSHA256 || hex.EncodeToString(digest.Sum(nil)) != canonical || size != manifest.Bytes ||
		entries != manifest.Entries || streamBytes != manifest.StreamBytes {
		return empty, errWorkspaceObjectUnconfirmed
	}
	return manifest, nil
}

// readWorkspaceManifest accepts only the exact server-pinned root receipt and checks all checkpoint fields before part reads.
// readWorkspaceManifest принимает только exact server-pinned root receipt и сверяет checkpoint fields до чтения частей.
func readWorkspaceManifest(ctx context.Context, store objectstorage.Store, checkpoint runtimecontract.WorkspaceCheckpointBinding) (runtimecontract.WorkspaceArchiveManifest, error) {
	var empty runtimecontract.WorkspaceArchiveManifest
	expected := checkpoint.Manifest
	object, err := store.Get(ctx, expected.Key, expected.Version)
	if err != nil || object.Body == nil {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		return empty, errWorkspaceObjectUnconfirmed
	}
	wanted := objectstorage.Receipt{Key: expected.Key, VersionID: expected.Version, ETag: expected.ETag, Digest: expected.Digest, SizeBytes: expected.SizeBytes}
	if object.Receipt != wanted {
		_ = object.Body.Close()
		return empty, errWorkspaceObjectUnconfirmed
	}
	raw, readErr := io.ReadAll(io.LimitReader(&workspaceStreamReader{ctx: ctx, source: object.Body}, expected.SizeBytes+1))
	closeErr := object.Body.Close()
	defer clear(raw)
	digest := sha256.Sum256(raw)
	if readErr != nil || closeErr != nil || ctx.Err() != nil || int64(len(raw)) != expected.SizeBytes || "sha256:"+hex.EncodeToString(digest[:]) != expected.Digest {
		return empty, errWorkspaceObjectUnconfirmed
	}
	manifest, err := runtimecontract.DecodeWorkspaceArchiveManifest(raw)
	if err != nil || manifest.Scope != checkpoint.Scope || manifest.WorkspaceRef != checkpoint.WorkspaceRef ||
		manifest.Generation != checkpoint.Generation || manifest.CheckpointRef != checkpoint.CheckpointRef ||
		manifest.TreeSHA256 != checkpoint.TreeSHA256 || manifest.Bytes != checkpoint.Bytes || manifest.Entries != checkpoint.Entries {
		return empty, errWorkspaceObjectUnconfirmed
	}
	return manifest, nil
}

// validWorkspaceArchiveHeader admits only the canonical writer's closed regular/directory metadata and optional PAX path.
// validWorkspaceArchiveHeader допускает только closed regular/directory metadata canonical writer и optional PAX path.
func validWorkspaceArchiveHeader(header *tar.Header) bool {
	if header == nil || header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir ||
		header.Mode < 0 || header.Mode > 0o777 || header.Size < 0 || header.Uid != 0 || header.Gid != 0 ||
		(header.Uname != workspaceStreamRoleOwner && header.Uname != workspaceStreamProviderOwner) || header.Gname != "" ||
		header.Linkname != "" || header.Devmajor != 0 || header.Devminor != 0 || len(header.Xattrs) != 0 ||
		!header.ModTime.Equal(time.Unix(0, 0)) || !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
		return false
	}
	for key, value := range header.PAXRecords {
		if key != "path" || value != header.Name {
			return false
		}
	}
	name := header.Name
	if header.Typeflag == tar.TypeDir {
		if !strings.HasSuffix(name, "/") || header.Size != 0 {
			return false
		}
		name = strings.TrimSuffix(name, "/")
	}
	return workspaceStreamPath(name)
}
