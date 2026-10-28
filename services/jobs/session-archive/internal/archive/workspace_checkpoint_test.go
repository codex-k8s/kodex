package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// checkpointFromResultFixture binds the actual snapshot receipt to the known fixture tree, without granting restore authority.
// checkpointFromResultFixture связывает actual snapshot receipt с известным fixture tree без restore authority.
func checkpointFromResultFixture(task model.Task, result model.Result, entries int64) runtimecontract.WorkspaceCheckpointBinding {
	return runtimecontract.WorkspaceCheckpointBinding{Scope: task.Workspace.Target.Scope, WorkspaceRef: task.Workspace.Target.WorkspaceRef,
		Generation: task.Workspace.Target.Generation, CheckpointRef: task.Workspace.CheckpointRef, TreeSHA256: result.SourceSHA256,
		Bytes: result.SourceSizeBytes, Entries: entries, Manifest: runtimecontract.WorkspaceObjectCommitment{
			Key: result.ObjectKey, Version: result.ObjectVersion, ETag: result.ObjectETag, Digest: result.ObjectDigest, SizeBytes: result.ObjectSizeBytes}}
}

// TestWorkspaceCheckpointReadsExactSnapshotWithoutWrites round-trips the real publication path through strict root/part/TAR verification.
// TestWorkspaceCheckpointReadsExactSnapshotWithoutWrites проверяет настоящий publication path через strict root/part/TAR verification.
func TestWorkspaceCheckpointReadsExactSnapshotWithoutWrites(t *testing.T) {
	task, encode := workspaceSnapshotFixture(t)
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	result, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := checkpointFromResultFixture(task, result, 1)
	manifest, err := VerifyWorkspaceCheckpoint(t.Context(), store, checkpoint)
	if err != nil || manifest.Bytes != 12 || manifest.Entries != 1 || manifest.TreeSHA256 != result.SourceSHA256 || store.puts != 2 || store.deletes != 0 {
		t.Fatal("exact checkpoint could not be verified read-only")
	}
	checkpoint.Entries++
	if _, err := VerifyWorkspaceCheckpoint(t.Context(), store, checkpoint); err == nil {
		t.Fatal("foreign aggregate pin accepted")
	}
}

// workspacePartFaultStore mutates only part reads after a complete valid publication, retaining exact operation counters.
// workspacePartFaultStore изменяет только part reads после valid publication, сохраняя exact operation counters.
type workspacePartFaultStore struct {
	objectstorage.Store
	fault         string
	reads, closes int
}

// Get injects bounded body or metadata faults only at the selected data-part boundary.
// Get вводит bounded body/metadata faults только на выбранной data-part boundary.
func (store *workspacePartFaultStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err != nil || !strings.Contains(key, "/parts/") {
		return object, err
	}
	store.reads++
	if store.fault == "missing" {
		_ = object.Body.Close()
		return objectstorage.Object{}, objectstorage.ErrNotFound
	}
	if store.fault == "version" {
		object.VersionID += "foreign"
	}
	if store.fault == "size" {
		object.SizeBytes++
	}
	if store.fault == "corrupt" || store.fault == "short" || store.fault == "extra" {
		raw, readErr := io.ReadAll(object.Body)
		_ = object.Body.Close()
		if readErr != nil {
			return objectstorage.Object{}, readErr
		}
		switch store.fault {
		case "corrupt":
			raw[512] ^= 1
		case "short":
			raw = raw[:len(raw)-1]
		case "extra":
			raw = append(raw, 0)
		}
		object.Body = io.NopCloser(bytes.NewReader(raw))
	}
	object.Body = &workspacePartCloseFixture{ReadCloser: object.Body, store: store}
	return object, nil
}

// workspacePartCloseFixture counts closes even when malformed content aborts TAR decoding early.
// workspacePartCloseFixture считает закрытия даже при раннем отказе TAR decode на malformed content.
type workspacePartCloseFixture struct {
	io.ReadCloser
	store *workspacePartFaultStore
}

// Close records release of the actual response before delegating to the fixture body.
// Close фиксирует освобождение actual response до закрытия fixture body.
func (body *workspacePartCloseFixture) Close() error {
	body.store.closes++
	return body.ReadCloser.Close()
}

// TestWorkspaceCheckpointRejectsMissingChangedAndUnclosedParts exercises failures without PUT/DELETE compensation.
// TestWorkspaceCheckpointRejectsMissingChangedAndUnclosedParts проверяет отказы без PUT/DELETE compensation.
func TestWorkspaceCheckpointRejectsMissingChangedAndUnclosedParts(t *testing.T) {
	for _, fault := range []string{"missing", "version", "size", "corrupt", "short", "extra"} {
		t.Run(fault, func(t *testing.T) {
			task, encode := workspaceSnapshotFixture(t)
			origin := &workspaceObjectFixture{Store: objectstoragetest.New()}
			result, err := snapshotWorkspace(t.Context(), origin, task, encode)
			if err != nil {
				t.Fatal(err)
			}
			store := &workspacePartFaultStore{Store: origin, fault: fault}
			if _, err := VerifyWorkspaceCheckpoint(t.Context(), store, checkpointFromResultFixture(task, result, 1)); err == nil {
				t.Fatal("unconfirmed part accepted")
			}
			if store.reads != 1 || fault != "missing" && store.closes != 1 || origin.puts != 2 || origin.deletes != 0 {
				t.Fatal("failed verification leaked body or changed custody")
			}
		})
	}
}

