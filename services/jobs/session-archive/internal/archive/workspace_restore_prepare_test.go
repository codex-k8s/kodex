package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// workspaceRestoreVolumeFixture creates an authorized-root stand-in under the current unprivileged UID, not a real PVC.
// workspaceRestoreVolumeFixture создаёт authorized-root stand-in под текущим unprivileged UID, не настоящий PVC.
func workspaceRestoreVolumeFixture(t *testing.T) (string, int, securefile.OwnedRecordIdentity) {
	t.Helper()
	root := t.TempDir()
	if os.Chmod(root, 0o700) != nil {
		t.Fatal("prepare private restore volume fixture")
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open private restore volume fixture")
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return root, fd, securefile.OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
}

// TestWorkspaceRestorePreparationCreatesOnlyBoundGeneration proves exact private ancestors, empty stage and unchanged intent on init replay.
// TestWorkspaceRestorePreparationCreatesOnlyBoundGeneration проверяет exact private ancestors, empty stage и неизменность intent при init replay.
func TestWorkspaceRestorePreparationCreatesOnlyBoundGeneration(t *testing.T) {
	task := newWorkspaceRestorePublicationFixture(t).task
	root, fd, owner := workspaceRestoreVolumeFixture(t)
	if prepareWorkspaceRestoreGenerationAt(t.Context(), fd, task, "pvc-fixture", owner) != nil {
		t.Fatal("prepare exact selected generation")
	}
	subpath, _ := task.Workspace.Target.GenerationSubPath()
	parentPath := filepath.Join(root, filepath.Dir(subpath))
	var before, after unix.Stat_t
	intentPath := filepath.Join(parentPath, workspaceRestoreIntentFile)
	if unix.Stat(intentPath, &before) != nil || before.Mode&0o7777 != 0o440 || before.Uid != owner.UID || before.Gid != owner.GID {
		t.Fatal("intent was not sealed to trusted owner")
	}
	if prepareWorkspaceRestoreGenerationAt(t.Context(), fd, task, "pvc-fixture", owner) != nil || unix.Stat(intentPath, &after) != nil ||
		before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid ||
		before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		t.Fatal("matching init replay replaced or rewrote its intent")
	}
	for path := parentPath; path != root; path = filepath.Dir(path) {
		var info unix.Stat_t
		if unix.Stat(path, &info) != nil || info.Uid != owner.UID || info.Mode&0o7777 != 0o700 {
			t.Fatal("private ancestor boundary differs from selected generation")
		}
	}
	names, err := os.ReadDir(parentPath)
	stage := filepath.Join(parentPath, workspaceRestoreStagingTree)
	var info unix.Stat_t
	entries, readErr := os.ReadDir(stage)
	if err != nil || len(names) != 2 || readErr != nil || len(entries) != 0 || unix.Stat(stage, &info) != nil ||
		info.Mode&0o7777 != 0o2770 || info.Uid != owner.UID || info.Gid != owner.GID {
		t.Fatal("initialization created served data, extra metadata or a nonempty shared stage")
	}
}

// TestWorkspaceRestorePreparationResumesOnlyEmptyMatchingIntent completes the two pre-data crash windows without replacing sealed bytes.
// TestWorkspaceRestorePreparationResumesOnlyEmptyMatchingIntent завершает два pre-data crash windows без замены sealed bytes.
func TestWorkspaceRestorePreparationResumesOnlyEmptyMatchingIntent(t *testing.T) {
	for _, hasIntent := range []bool{false, true} {
		task := newWorkspaceRestorePublicationFixture(t).task
		root, fd, owner := workspaceRestoreVolumeFixture(t)
		subpath, _ := task.Workspace.Target.GenerationSubPath()
		parentPath := filepath.Join(root, filepath.Dir(subpath))
		if os.MkdirAll(parentPath, 0o700) != nil {
			t.Fatal("prepare empty generation left before intent creation")
		}
		parent, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal("open empty generation fixture")
		}
		if hasIntent {
			raw, err := workspaceRestoreIntentBytes(task, "pvc-fixture")
			if err != nil || securefile.WriteOwnedRecordAt(t.Context(), parent, workspaceRestoreIntentFile, raw, int64(len(raw)), owner) != nil {
				_ = unix.Close(parent)
				t.Fatal("prepare sealed intent left before stage creation")
			}
		}
		if unix.Close(parent) != nil || prepareWorkspaceRestoreGenerationAt(t.Context(), fd, task, "pvc-fixture", owner) != nil {
			t.Fatal("matching pre-data preparation did not resume")
		}
		entries, err := os.ReadDir(filepath.Join(parentPath, workspaceRestoreStagingTree))
		if err != nil || len(entries) != 0 {
			t.Fatal("resumed initialization did not leave an empty unpublished stage")
		}
	}
}

