package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

const manifestName = "manifest.json"

type manifest struct {
	FormatVersion          uint32 `json:"format_version"`
	OrganizationRef        string `json:"organization_ref"`
	ProjectRef             string `json:"project_ref"`
	SessionRef             string `json:"session_ref"`
	ProviderAccountRef     string `json:"provider_account_ref"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	CodexSessionID         string `json:"codex_session_id"`
	ContentGeneration      int64  `json:"content_generation"`
	SourceRelativePath     string `json:"source_relative_path"`
	SourceSHA256           string `json:"source_sha256"`
	SourceSizeBytes        int64  `json:"source_size_bytes"`
}

func Snapshot(ctx context.Context, store objectstorage.Store, workspace string, task model.Task) (model.Result, error) {
	source, err := readSource(workspace, task.SourceRelativePath, task.SourceSHA256, task.SourceSizeBytes)
	if err != nil {
		return model.Result{}, err
	}
	archive, err := encode(task, source)
	if err != nil {
		return model.Result{}, err
	}
	archiveDigest := digest(archive)
	receipt, err := store.Put(ctx, objectstorage.PutInput{Key: task.TargetObjectKey, MediaType: "application/x-tar", Digest: archiveDigest, SizeBytes: int64(len(archive)), Body: bytes.NewReader(archive)})
	if err != nil {
		cleanupObject(ctx, store, task.TargetObjectKey, "")
		return model.Result{}, errors.New("put session archive object")
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanupObject(ctx, store, receipt.Key, receipt.VersionID)
	}()
	object, err := store.Get(ctx, receipt.Key, receipt.VersionID)
	if err != nil {
		return model.Result{}, errors.New("read back session archive object")
	}
	readback, readErr := io.ReadAll(io.LimitReader(object.Body, model.MaximumObjectBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil || int64(len(readback)) != receipt.SizeBytes || digest(readback) != receipt.Digest ||
		object.Key != receipt.Key || object.VersionID != receipt.VersionID || object.ETag != receipt.ETag {
		return model.Result{}, errors.New("session archive object readback mismatch")
	}
	if _, err := decode(task, readback); err != nil {
		return model.Result{}, err
	}
	committed = true
	return model.Result{Success: true, FormatVersion: model.FormatVersion, ObjectKey: receipt.Key,
		ObjectVersion: receipt.VersionID, ObjectETag: receipt.ETag, ObjectDigest: receipt.Digest,
		ObjectSizeBytes: receipt.SizeBytes, SourceSHA256: task.SourceSHA256, SourceSizeBytes: task.SourceSizeBytes}, nil
}

func cleanupObject(ctx context.Context, store objectstorage.Store, key, version string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = store.Delete(cleanup, key, version)
}

func Restore(ctx context.Context, store objectstorage.Store, workspace string, task model.Task) (model.Result, error) {
	binding := task.Archive
	object, err := store.Get(ctx, binding.ObjectKey, binding.ObjectVersion)
	if err != nil {
		return model.Result{}, errors.New("get session archive object")
	}
	raw, readErr := io.ReadAll(io.LimitReader(object.Body, model.MaximumObjectBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != binding.ObjectSizeBytes || digest(raw) != binding.ObjectDigest ||
		object.Key != binding.ObjectKey || object.VersionID != binding.ObjectVersion || object.ETag != binding.ObjectETag ||
		object.SizeBytes != binding.ObjectSizeBytes || object.Digest != binding.ObjectDigest {
		return model.Result{}, errors.New("session archive restore readback mismatch")
	}
	source, err := decode(task, raw)
	if err != nil {
		return model.Result{}, err
	}
	if err := writeSource(workspace, binding.SourceRelativePath, source); err != nil {
		return model.Result{}, err
	}
	verified, err := readSource(workspace, binding.SourceRelativePath, binding.SourceSHA256, binding.SourceSizeBytes)
	if err != nil || !bytes.Equal(verified, source) {
		return model.Result{}, errors.New("restored session source verification failed")
	}
	return model.Result{Success: true, FormatVersion: binding.FormatVersion, ObjectKey: binding.ObjectKey,
		ObjectVersion: binding.ObjectVersion, ObjectETag: binding.ObjectETag, ObjectDigest: binding.ObjectDigest,
		ObjectSizeBytes: binding.ObjectSizeBytes, SourceSHA256: binding.SourceSHA256, SourceSizeBytes: binding.SourceSizeBytes}, nil
}

func Delete(ctx context.Context, store objectstorage.Store, task model.Task) (model.Result, error) {
	if err := store.Delete(ctx, task.TargetObjectKey, task.TargetObjectVersion); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return model.Result{}, errors.New("delete session archive object")
	}
	if _, err := store.Head(ctx, task.TargetObjectKey, task.TargetObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		return model.Result{}, errors.New("session archive object deletion was not observed")
	}
	return model.Result{Success: true, ObjectKey: task.TargetObjectKey, ObjectVersion: task.TargetObjectVersion}, nil
}

func encode(task model.Task, source []byte) ([]byte, error) {
	m := manifest{model.FormatVersion, task.OrganizationRef, task.ProjectRef, task.SessionRef,
		task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionVersion,
		task.RuntimeRevisionDigest, task.CodexSessionID, task.ContentGeneration,
		task.SourceRelativePath, task.SourceSHA256, task.SourceSizeBytes}
	manifestRaw, err := json.Marshal(m)
	if err != nil {
		return nil, errors.New("encode session archive manifest")
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, item := range []struct {
		name string
		body []byte
	}{{manifestName, manifestRaw}, {task.SourceRelativePath, source}} {
		header := &tar.Header{Name: item.name, Mode: 0o600, Size: int64(len(item.body)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if writer.WriteHeader(header) != nil || writeExact(writer, item.body) != nil {
			return nil, errors.New("write session archive")
		}
	}
	if writer.Close() != nil || int64(buffer.Len()) > model.MaximumObjectBytes {
		return nil, errors.New("session archive exceeds the object budget")
	}
	return buffer.Bytes(), nil
}

func decode(task model.Task, raw []byte) ([]byte, error) {
	if int64(len(raw)) < 1 || int64(len(raw)) > model.MaximumObjectBytes {
		return nil, errors.New("session archive size is invalid")
	}
	reader := tar.NewReader(bytes.NewReader(raw))
	manifestHeader, err := reader.Next()
	if err != nil || manifestHeader.Name != manifestName || manifestHeader.Typeflag != tar.TypeReg || manifestHeader.Size > model.MaximumTaskBytes {
		return nil, errors.New("session archive manifest is invalid")
	}
	manifestRaw, err := io.ReadAll(io.LimitReader(reader, model.MaximumTaskBytes+1))
	if err != nil {
		return nil, errors.New("read session archive manifest")
	}
	var actual manifest
	if json.Unmarshal(manifestRaw, &actual) != nil || actual != expectedManifest(task) {
		return nil, errors.New("session archive manifest binding mismatch")
	}
	sourceHeader, err := reader.Next()
	if err != nil || sourceHeader.Name != actual.SourceRelativePath || sourceHeader.Typeflag != tar.TypeReg || sourceHeader.Size != actual.SourceSizeBytes {
		return nil, errors.New("session archive source entry is invalid")
	}
	source, err := io.ReadAll(io.LimitReader(reader, model.MaximumSourceBytes+1))
	if err != nil || int64(len(source)) != actual.SourceSizeBytes || digestPlain(source) != actual.SourceSHA256 {
		return nil, errors.New("session archive source digest mismatch")
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		return nil, errors.New("session archive contains unexpected entries")
	}
	return source, nil
}

func expectedManifest(task model.Task) manifest {
	path, sha, size := task.SourceRelativePath, task.SourceSHA256, task.SourceSizeBytes
	if task.Archive != nil {
		path, sha, size = task.Archive.SourceRelativePath, task.Archive.SourceSHA256, task.Archive.SourceSizeBytes
	}
	return manifest{model.FormatVersion, task.OrganizationRef, task.ProjectRef, task.SessionRef,
		task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionVersion,
		task.RuntimeRevisionDigest, task.CodexSessionID, task.ContentGeneration, path, sha, size}
}

func readSource(root, relative, expectedSHA string, expectedSize int64) ([]byte, error) {
	path, err := safePath(root, relative, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("session source file identity is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open session source file")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, model.MaximumSourceBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != expectedSize || digestPlain(raw) != expectedSHA {
		return nil, errors.New("session source file digest mismatch")
	}
	return raw, nil
}

func writeSource(root, relative string, raw []byte) error {
	path, err := safePath(root, relative, true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return errors.New("create session source directory")
	}
	if _, err := safePath(root, relative, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".restore-*")
	if err != nil {
		return errors.New("create restored session source")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if temporary.Chmod(0o640) != nil || writeExact(temporary, raw) != nil || temporary.Sync() != nil || temporary.Close() != nil || os.Rename(temporaryPath, path) != nil {
		return errors.New("commit restored session source")
	}
	return nil
}

func safePath(root, relative string, allowMissing bool) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || relative == "" || filepath.IsAbs(relative) ||
		filepath.ToSlash(filepath.Clean(relative)) != relative || strings.Contains(relative, "..") {
		return "", errors.New("session source path is unsafe")
	}
	current := root
	parts := strings.Split(relative, "/")
	for index, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if allowMissing && os.IsNotExist(err) {
				break
			}
			return "", fmt.Errorf("inspect session source component %d", index)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("session source path contains an unsafe component")
		}
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func digest(raw []byte) string      { return "sha256:" + digestPlain(raw) }
func digestPlain(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func writeExact(writer io.Writer, raw []byte) error {
	written, err := writer.Write(raw)
	if err != nil || written != len(raw) {
		return io.ErrShortWrite
	}
	return nil
}
