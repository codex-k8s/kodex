package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// restoreTaskFromSnapshotFixture creates only a typed data-copy fixture, not a production restore grant.
// restoreTaskFromSnapshotFixture создаёт только типизированную fixture копирования, не production restore grant.
func restoreTaskFromSnapshotFixture(t *testing.T, task model.Task, result model.Result) model.Task {
	t.Helper()
	checkpoint := checkpointFromResultFixture(task, result, result.SourceEntryCount)
	body := task.Workspace
	task.Kind = "RESTORE"
	task.Workspace = &model.WorkspaceArchiveTask{Target: body.Target, CheckpointRef: checkpoint.CheckpointRef,
		Checkpoint: &checkpoint, Policy: body.Policy}
	task.Workspace.Target.Generation++
	task.TargetObjectKey, task.TargetObjectVersion = checkpoint.Manifest.Key, checkpoint.Manifest.Version
	if task.Validate() != nil {
		t.Fatal("invalid restore data fixture")
	}
	return task
}

// workspaceRestoreFilesystemFixture uses a real unprivileged tree under the current UID for stream/fencing tests.
// It does not prove the production two-UID peer or Kubernetes mount boundary.
// workspaceRestoreFilesystemFixture использует настоящее непривилегированное дерево текущего UID для проверки stream/fencing.
// Она не доказывает production two-UID peer или границу Kubernetes mount.
func workspaceRestoreFilesystemFixture(t *testing.T, ctx context.Context) (string, *workspaceRestoreFilesystem, int) {
	t.Helper()
	root := t.TempDir()
	if unix.Chmod(root, 0o2770) != nil {
		t.Fatal("prepare restore fixture directory")
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open restore fixture directory")
	}
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	files, err := newWorkspaceRestoreFilesystem(ctx, fd, runtimecontract.RuntimeWorkspacePolicyV1(),
		workspaceRestoreIdentity{actor: uid, role: uid, provider: uid, group: gid})
	if err != nil {
		_ = unix.Close(fd)
		t.Fatal("construct restore filesystem fixture")
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil && !errors.Is(err, errWorkspaceObjectUnconfirmed) {
			t.Error("close restore fixture adapter")
		}
		if unix.Close(fd) != nil {
			t.Error("close restore fixture root")
		}
	})
	return root, files, fd
}

// workspaceRestoreTrackingFiles records logical routing while delegating all writes and fsync to real descriptors.
// workspaceRestoreTrackingFiles фиксирует logical routing и делегирует запись и fsync настоящим descriptors.
type workspaceRestoreTrackingFiles struct {
	workspaceRestoreFiles
	owner  string
	events *[]string
}

// Directory records which fixed logical identity creates the node before applying the real operation.
// Directory фиксирует logical identity создателя до настоящей операции.
func (files workspaceRestoreTrackingFiles) Directory(ctx context.Context, name string) error {
	*files.events = append(*files.events, "directory:"+files.owner+":"+name)
	return files.workspaceRestoreFiles.Directory(ctx, name)
}

// File records logical identity and leaves exclusive creation and bounded writes to the actual filesystem.
// File фиксирует logical identity; exclusive creation и bounded writes остаются настоящему filesystem.
func (files workspaceRestoreTrackingFiles) File(ctx context.Context, name string, mode uint32, size int64) (io.WriteCloser, error) {
	*files.events = append(*files.events, "file:"+files.owner+":"+name)
	return files.workspaceRestoreFiles.File(ctx, name, mode, size)
}

// SealDirectory records postorder permission restoration after all descendant writers have closed.
// SealDirectory фиксирует восстановление прав после закрытия всех descendant writers.
func (files workspaceRestoreTrackingFiles) SealDirectory(ctx context.Context, name string, mode uint32) error {
	*files.events = append(*files.events, "seal:"+files.owner+":"+name)
	return files.workspaceRestoreFiles.SealDirectory(ctx, name, mode)
}