// TestWorkspaceRestorePreparationRejectsInputBeforeCreation denies unknown PVC/owner, cancellation and all reconciliation-only attempts.
// TestWorkspaceRestorePreparationRejectsInputBeforeCreation отклоняет unknown PVC/owner, cancellation и reconciliation-only attempts до create.
func TestWorkspaceRestorePreparationRejectsInputBeforeCreation(t *testing.T) {
	for _, fault := range []string{"wrong-pvc", "missing-pvc", "wrong-owner", "reconcile", "cancel", "nil-context"} {
		t.Run(fault, func(t *testing.T) {
			task := newWorkspaceRestorePublicationFixture(t).task
			root, fd, owner := workspaceRestoreVolumeFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var inputContext context.Context = ctx
			pvc := "pvc-fixture"
			switch fault {
			case "wrong-pvc":
				pvc = "other-pvc"
			case "missing-pvc":
				task.Workspace.PVCUID = ""
			case "wrong-owner":
				owner.UID++
			case "reconcile":
				task.Attempt, task.Workspace.ReconcileOnly = 2, true
			case "cancel":
				cancel()
			case "nil-context":
				inputContext = nil
			}
			if err := prepareWorkspaceRestoreGenerationAt(inputContext, fd, task, pvc, owner); err == nil || strings.Contains(err.Error(), root) {
				t.Fatal("unbound restore preparation was accepted or exposed a path")
			}
			names, err := os.ReadDir(root)
			var info unix.Stat_t
			if err != nil || len(names) != 0 || unix.Fstat(fd, &info) != nil {
				t.Fatal("denied preparation created state or closed caller root")
			}
		})
	}
}

// TestWorkspaceRestorePreparationRetainsUnknownState refuses partial work, mismatched intent and aliases without cleanup or chmod repair.
// TestWorkspaceRestorePreparationRetainsUnknownState отклоняет partial work, mismatched intent и aliases без cleanup/chmod repair.
func TestWorkspaceRestorePreparationRetainsUnknownState(t *testing.T) {
	for _, fault := range []string{"partial-tree", "partial-record", "missing-intent", "foreign-intent", "extra-metadata", "published", "stage-mode", "ancestor-mode", "linked-ancestor"} {
		t.Run(fault, func(t *testing.T) {
			task := newWorkspaceRestorePublicationFixture(t).task
			root, fd, owner := workspaceRestoreVolumeFixture(t)
			if prepareWorkspaceRestoreGenerationAt(t.Context(), fd, task, "pvc-fixture", owner) != nil {
				t.Fatal("prepare retained-state fixture")
			}
			subpath, _ := task.Workspace.Target.GenerationSubPath()
			parent := filepath.Join(root, filepath.Dir(subpath))
			observed := filepath.Join(parent, workspaceRestoreIntentFile)
			switch fault {
			case "partial-tree":
				observed = filepath.Join(parent, workspaceRestoreStagingTree, "unfinished")
				if os.WriteFile(observed, []byte("retain"), 0o600) != nil {
					t.Fatal("prepare partial work")
				}
			case "partial-record":
				if os.Chmod(observed, 0o600) != nil || os.WriteFile(observed, []byte("{"), 0o600) != nil {
					t.Fatal("prepare partial intent")
				}
			case "missing-intent":
				if os.Remove(observed) != nil {
					t.Fatal("prepare missing intent")
				}
				observed = filepath.Join(parent, workspaceRestoreStagingTree)
			case "foreign-intent":
				task.RuntimeRevisionVersion++
			case "extra-metadata":
				observed = filepath.Join(parent, "unexpected.json")
				if os.WriteFile(observed, []byte("retain"), 0o600) != nil {
					t.Fatal("prepare extra metadata")
				}
			case "published":
				observed = filepath.Join(parent, "tree")
				if os.Rename(filepath.Join(parent, workspaceRestoreStagingTree), observed) != nil {
					t.Fatal("prepare occupied served destination")
				}
			case "stage-mode":
				observed = filepath.Join(parent, workspaceRestoreStagingTree)
				if os.Chmod(observed, 0o755) != nil {
					t.Fatal("prepare changed stage mode")
				}
			case "ancestor-mode":
				observed = filepath.Dir(parent)
				if os.Chmod(observed, 0o755) != nil {
					t.Fatal("prepare changed ancestor mode")
				}
			case "linked-ancestor":
				observed = filepath.Join(root, "workspaces")
				if os.Rename(observed, filepath.Join(root, "retained")) != nil || os.Symlink("retained", observed) != nil {
					t.Fatal("prepare linked ancestor")
				}
			}
			before, err := os.Lstat(observed)
			if err != nil {
				t.Fatal("observe retained state")
			}
			if prepareWorkspaceRestoreGenerationAt(t.Context(), fd, task, "pvc-fixture", owner) == nil {
				t.Fatal("uncertain generation obtained initialization credit")
			}
			after, err := os.Lstat(observed)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatal("rejected state was erased, replaced or repaired")
			}
		})
	}
}
