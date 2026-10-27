package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// workspaceRestorePublicationFixture keeps real private records and data descriptors under the unprivileged test UID.
// It proves publication behavior only, not a production grant, distinct provider UID or Pod/PVC admission.
// workspaceRestorePublicationFixture хранит настоящие private records и data descriptors под непривилегированным тестовым UID.
// Она доказывает публикацию, но не production grant, отдельный provider UID или Pod/PVC admission.
type workspaceRestorePublicationFixture struct {
	root         string
	parent, tree int
	task         model.Task
	store        *workspaceObjectFixture
	files        *workspaceRestoreFilesystem
	publication  *workspaceRestorePublication
	owner        securefile.OwnedRecordIdentity
}

// newWorkspaceRestorePublicationFixture prepares a real checkpoint and a sealed intent without impersonating the owner service.
// newWorkspaceRestorePublicationFixture создаёт настоящий checkpoint и sealed intent без подмены owner service.
func newWorkspaceRestorePublicationFixture(t *testing.T, encoders ...workspaceSnapshotEncoder) workspaceRestorePublicationFixture {
	t.Helper()
	ctx := t.Context()
	task, encode := workspaceSnapshotFixture(t)
	if len(encoders) > 1 {
		t.Fatal("ambiguous checkpoint encoder fixture")
	}
	if len(encoders) == 1 {
		encode = encoders[0]
	}
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	result, err := snapshotWorkspace(ctx, store, task, encode)
	if err != nil {
		t.Fatal("prepare checkpoint fixture")
	}
	task = restoreTaskFromSnapshotFixture(t, task, result)
	root := t.TempDir()
	if unix.Chmod(root, 0o700) != nil || unix.Mkdir(filepath.Join(root, workspaceRestoreStagingTree), 0o2770) != nil ||
		unix.Chmod(filepath.Join(root, workspaceRestoreStagingTree), 0o2770) != nil {
		t.Fatal("prepare private generation fixture")
	}
	parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open generation fixture")
	}
	t.Cleanup(func() { _ = unix.Close(parent) })
	tree, err := unix.Openat(parent, workspaceRestoreStagingTree, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open unpublished tree fixture")
	}
	t.Cleanup(func() { _ = unix.Close(tree) })
	owner := securefile.OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	intent, err := workspaceRestoreIntentBytes(task, "pvc-fixture")
	if err != nil || securefile.WriteOwnedRecordAt(ctx, parent, workspaceRestoreIntentFile, intent,
		runtimecontract.MaximumWorkspaceManifestBytes, owner) != nil {
		t.Fatal("seal logical restore fixture")
	}
	publication, err := newWorkspaceRestorePublication(ctx, parent, task, "pvc-fixture", owner)
	if err != nil {
		t.Fatal("open actual generation publisher")
	}
	t.Cleanup(func() { _ = publication.Close() })
	files, err := newWorkspaceRestoreFilesystem(ctx, tree, task.Workspace.Policy,
		workspaceRestoreIdentity{actor: owner.UID, role: owner.UID, provider: owner.UID, group: owner.GID})
	if err != nil {
		t.Fatal("open real restore filesystem")
	}
	t.Cleanup(func() { _ = files.Close() })
	return workspaceRestorePublicationFixture{root: root, parent: parent, tree: tree, task: task, store: store,
		files: files, publication: publication, owner: owner}
}

// restore feeds actual verified object bytes through the filesystem and the real publisher, including writer closure checks.
// restore передаёт проверенные object bytes настоящим filesystem и publisher с проверкой закрытия writers.
func (fixture workspaceRestorePublicationFixture) restore(ctx context.Context) (model.Result, error) {
	return restoreWorkspaceStream(ctx, fixture.store, fixture.task, workspaceRestoreDestination{
		role: fixture.files, provider: fixture.files, commit: func(ctx context.Context) error {
			if fixture.files.openFiles != 0 || fixture.files.failed || fixture.files.Close() != nil {
				return errWorkspaceObjectUnconfirmed
			}
			return fixture.publication.Commit(ctx, fixture.tree)
		},
	})
}

// retry opens the same logical intent under a new reconciliation-only attempt without granting extraction credit.
// retry открывает прежний logical intent в новой reconciliation-only attempt без разрешения извлечения.
func (fixture workspaceRestorePublicationFixture) retry(t *testing.T) *workspaceRestorePublication {
	t.Helper()
	task := fixture.task
	body := *task.Workspace
	task.Workspace = &body
	task.Attempt, task.InputDigest, body.ReconcileOnly = 2, strings.Repeat("e", 64), true
	if fixture.publication.Close() != nil {
		t.Fatal("close original publisher")
	}
	publication, err := newWorkspaceRestorePublication(t.Context(), fixture.parent, task, "pvc-fixture", fixture.owner)
	if err != nil {
		t.Fatal("open reconciliation publisher")
	}
	t.Cleanup(func() { _ = publication.Close() })
	return publication
}