// TestWorkspaceRestoreStreamRoutesOwnersAndPreservesModes exercises real canonical publication and real destination files.
// Logical owners share the current test UID; a separate native contour must prove distinct production UIDs.
// TestWorkspaceRestoreStreamRoutesOwnersAndPreservesModes проверяет настоящую canonical publication и файлы назначения.
// Logical owners используют текущий тестовый UID; разные production UID должен доказать отдельный native-контур.
func TestWorkspaceRestoreStreamRoutesOwnersAndPreservesModes(t *testing.T) {
	task, _ := workspaceSnapshotFixture(t)
	encode := func(ctx context.Context, destination io.Writer, task model.Task) (string, int64, int64, int64, error) {
		encoder, err := newWorkspaceStreamEncoder(ctx, destination, task.Workspace.Policy)
		if err != nil {
			return "", 0, 0, 0, err
		}
		for _, entry := range []struct {
			name, owner, content string
			mode                 uint32
			directory            bool
		}{
			{"bin", workspaceStreamRoleOwner, "", 0o750, true},
			{"bin/service", workspaceStreamRoleOwner, "service", 0o755, false},
			{"private", workspaceStreamProviderOwner, "", 0o700, true},
			{"private/state", workspaceStreamProviderOwner, "draft", 0o600, false},
			{"readme.md", workspaceStreamRoleOwner, "readme", 0o644, false},
		} {
			var contents io.Reader = strings.NewReader(entry.content)
			if entry.directory {
				contents = nil
			}
			if encoder.appendOwnedEntry(entry.name, entry.directory, entry.mode, int64(len(entry.content)), contents, entry.owner) != nil {
				return "", 0, 0, 0, errWorkspaceStreamInvalid
			}
		}
		return encoder.finish()
	}
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	snapshot, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil {
		t.Fatal("publish restore fixture")
	}
	task = restoreTaskFromSnapshotFixture(t, task, snapshot)
	root, files, fd := workspaceRestoreFilesystemFixture(t, t.Context())
	var events []string
	committed := false
	target := workspaceRestoreDestination{
		role:     workspaceRestoreTrackingFiles{files, workspaceStreamRoleOwner, &events},
		provider: workspaceRestoreTrackingFiles{files, workspaceStreamProviderOwner, &events},
		commit: func(ctx context.Context) error {
			if ctx.Err() != nil || files.openFiles != 0 || files.failed || unix.Fsync(fd) != nil {
				return errWorkspaceObjectUnconfirmed
			}
			committed = true
			return nil
		},
	}
	result, err := restoreWorkspaceStream(t.Context(), store, task, target)
	if err != nil || !result.Success || !committed || result.SourceSHA256 != snapshot.SourceSHA256 ||
		result.SourceEntryCount != 5 || store.puts != 2 || store.deletes != 0 {
		t.Fatal("verified stream was not copied without storage writes")
	}
	wanted := "directory:ROLE:bin,file:ROLE:bin/service,seal:ROLE:bin,directory:PROVIDER:private,file:PROVIDER:private/state,seal:PROVIDER:private,file:ROLE:readme.md"
	if strings.Join(events, ",") != wanted {
		t.Fatal("logical owner routing or directory sealing order changed")
	}
	for name, content := range map[string]string{"bin/service": "service", "private/state": "draft", "readme.md": "readme"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(raw) != content {
			t.Fatal("restored bytes changed")
		}
	}
	for name, mode := range map[string]os.FileMode{"bin": 0o750, "bin/service": 0o755, "private": 0o700, "private/state": 0o600, "readme.md": 0o644} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal("restored permission mode changed")
		}
	}
}

// workspaceRestoreSecondReadFault corrupts only the extraction read after the complete source preflight succeeded.
// workspaceRestoreSecondReadFault повреждает только extraction read после полной предварительной проверки source.
type workspaceRestoreSecondReadFault struct {
	objectstorage.Store
	reads int
}

