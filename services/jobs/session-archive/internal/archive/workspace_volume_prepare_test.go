package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// workspaceVolumeFixture creates an empty shared-mode stand-in inside a private temporary parent, not a Kubernetes volume or owner grant.
// workspaceVolumeFixture создаёт empty shared-mode stand-in внутри private temporary parent, не Kubernetes volume или owner grant.
func workspaceVolumeFixture(t *testing.T) (string, int, securefile.OwnedRecordIdentity, runtimecontract.WorkspaceVolumePreparation) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "volume")
	if os.Mkdir(root, 0o700) != nil || unix.Chmod(root, 0o2777) != nil {
		t.Fatal("prepare private volume stand-in")
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open private volume stand-in")
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return root, fd, securefile.OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())},
		runtimecontract.WorkspaceVolumePreparation{Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000",
			SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}, WorkspaceRef: "wks_fixture000", TaskRef: "svt_fixture000",
			PVCUID: "10000000-0000-4000-8000-000000000001", VolumeGeneration: 1}
}

// workspaceVolumeState records inode, mode, ownership, size and write timestamps; reads may change atime, which is intentionally excluded.
// workspaceVolumeState фиксирует inode/mode/ownership/size/write timestamps; reads могут менять atime, намеренно исключённый из сравнения.
func workspaceVolumeState(t *testing.T, path string) unix.Stat_t {
	t.Helper()
	var value unix.Stat_t
	if unix.Lstat(path, &value) != nil {
		t.Fatal("observe retained volume state")
	}
	value.Atim = unix.Timespec{}
	return value
}

// TestWorkspaceVolumePreparationSealsEmptyRootAndRejoinsReadOnly preserves sealed metadata and subsequent private business data on rejoin.
// This is a local filesystem test; actual read-only mount enforcement and fixed root/worker UIDs need the native contour.
// TestWorkspaceVolumePreparationSealsEmptyRootAndRejoinsReadOnly сохраняет sealed metadata и поздние private business data при rejoin.
// Это local filesystem test; actual read-only mount enforcement и fixed root/worker UIDs проверяются native-контуром.
func TestWorkspaceVolumePreparationSealsEmptyRootAndRejoinsReadOnly(t *testing.T) {
	root, fd, owner, intent := workspaceVolumeFixture(t)
	ready, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, owner.UID, false)
	if err != nil || ready.Preparation != intent || ready.Schema != runtimecontract.WorkspaceVolumeReadySchema {
		t.Fatal("new empty root did not produce a bound ready record")
	}
	metadata := filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory)
	marker := filepath.Join(metadata, runtimecontract.WorkspaceVolumeReadyFile)
	parent, record := workspaceVolumeState(t, metadata), workspaceVolumeState(t, marker)
	if parent.Mode&0o7777 != 0o700 || parent.Uid != owner.UID || record.Mode&0o7777 != 0o440 || record.Uid != owner.UID || record.Gid != owner.GID {
		t.Fatal("volume observation was not sealed to the trusted helper")
	}
	private := filepath.Join(root, "private")
	if os.Mkdir(private, 0o700) != nil || unix.Chmod(private, 0o700) != nil || os.WriteFile(filepath.Join(private, "draft"), []byte("retained work"), 0o600) != nil {
		t.Fatal("prepare subsequent private data")
	}
	before := workspaceVolumeState(t, private)
	file := workspaceVolumeState(t, filepath.Join(private, "draft"))
	rejoined, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, owner.UID, true)
	if err != nil || rejoined != ready || parent != workspaceVolumeState(t, metadata) || record != workspaceVolumeState(t, marker) ||
		before != workspaceVolumeState(t, private) || file != workspaceVolumeState(t, filepath.Join(private, "draft")) {
		t.Fatal("read-only reconciliation changed private data or replaced its ready record")
	}
	if _, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, owner.UID, false); err == nil || record != workspaceVolumeState(t, marker) {
		t.Fatal("a nonempty root acquired first-attempt initialization credit")
	}
}

// TestWorkspaceVolumePreparationNeverAdoptsUnknownEmptyOrPartialState rejects unknown contents and incomplete metadata without repair or cleanup.
// TestWorkspaceVolumePreparationNeverAdoptsUnknownEmptyOrPartialState отклоняет unknown contents/incomplete metadata без repair или cleanup.
func TestWorkspaceVolumePreparationNeverAdoptsUnknownEmptyOrPartialState(t *testing.T) {
	for _, fault := range []string{"missing-record", "unknown-data", "partial-metadata", "linked-metadata", "root-mode"} {
		for _, reconcile := range []bool{false, true} {
			if fault == "missing-record" && !reconcile {
				continue
			}
			t.Run(fault+"/"+map[bool]string{true: "reconcile", false: "first"}[reconcile], func(t *testing.T) {
				root, fd, owner, intent := workspaceVolumeFixture(t)
				observed := root
				metadata := filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory)
				switch fault {
				case "missing-record":
				case "unknown-data":
					observed = filepath.Join(root, "unknown")
					if os.WriteFile(observed, []byte("retain"), 0o600) != nil {
						t.Fatal("prepare unknown retained data")
					}
				case "partial-metadata":
					observed = metadata
					if os.Mkdir(metadata, 0o700) != nil {
						t.Fatal("prepare incomplete metadata")
					}
				case "linked-metadata":
					observed = metadata
					if os.Symlink(t.TempDir(), metadata) != nil {
						t.Fatal("prepare foreign metadata alias")
					}
				case "root-mode":
					if unix.Chmod(root, 0o777) != nil {
						t.Fatal("prepare unqualified root mode")
					}
				}
				before := workspaceVolumeState(t, observed)
				if _, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, owner.UID, reconcile); err == nil || before != workspaceVolumeState(t, observed) {
					t.Fatal("unconfirmed volume was adopted, rewritten or removed")
				}
			})
		}
	}
}