// TestWorkspaceRestorePublicationCommitsVerifiedTreeAndRejoins proves real source records, atomic name publication and no data rewrite on retry.
// TestWorkspaceRestorePublicationCommitsVerifiedTreeAndRejoins проверяет source records, атомарную публикацию и отсутствие повторной записи при retry.
func TestWorkspaceRestorePublicationCommitsVerifiedTreeAndRejoins(t *testing.T) {
	fixture := newWorkspaceRestorePublicationFixture(t)
	result, err := fixture.restore(t.Context())
	if err != nil || !result.Success || fixture.store.puts != 2 || fixture.store.deletes != 0 {
		t.Fatal("verified restore was not durably published without object writes")
	}
	data, err := os.ReadFile(filepath.Join(fixture.root, "tree", "source.go"))
	if err != nil || string(data) != "package main" {
		t.Fatal("published source bytes changed")
	}
	if _, err := os.Lstat(filepath.Join(fixture.root, workspaceRestoreStagingTree)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unpublished alias remained after commit")
	}
	wanted, _ := json.Marshal(fixture.task.Workspace.Checkpoint)
	actual, err := securefile.ReadOwnedRecordAt(t.Context(), fixture.parent, workspaceRestoreSourceFile,
		runtimecontract.MaximumWorkspaceManifestBytes, fixture.owner)
	if err != nil || !bytes.Equal(wanted, actual) {
		t.Fatal("runner source record does not match the exact checkpoint")
	}
	var before, after unix.Stat_t
	if unix.Fstatat(fixture.parent, "tree/source.go", &before, unix.AT_SYMLINK_NOFOLLOW) != nil {
		t.Fatal("observe published inode")
	}
	retry := fixture.retry(t)
	for range 2 {
		if retry.Rejoin(t.Context()) != nil || retry.Commit(t.Context(), fixture.tree) == nil {
			t.Fatal("retry could not rejoin or acquired new publication credit")
		}
	}
	if unix.Fstatat(fixture.parent, "tree/source.go", &after, unix.AT_SYMLINK_NOFOLLOW) != nil || before != after ||
		fixture.store.puts != 2 || fixture.store.deletes != 0 {
		t.Fatal("read-only rejoin changed file identity, bytes or object custody")
	}
}

// TestWorkspaceRestorePublicationCrashBoundaries never accepts an unpublished tree and rejoins a completed rename after lost acknowledgement.
// TestWorkspaceRestorePublicationCrashBoundaries не принимает unpublished tree и подтверждает завершённый rename после потери ACK.
func TestWorkspaceRestorePublicationCrashBoundaries(t *testing.T) {
	for _, stage := range []string{"SOURCE", "PREPARED", "RENAMED", "SYNCED"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			fixture.publication.after = func(actual string) error {
				if actual == stage {
					return errWorkspaceObjectUnconfirmed
				}
				return nil
			}
			if result, err := fixture.restore(t.Context()); err == nil || result.Success {
				t.Fatal("lost publication acknowledgement was reported as success")
			}
			retry := fixture.retry(t)
			published := stage == "RENAMED" || stage == "SYNCED"
			if (retry.Rejoin(t.Context()) == nil) != published || retry.Commit(t.Context(), fixture.tree) == nil {
				t.Fatal("reconciliation confused preparation with publication or repeated a rename")
			}
			name := workspaceRestoreStagingTree
			if published {
				name = "tree"
			}
			data, err := os.ReadFile(filepath.Join(fixture.root, name, "source.go"))
			if err != nil || string(data) != "package main" || fixture.store.puts != 2 || fixture.store.deletes != 0 {
				t.Fatal("uncertain restore lost evidence or changed checkpoint custody")
			}
		})
	}
}

// TestWorkspaceRestorePublicationNeverReplacesExistingTree preserves the occupant and the newly verified unpublished data.
// TestWorkspaceRestorePublicationNeverReplacesExistingTree сохраняет прежний каталог и новые проверенные unpublished data.
func TestWorkspaceRestorePublicationNeverReplacesExistingTree(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[occupied], func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			target := filepath.Join(fixture.root, "tree")
			if os.Mkdir(target, 0o700) != nil {
				t.Fatal("prepare occupied destination")
			}
			if occupied && os.WriteFile(filepath.Join(target, "existing"), []byte("retain"), 0o600) != nil {
				t.Fatal("prepare destination evidence")
			}
			before, err := os.Stat(target)
			if err != nil {
				t.Fatal("observe occupied destination")
			}
			if result, err := fixture.restore(t.Context()); err == nil || result.Success || fixture.publication.Rejoin(t.Context()) == nil {
				t.Fatal("existing destination was replaced or falsely accepted")
			}
			after, err := os.Stat(target)
			data, readErr := os.ReadFile(filepath.Join(fixture.root, workspaceRestoreStagingTree, "source.go"))
			if err != nil || !os.SameFile(before, after) || readErr != nil || string(data) != "package main" {
				t.Fatal("publication failure destroyed old or new data")
			}
		})
	}
}

