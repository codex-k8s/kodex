package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// workspaceRestoreFiles confines one fixed process identity to its unpublished generation tree.
// Close must fsync and validate file metadata; a successful stream never implies owner acceptance.
// workspaceRestoreFiles ограничивает одну фиксированную идентичность неопубликованным деревом поколения.
// Close проверяет metadata и вызывает fsync; успешный поток сам по себе не является принятием владельцем.
type workspaceRestoreFiles interface {
	Directory(context.Context, string) error
	File(context.Context, string, uint32, int64) (io.WriteCloser, error)
	SealDirectory(context.Context, string, uint32) error
}

// workspaceRestoreDestination selects only ROLE/PROVIDER writers and publishes after complete verification.
// Its caller owns all descriptors and preserves partial trees on error; this interface grants no cleanup credit.
// workspaceRestoreDestination выбирает только ROLE/PROVIDER writer и публикует после полной проверки.
// Caller владеет descriptors и сохраняет частичное дерево при ошибке; интерфейс не разрешает очистку.
type workspaceRestoreDestination struct {
	role, provider workspaceRestoreFiles
	commit         func(context.Context) error
}

// workspaceRestoreDirectory retains only the current ancestor stack for postorder permission sealing.
// workspaceRestoreDirectory хранит только стек предков для установки итоговых прав в обратном порядке.
type workspaceRestoreDirectory struct {
	name, owner string
	mode        uint32
}

// files rejects every owner other than the two closed archive identities without interpreting a numeric UID.
// files отклоняет владельцев вне двух идентичностей архива, не интерпретируя числовой UID.
func (target workspaceRestoreDestination) files(owner string) workspaceRestoreFiles {
	switch owner {
	case workspaceStreamRoleOwner:
		return target.role
	case workspaceStreamProviderOwner:
		return target.provider
	default:
		return nil
	}
}

// restoreWorkspaceStream verifies all source parts before filesystem effects, then verifies them again while extracting.
// Only a fresh owner task can write. Reconciliation and filesystem publication have separate protected entrypoints.
// restoreWorkspaceStream проверяет все source parts до записи и повторно проверяет их во время извлечения.
// Запись доступна только новой owner task; reconciliation и публикация имеют отдельные защищённые entrypoints.
func restoreWorkspaceStream(ctx context.Context, store objectstorage.Store, task model.Task, target workspaceRestoreDestination) (model.Result, error) {
	if ctx == nil || ctx.Err() != nil || store == nil || task.Validate() != nil || task.Kind != "RESTORE" || task.Workspace == nil ||
		task.Workspace.ReconcileOnly || target.role == nil || target.provider == nil || target.commit == nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	checkpoint := task.Workspace.Checkpoint
	if checkpoint == nil || checkpoint.Bytes > task.Workspace.Policy.MaximumWritableBytes || checkpoint.Entries > task.Workspace.Policy.MaximumFileCount {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	manifest, err := VerifyWorkspaceCheckpoint(ctx, store, *checkpoint)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	reader := &workspacePartReader{ctx: ctx, store: store, manifest: manifest}
	defer reader.Close()
	digest := sha256.New()
	source := io.TeeReader(&workspaceStreamReader{ctx: ctx, source: reader}, digest)
	stream := tar.NewReader(source)
	encoder, err := newWorkspaceStreamEncoder(ctx, io.Discard, task.Workspace.Policy)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	var directories []workspaceRestoreDirectory
	seal := func() error {
		last := directories[len(directories)-1]
		files := target.files(last.owner)
		if files == nil || files.SealDirectory(ctx, last.name, last.mode) != nil {
			return errWorkspaceObjectUnconfirmed
		}
		directories = directories[:len(directories)-1]
		return nil
	}
	for {
		header, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || !validWorkspaceArchiveHeader(header) || ctx.Err() != nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
		name := strings.TrimSuffix(header.Name, "/")
		for len(directories) > 0 && !strings.HasPrefix(name, directories[len(directories)-1].name+"/") {
			if seal() != nil {
				return model.Result{}, errWorkspaceObjectUnconfirmed
			}
		}
		files := target.files(header.Uname)
		if files == nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
		if header.Typeflag == tar.TypeDir {
			if encoder.appendOwnedEntry(name, true, uint32(header.Mode), 0, nil, header.Uname) != nil || files.Directory(ctx, name) != nil {
				return model.Result{}, errWorkspaceObjectUnconfirmed
			}
			directories = append(directories, workspaceRestoreDirectory{name: name, owner: header.Uname, mode: uint32(header.Mode)})
			continue
		}
		file, err := files.File(ctx, name, uint32(header.Mode), header.Size)
		if err != nil || file == nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
		copyErr := encoder.appendOwnedEntry(name, false, uint32(header.Mode), header.Size, io.TeeReader(stream, file), header.Uname)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
	}
	var extra [1]byte
	count, readErr := source.Read(extra[:])
	canonical, size, entries, streamBytes, finishErr := encoder.finish()
	if count != 0 || !errors.Is(readErr, io.EOF) || finishErr != nil || reader.Close() != nil || ctx.Err() != nil ||
		canonical != manifest.TreeSHA256 || hex.EncodeToString(digest.Sum(nil)) != canonical || size != manifest.Bytes ||
		entries != manifest.Entries || streamBytes != manifest.StreamBytes {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	for len(directories) > 0 {
		if seal() != nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
	}
	if ctx.Err() != nil || target.commit(ctx) != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	return model.Result{Success: true, FormatVersion: 2, ObjectKey: checkpoint.Manifest.Key, ObjectVersion: checkpoint.Manifest.Version,
		ObjectETag: checkpoint.Manifest.ETag, ObjectDigest: checkpoint.Manifest.Digest, ObjectSizeBytes: checkpoint.Manifest.SizeBytes,
		SourceSHA256: canonical, SourceSizeBytes: size, SourceEntryCount: entries}, nil
}