// TestWorkspaceVolumeRejoinRejectsChangedPinsAndUnsealedMetadata keeps unrelated volume incarnations, task identities and damaged records untrusted.
// TestWorkspaceVolumeRejoinRejectsChangedPinsAndUnsealedMetadata сохраняет недоверие к unrelated volume incarnations/task identities/damaged records.
func TestWorkspaceVolumeRejoinRejectsChangedPinsAndUnsealedMetadata(t *testing.T) {
	for _, fault := range []string{"scope", "work", "task", "pvc", "generation", "marker-mode", "marker-partial", "marker-hardlink", "metadata-mode", "metadata-extra", "root-owner"} {
		t.Run(fault, func(t *testing.T) {
			root, fd, owner, intent := workspaceVolumeFixture(t)
			if _, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, owner.UID, false); err != nil {
				t.Fatal("prepare ready-volume countercase")
			}
			metadata := filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory)
			marker := filepath.Join(metadata, runtimecontract.WorkspaceVolumeReadyFile)
			rootOwner := owner.UID
			switch fault {
			case "scope":
				intent.Scope.ProjectRef = "prj_foreign000"
			case "work":
				intent.WorkspaceRef = "wks_foreign000"
			case "task":
				intent.TaskRef = "svt_foreign000"
			case "pvc":
				intent.PVCUID = "other-pvc"
			case "generation":
				intent.VolumeGeneration++
			case "marker-mode":
				if unix.Chmod(marker, 0o660) != nil {
					t.Fatal("prepare unsealed marker mode")
				}
			case "marker-partial":
				if unix.Chmod(marker, 0o600) != nil || os.WriteFile(marker, []byte("{"), 0o600) != nil || unix.Chmod(marker, 0o440) != nil {
					t.Fatal("prepare partial retained marker")
				}
			case "marker-hardlink":
				if os.Link(marker, filepath.Join(metadata, "alias")) != nil {
					t.Fatal("prepare linked retained marker")
				}
			case "metadata-mode":
				if unix.Chmod(metadata, 0o770) != nil {
					t.Fatal("prepare unsealed metadata mode")
				}
			case "metadata-extra":
				if os.WriteFile(filepath.Join(metadata, "unknown"), []byte("retain"), 0o600) != nil {
					t.Fatal("prepare unrelated retained metadata")
				}
			case "root-owner":
				rootOwner++
			}
			before, parent := workspaceVolumeState(t, marker), workspaceVolumeState(t, metadata)
			if _, err := prepareWorkspaceVolumeAt(t.Context(), fd, intent, owner, rootOwner, true); err == nil ||
				before != workspaceVolumeState(t, marker) || parent != workspaceVolumeState(t, metadata) {
				t.Fatal("changed volume pins or metadata were accepted or repaired")
			}
		})
	}
}

// TestWorkspaceVolumePreparationRejectsCancelledOrUnboundInput creates nothing when context or intent is invalid and never closes the caller descriptor.
// TestWorkspaceVolumePreparationRejectsCancelledOrUnboundInput ничего не создаёт при invalid context/intent и не закрывает caller descriptor.
func TestWorkspaceVolumePreparationRejectsCancelledOrUnboundInput(t *testing.T) {
	for _, fault := range []string{"nil-context", "cancel", "missing-pvc", "wrong-helper"} {
		t.Run(fault, func(t *testing.T) {
			root, fd, owner, intent := workspaceVolumeFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var call context.Context = ctx
			switch fault {
			case "nil-context":
				call = nil
			case "cancel":
				cancel()
			case "missing-pvc":
				intent.PVCUID = ""
			case "wrong-helper":
				owner.UID++
			}
			if _, err := prepareWorkspaceVolumeAt(call, fd, intent, owner, uint32(os.Geteuid()), false); err == nil {
				t.Fatal("unbound volume preparation accepted")
			}
			names, err := os.ReadDir(root)
			var info unix.Stat_t
			if err != nil || len(names) != 0 || unix.Fstat(fd, &info) != nil {
				t.Fatal("rejected preparation wrote metadata or closed caller descriptor")
			}
		})
	}
}
