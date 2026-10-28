package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// errWorkspaceObjectUnconfirmed preserves uncertain custody without disclosing private keys or provider errors.
// errWorkspaceObjectUnconfirmed сохраняет неизвестный исход custody без раскрытия private keys и provider errors.
var errWorkspaceObjectUnconfirmed = errors.New("session workspace object custody is unconfirmed")

// writeWorkspaceObject submits at most one PUT and requires exact-version content readback before returning.
// An existing same-key object is reconciled read-only; uncertain PUT never triggers a replacement or deletion.
// The caller must derive the key from an owner-fenced checkpoint and provide initial-write authority.
// A recovery invocation sets allowInitialWrite=false even if current readback says NotFound.
// writeWorkspaceObject выполняет не более одного PUT и требует exact-version content readback до возврата.
// Существующий same-key object проверяется read-only; unknown PUT не вызывает замену или удаление.
// Caller выводит key из owner-fenced checkpoint и отдельно разрешает initial write.
// Recovery вызывает helper с allowInitialWrite=false даже при NotFound текущего readback.
func writeWorkspaceObject(ctx context.Context, store objectstorage.Store, key, mediaType string, raw []byte, maximum int64, allowInitialWrite bool) (runtimecontract.WorkspaceObjectCommitment, error) {
	var empty runtimecontract.WorkspaceObjectCommitment
	if ctx == nil || ctx.Err() != nil || store == nil || !objectstorage.ValidKey(key) || mediaType == "" ||
		maximum < 1 || maximum > runtimecontract.MaximumWorkspaceArchivePartBytes || len(raw) == 0 || int64(len(raw)) > maximum {
		return empty, errWorkspaceObjectUnconfirmed
	}
	sum := sha256.Sum256(raw)
	expectedDigest := "sha256:" + hex.EncodeToString(sum[:])
	current, err := store.Head(ctx, key, "")
	if err == nil {
		return verifyWorkspaceObjectReceipt(ctx, store, current, key, expectedDigest, int64(len(raw)), maximum)
	}
	if !errors.Is(err, objectstorage.ErrNotFound) || ctx.Err() != nil || !allowInitialWrite {
		return empty, errWorkspaceObjectUnconfirmed
	}
	receipt, writeErr := store.Put(ctx, objectstorage.PutInput{Key: key, MediaType: mediaType,
		Digest: expectedDigest, SizeBytes: int64(len(raw)), Body: bytes.NewReader(raw)})
	if writeErr != nil {
		// Даже ErrNotFound после неизвестного PUT не разрешает вторую запись или cleanup.
		receipt, err = store.Head(ctx, key, "")
		if err != nil {
			return empty, errWorkspaceObjectUnconfirmed
		}
	}
	return verifyWorkspaceObjectReceipt(ctx, store, receipt, key, expectedDigest, int64(len(raw)), maximum)
}

// verifyWorkspaceObjectReceipt binds all metadata and hashes bounded bytes fetched using the exact stored version.
// It neither buffers a second part nor promotes a missing or mismatched object to successful custody.
// verifyWorkspaceObjectReceipt связывает metadata и хеширует bounded bytes по точной stored version.
// Второй part buffer не создаётся; missing/mismatched object не становится успешной custody.
func verifyWorkspaceObjectReceipt(ctx context.Context, store objectstorage.Store, receipt objectstorage.Receipt, key, expectedDigest string, expectedSize, maximum int64) (runtimecontract.WorkspaceObjectCommitment, error) {
	value := runtimecontract.WorkspaceObjectCommitment{Key: receipt.Key, Version: receipt.VersionID,
		ETag: receipt.ETag, Digest: receipt.Digest, SizeBytes: receipt.SizeBytes}
	if ctx == nil || ctx.Err() != nil || store == nil || maximum < 1 || maximum > runtimecontract.MaximumWorkspaceArchivePartBytes ||
		value.Validate(maximum) != nil || value.Key != key || value.Digest != expectedDigest || value.SizeBytes != expectedSize {
		return runtimecontract.WorkspaceObjectCommitment{}, errWorkspaceObjectUnconfirmed
	}
	object, err := store.Get(ctx, value.Key, value.Version)
	if err != nil || object.Body == nil {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		return runtimecontract.WorkspaceObjectCommitment{}, errWorkspaceObjectUnconfirmed
	}
	hash := sha256.New()
	written, readErr := io.Copy(hash, io.LimitReader(&workspaceStreamReader{ctx: ctx, source: object.Body}, value.SizeBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil || ctx.Err() != nil || object.Receipt != receipt || written != value.SizeBytes ||
		"sha256:"+hex.EncodeToString(hash.Sum(nil)) != value.Digest {
		return runtimecontract.WorkspaceObjectCommitment{}, errWorkspaceObjectUnconfirmed
	}
	return value, nil
}