// TestWorkspaceRestorePublicationRejectsChangedEvidence closes on changed metadata or inode while retaining all diagnostic state.
// TestWorkspaceRestorePublicationRejectsChangedEvidence закрыто отклоняет changed metadata/inode и сохраняет диагностическое состояние.
func TestWorkspaceRestorePublicationRejectsChangedEvidence(t *testing.T) {
	for _, change := range []string{"intent", "source", "prepared", "partial-record", "unknown-field", "duplicate-field", "tree-inode", "tree-symlink", "staging-alias", "parent-mode"} {
		t.Run(change, func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			if _, err := fixture.restore(t.Context()); err != nil {
				t.Fatal("prepare published generation")
			}
			name := workspaceRestorePreparedFile
			if change == "intent" {
				name = workspaceRestoreIntentFile
			} else if change == "source" {
				name = workspaceRestoreSourceFile
			}
			path := filepath.Join(fixture.root, name)
			switch change {
			case "intent", "source", "prepared", "partial-record", "unknown-field", "duplicate-field":
				raw, err := os.ReadFile(path)
				if err != nil || os.Chmod(path, 0o600) != nil {
					t.Fatal("prepare metadata drift")
				}
				switch change {
				case "unknown-field":
					raw = append([]byte(`{"unexpected":true,`), raw[1:]...)
				case "duplicate-field":
					raw = append([]byte(`{"Schema":"kodex.workspace-restore-prepared.v1",`), raw[1:]...)
				default:
					raw = []byte("{}")
				}
				if os.WriteFile(path, raw, 0o600) != nil || change != "partial-record" && os.Chmod(path, 0o440) != nil {
					t.Fatal("apply metadata drift")
				}
			case "tree-inode", "tree-symlink":
				if os.Rename(filepath.Join(fixture.root, "tree"), filepath.Join(fixture.root, "retained")) != nil {
					t.Fatal("retain published tree")
				}
				if change == "tree-symlink" {
					if os.Symlink("retained", filepath.Join(fixture.root, "tree")) != nil {
						t.Fatal("prepare linked replacement")
					}
				} else if unix.Mkdirat(fixture.parent, "tree", 0o2770) != nil || unix.Chmod(filepath.Join(fixture.root, "tree"), 0o2770) != nil {
					t.Fatal("prepare inode replacement")
				}
			case "staging-alias":
				if os.Symlink("tree", filepath.Join(fixture.root, workspaceRestoreStagingTree)) != nil {
					t.Fatal("prepare residual staging alias")
				}
			case "parent-mode":
				if os.Chmod(fixture.root, 0o750) != nil {
					t.Fatal("prepare private boundary drift")
				}
			}
			if fixture.publication.Rejoin(t.Context()) == nil {
				t.Fatal("changed publication evidence was accepted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("rejected metadata was removed")
			}
		})
	}
}

// TestWorkspaceRestorePublicationChecksIntentBeforeEffects rejects cancellation, swapped inputs and wrong source descriptors before metadata publication.
// TestWorkspaceRestorePublicationChecksIntentBeforeEffects отклоняет cancellation, swapped inputs и wrong descriptors до metadata publication.
func TestWorkspaceRestorePublicationChecksIntentBeforeEffects(t *testing.T) {
	for _, change := range []string{"intent", "cancel", "wrong-tree", "closed", "wrong-pvc", "wrong-owner", "wrong-input"} {
		t.Run(change, func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fd := fixture.tree
			switch change {
			case "intent":
				path := filepath.Join(fixture.root, workspaceRestoreIntentFile)
				if os.Chmod(path, 0o600) != nil || os.WriteFile(path, []byte("{}"), 0o600) != nil || os.Chmod(path, 0o440) != nil {
					t.Fatal("change logical intent")
				}
			case "cancel":
				cancel()
			case "wrong-tree":
				fd = fixture.parent
			case "closed":
				if fixture.publication.Close() != nil {
					t.Fatal("close publisher")
				}
			default:
				task, owner, pvc := fixture.task, fixture.owner, "pvc-fixture"
				if change == "wrong-pvc" {
					pvc = "other-pvc"
				} else if change == "wrong-owner" {
					owner.UID++
				} else {
					task.RuntimeRevisionVersion++
				}
				if opened, err := newWorkspaceRestorePublication(ctx, fixture.parent, task, pvc, owner); err == nil || opened != nil {
					t.Fatal("different restore input was allowed to consume intent")
				}
				return
			}
			if fixture.publication.Commit(ctx, fd) == nil {
				t.Fatal("invalid publication input produced success")
			}
			for _, name := range []string{"tree", workspaceRestoreSourceFile, workspaceRestorePreparedFile} {
				if _, err := os.Lstat(filepath.Join(fixture.root, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("rejected publication created metadata or a served tree")
				}
			}
		})
	}
}