// Get retains exact metadata while modifying one body byte on the second read of the same part version.
// Get сохраняет exact metadata и меняет один body byte при втором чтении того же part version.
func (store *workspaceRestoreSecondReadFault) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err != nil || !strings.Contains(key, "/parts/") {
		return object, err
	}
	store.reads++
	if store.reads == 2 {
		raw, readErr := io.ReadAll(object.Body)
		closeErr := object.Body.Close()
		if readErr != nil || closeErr != nil || len(raw) <= 512 {
			return objectstorage.Object{}, errWorkspaceObjectUnconfirmed
		}
		raw[512] ^= 1
		object.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return object, nil
}

// TestWorkspaceRestoreStreamRejectsCorruptionAndUnconfirmedCommit never publishes failed or partial extraction.
// TestWorkspaceRestoreStreamRejectsCorruptionAndUnconfirmedCommit не публикует неудачное либо частичное извлечение.
func TestWorkspaceRestoreStreamRejectsCorruptionAndUnconfirmedCommit(t *testing.T) {
	for _, fault := range []string{"missing", "version", "size", "corrupt", "short", "extra", "second-read", "commit"} {
		t.Run(fault, func(t *testing.T) {
			task, encode := workspaceSnapshotFixture(t)
			store := &workspaceObjectFixture{Store: objectstoragetest.New()}
			result, err := snapshotWorkspace(t.Context(), store, task, encode)
			if err != nil {
				t.Fatal("publish restore fault fixture")
			}
			task = restoreTaskFromSnapshotFixture(t, task, result)
			root, files, _ := workspaceRestoreFilesystemFixture(t, t.Context())
			var source objectstorage.Store = &workspacePartFaultStore{Store: store, fault: fault}
			if fault == "second-read" {
				source = &workspaceRestoreSecondReadFault{Store: store}
			}
			committed := false
			target := workspaceRestoreDestination{role: files, provider: files, commit: func(context.Context) error {
				if fault == "commit" {
					return errWorkspaceObjectUnconfirmed
				}
				committed = true
				return nil
			}}
			result, err = restoreWorkspaceStream(t.Context(), source, task, target)
			if err == nil || result.Success || committed || store.puts != 2 || store.deletes != 0 {
				t.Fatal("unconfirmed restore was accepted or compensated by a storage write")
			}
			entries, err := os.ReadDir(root)
			if err != nil || fault != "second-read" && fault != "commit" && len(entries) != 0 ||
				(fault == "second-read" || fault == "commit") && len(entries) != 1 {
				t.Fatal("preflight wrote data or failed extraction erased retained evidence")
			}
		})
	}
}

// TestWorkspaceRestoreStreamRejectsRetryAndSmallerQuota forbids write credit on reconciliation or over-limit sources.
// TestWorkspaceRestoreStreamRejectsRetryAndSmallerQuota запрещает запись при reconciliation и превышении квоты source.
func TestWorkspaceRestoreStreamRejectsRetryAndSmallerQuota(t *testing.T) {
	for _, fault := range []string{"retry", "quota", "missing-writer"} {
		t.Run(fault, func(t *testing.T) {
			task, encode := workspaceSnapshotFixture(t)
			store := &workspaceObjectFixture{Store: objectstoragetest.New()}
			result, err := snapshotWorkspace(t.Context(), store, task, encode)
			if err != nil {
				t.Fatal("publish restore boundary fixture")
			}
			task = restoreTaskFromSnapshotFixture(t, task, result)
			root, files, _ := workspaceRestoreFilesystemFixture(t, t.Context())
			target := workspaceRestoreDestination{role: files, provider: files, commit: func(context.Context) error { t.Fatal("unexpected commit"); return nil }}
			switch fault {
			case "retry":
				task.Attempt, task.Workspace.ReconcileOnly = 2, true
			case "quota":
				policy, err := runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 1, MaxFiles: 1})
				if err != nil {
					t.Fatal("construct lower quota")
				}
				task.Workspace.Policy = policy
			case "missing-writer":
				target.provider = nil
			}
			if task.Validate() != nil {
				t.Fatal("boundary fixture lost valid task shape")
			}
			if result, err := restoreWorkspaceStream(t.Context(), store, task, target); err == nil || result.Success {
				t.Fatal("restore write boundary was bypassed")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected restore wrote a file")
			}
		})
	}
}
