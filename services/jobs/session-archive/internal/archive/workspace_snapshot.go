package archive

import (
	"context"
	"io"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// workspaceSnapshotEncoder emits the complete fenced tree without selecting an object key or publication policy.
// workspaceSnapshotEncoder выдаёт полное ограждённое дерево без выбора object key или политики публикации.
type workspaceSnapshotEncoder func(context.Context, io.Writer, model.Task) (string, int64, int64, int64, error)

// workspacePartsWriter retains at most one fixed-size part and exact confirmed receipts for its predecessors.
// Failure poisons the writer; later calls cannot repeat an uncertain PUT or skip a missing part.
// workspacePartsWriter хранит не более одной части фиксированного размера и точные квитанции предыдущих.
// Отказ закрывает writer: последующие вызовы не повторяют неизвестный PUT и не пропускают часть.
type workspacePartsWriter struct {
	ctx        context.Context
	store      objectstorage.Store
	manifest   runtimecontract.WorkspaceArchiveManifest
	buffer     []byte
	total      int64
	maximum    int64
	allowWrite bool
	failed     bool
	closed     bool
}

// SnapshotWorkspace publishes data parts before the root manifest and verifies every immutable object version.
// It never deletes uncertain objects, changes source permissions or promotes work to a CLEAN artifact.
// SnapshotWorkspace публикует части до корневого manifest и проверяет каждую неизменяемую версию объекта.
// Unknown objects не удаляются, права исходника не меняются, work не становится CLEAN artifact.
func SnapshotWorkspace(ctx context.Context, store objectstorage.Store, task model.Task) (model.Result, error) {
	return snapshotWorkspace(ctx, store, task, EncodeWorkspaceSnapshot)
}

// snapshotWorkspace separates the fixed source reader from publication so fault fixtures exercise the real writer.
// A retry has reconciliation-only credit even when an exact object currently reports NotFound.
// snapshotWorkspace отделяет fixed reader от публикации для проверки настоящего writer при сбоях.
// Повтор имеет только reconciliation credit, даже если точный объект сейчас возвращает NotFound.
func snapshotWorkspace(ctx context.Context, store objectstorage.Store, task model.Task, encode workspaceSnapshotEncoder) (model.Result, error) {
	if ctx == nil || ctx.Err() != nil || store == nil || encode == nil || task.Validate() != nil || task.Workspace == nil || task.Kind != "SNAPSHOT" {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	identity, err := task.Workspace.SnapshotIdentity()
	maximum, boundErr := runtimecontract.MaximumWorkspaceStreamBytes(task.Workspace.Policy.MaximumWritableBytes, task.Workspace.Policy.MaximumFileCount)
	if err != nil || boundErr != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	writer := &workspacePartsWriter{ctx: ctx, store: store, manifest: identity, maximum: maximum,
		allowWrite: !task.Workspace.ReconcileOnly, buffer: make([]byte, 0, runtimecontract.MaximumWorkspaceArchivePartBytes)}
	defer clear(writer.buffer[:cap(writer.buffer)])
	digest, size, entries, streamBytes, err := encode(ctx, writer, task)
	if err != nil || writer.failed || ctx.Err() != nil || streamBytes != writer.total ||
		size < 0 || size > task.Workspace.Policy.MaximumWritableBytes || entries < 0 || entries > task.Workspace.Policy.MaximumFileCount {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	if len(writer.buffer) > 0 && writer.flush() != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	writer.closed = true
	writer.manifest.TreeSHA256, writer.manifest.Bytes, writer.manifest.Entries, writer.manifest.StreamBytes = digest, size, entries, streamBytes
	raw, err := runtimecontract.EncodeWorkspaceArchiveManifest(writer.manifest)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer clear(raw)
	receipt, err := writeWorkspaceObject(ctx, store, task.TargetObjectKey, "application/json", raw,
		runtimecontract.MaximumWorkspaceManifestBytes, writer.allowWrite)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	return model.Result{Success: true, FormatVersion: 2, ObjectKey: receipt.Key, ObjectVersion: receipt.Version,
		ObjectETag: receipt.ETag, ObjectDigest: receipt.Digest, ObjectSizeBytes: receipt.SizeBytes,
		SourceSHA256: digest, SourceSizeBytes: size}, nil
}

// Write splits incoming canonical bytes without retaining a second part or acknowledging an unconfirmed flush.
// Write делит canonical bytes без второго part buffer и без подтверждения неизвестного flush.
func (writer *workspacePartsWriter) Write(raw []byte) (int, error) {
	if writer == nil || writer.failed || writer.closed || writer.ctx.Err() != nil || int64(len(raw)) > writer.maximum-writer.total {
		if writer != nil {
			writer.failed = true
		}
		return 0, errWorkspaceObjectUnconfirmed
	}
	written := 0
	for len(raw) > 0 {
		count := min(len(raw), cap(writer.buffer)-len(writer.buffer))
		writer.buffer = append(writer.buffer, raw[:count]...)
		writer.total += int64(count)
		written += count
		raw = raw[count:]
		if len(writer.buffer) == cap(writer.buffer) && writer.flush() != nil {
			return written, errWorkspaceObjectUnconfirmed
		}
	}
	return written, nil
}

// flush confirms one derived part version exactly once before reusing its bounded buffer.
// No root receipt can be published while a part's custody is unknown.
// flush подтверждает одну derived part version ровно один раз до повторного использования bounded buffer.
// Root receipt не публикуется, пока custody части неизвестна.
func (writer *workspacePartsWriter) flush() error {
	if writer.failed || writer.closed || len(writer.buffer) == 0 || writer.ctx.Err() != nil {
		writer.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	index := int32(len(writer.manifest.Parts) + 1)
	key, err := writer.manifest.PartObjectKey(index)
	if err != nil {
		writer.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	receipt, err := writeWorkspaceObject(writer.ctx, writer.store, key, "application/x-tar", writer.buffer,
		runtimecontract.MaximumWorkspaceArchivePartBytes, writer.allowWrite)
	if err != nil {
		writer.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	writer.manifest.Parts = append(writer.manifest.Parts, runtimecontract.WorkspaceArchivePart{Index: index,
		Version: receipt.Version, ETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: receipt.SizeBytes})
	clear(writer.buffer)
	writer.buffer = writer.buffer[:0]
	return nil
}