// TestWorkspaceCheckpointRejectsNoncanonicalArchiveWithValidObjectHashes proves content policy independently of provider digests.
// TestWorkspaceCheckpointRejectsNoncanonicalArchiveWithValidObjectHashes проверяет content policy независимо от provider digests.
func TestWorkspaceCheckpointRejectsNoncanonicalArchiveWithValidObjectHashes(t *testing.T) {
	for _, fault := range []string{"mtime", "owner", "traversal", "mode", "link", "trailing"} {
		t.Run(fault, func(t *testing.T) {
			task, _ := workspaceSnapshotFixture(t)
			store := objectstoragetest.New()
			header := &tar.Header{Name: "source.go", Mode: 0o600, Size: 12, Typeflag: tar.TypeReg, Uname: workspaceStreamRoleOwner, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
			switch fault {
			case "mtime":
				header.ModTime = time.Unix(1, 0)
			case "owner":
				header.Uname = "root"
			case "traversal":
				header.Name = "../escape"
			case "mode":
				header.Mode = 0o4600
			case "link":
				header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, "escape", 0
			}
			var buffer bytes.Buffer
			writer := tar.NewWriter(&buffer)
			if writer.WriteHeader(header) != nil {
				t.Fatal("fixture header rejected")
			}
			if header.Size > 0 {
				if _, err := io.WriteString(writer, "package main"); err != nil {
					t.Fatal(err)
				}
			}
			if writer.Close() != nil {
				t.Fatal("fixture close failed")
			}
			if fault == "trailing" {
				buffer.Write(make([]byte, 512))
			}
			manifest, err := task.Workspace.SnapshotIdentity()
			if err != nil {
				t.Fatal(err)
			}
			key, err := manifest.PartObjectKey(1)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := writeWorkspaceObject(t.Context(), store, key, "application/x-tar", buffer.Bytes(), runtimecontract.MaximumWorkspaceArchivePartBytes, true)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(buffer.Bytes())
			manifest.TreeSHA256 = hex.EncodeToString(digest[:])
			manifest.Bytes, manifest.Entries, manifest.StreamBytes = header.Size, 1, int64(buffer.Len())
			manifest.Parts = []runtimecontract.WorkspaceArchivePart{{Index: 1, Version: receipt.Version, ETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: receipt.SizeBytes}}
			raw, err := runtimecontract.EncodeWorkspaceArchiveManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			root, err := writeWorkspaceObject(t.Context(), store, task.TargetObjectKey, "application/json", raw, runtimecontract.MaximumWorkspaceManifestBytes, true)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := runtimecontract.WorkspaceCheckpointBinding{Scope: manifest.Scope, WorkspaceRef: manifest.WorkspaceRef, Generation: manifest.Generation,
				CheckpointRef: manifest.CheckpointRef, Manifest: root, TreeSHA256: manifest.TreeSHA256, Bytes: manifest.Bytes, Entries: manifest.Entries}
			if _, err := VerifyWorkspaceCheckpoint(t.Context(), store, checkpoint); err == nil {
				t.Fatal("correct object hashes legitimized unsafe archive")
			}
		})
	}
}

// workspaceZeroReader generates bounded fixture data through an outer LimitReader without a file-sized allocation.
// workspaceZeroReader генерирует bounded fixture data через внешний LimitReader без file-sized allocation.
type workspaceZeroReader struct{}

// Read fills the supplied buffer; the fixture's explicit LimitReader owns the finite length.
// Read заполняет supplied buffer; конечной длиной владеет явный LimitReader оснастки.
func (workspaceZeroReader) Read(raw []byte) (int, error) { clear(raw); return len(raw), nil }

// TestWorkspaceCheckpointCrossesRealPartBoundary verifies actual 64MiB splitting and exact sequential readback.
// TestWorkspaceCheckpointCrossesRealPartBoundary проверяет настоящее 64MiB splitting и exact sequential readback.
func TestWorkspaceCheckpointCrossesRealPartBoundary(t *testing.T) {
	task, _ := workspaceSnapshotFixture(t)
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	encode := func(ctx context.Context, destination io.Writer, task model.Task) (string, int64, int64, int64, error) {
		encoder, err := newWorkspaceStreamEncoder(ctx, destination, task.Workspace.Policy)
		if err != nil {
			return "", 0, 0, 0, err
		}
		size := runtimecontract.MaximumWorkspaceArchivePartBytes
		if err := encoder.appendOwnedEntry("data.bin", false, 0o600, size, io.LimitReader(workspaceZeroReader{}, size), workspaceStreamProviderOwner); err != nil {
			return "", 0, 0, 0, err
		}
		return encoder.finish()
	}
	result, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := VerifyWorkspaceCheckpoint(t.Context(), store, checkpointFromResultFixture(task, result, 1))
	if err != nil || len(manifest.Parts) != 2 || manifest.Parts[0].SizeBytes != runtimecontract.MaximumWorkspaceArchivePartBytes || store.puts != 3 || store.deletes != 0 {
		t.Fatal("actual multipart boundary lost canonical source bytes")
	}
}
