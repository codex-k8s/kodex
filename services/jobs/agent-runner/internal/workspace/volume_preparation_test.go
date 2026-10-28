package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// preparedVolumeReadFixture creates only a private sealed stand-in under the test user's IDs.
// It supplies no domain authority, Kubernetes object, production mount or model credential.
// preparedVolumeReadFixture создаёт только private sealed stand-in под test user IDs.
// Domain authority, Kubernetes object, production mount либо model credential не выдаются.
func preparedVolumeReadFixture(t *testing.T) (int, string, runtimecontract.WorkspaceVolumeReady, securefile.OwnedRecordIdentity) {
	t.Helper()
	name := t.TempDir()
	if err := os.Chmod(name, os.ModeSetgid|0o777); err != nil {
		t.Fatal(err)
	}
	root, err := unix.Open(name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(root) })
	var info unix.Stat_t
	if unix.Fstat(root, &info) != nil {
		t.Fatal("stat test volume")
	}
	owner := securefile.OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: info.Gid}
	ready := runtimecontract.WorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: uint64(info.Dev), Inode: info.Ino,
		Preparation: runtimecontract.WorkspaceVolumePreparation{Scope: runtimecontract.WorkspaceStorageScope{
			OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
			WorkspaceRef: "wks_fixture000", TaskRef: "svt_fixture000", PVCUID: "30000000-0000-4000-8000-000000000001", VolumeGeneration: 1}}
	metadata, _, err := securefile.OpenOwnedDirectoryAt(t.Context(), root, runtimecontract.WorkspaceVolumeMetadataDirectory, 0o700, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(metadata)
	raw, err := runtimecontract.EncodeWorkspaceVolumeReady(ready)
	if err != nil || securefile.WriteOwnedRecordAt(t.Context(), metadata, runtimecontract.WorkspaceVolumeReadyFile,
		raw, runtimecontract.MaximumWorkspaceVolumeReadyBytes, owner) != nil {
		t.Fatal("seal test volume")
	}
	return root, name, ready, owner
}

// TestPreparedVolumeReadNeverRepairsUntrustedMetadata checks sealed identity, nofollow reads and
// exact inode/record preservation on both successful rejoin and every rejected observation.
// TestPreparedVolumeReadNeverRepairsUntrustedMetadata проверяет sealed identity, nofollow reads
// и exact inode/record preservation при successful rejoin и каждом rejected observation.
func TestPreparedVolumeReadNeverRepairsUntrustedMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, *runtimecontract.WorkspaceVolumeReady)
		valid  bool
	}{
		{name: "exact", valid: true},
		{name: "foreign_inode", mutate: func(_ *testing.T, _ string, ready *runtimecontract.WorkspaceVolumeReady) { ready.Inode++ }},
		{name: "foreign_pvc", mutate: func(_ *testing.T, _ string, ready *runtimecontract.WorkspaceVolumeReady) {
			ready.Preparation.PVCUID = "40000000-0000-4000-8000-000000000001"
		}},
		{name: "missing_record", mutate: func(t *testing.T, root string, _ *runtimecontract.WorkspaceVolumeReady) {
			if err := os.Remove(filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory, runtimecontract.WorkspaceVolumeReadyFile)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "writable_record", mutate: func(t *testing.T, root string, _ *runtimecontract.WorkspaceVolumeReady) {
			if err := os.Chmod(filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory, runtimecontract.WorkspaceVolumeReadyFile), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "metadata_link", mutate: func(t *testing.T, root string, _ *runtimecontract.WorkspaceVolumeReady) {
			metadata := filepath.Join(root, runtimecontract.WorkspaceVolumeMetadataDirectory)
			if err := os.Rename(metadata, metadata+"-retained"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(metadata+"-retained", metadata); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "root_permissions", mutate: func(t *testing.T, root string, _ *runtimecontract.WorkspaceVolumeReady) {
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, name, ready, owner := preparedVolumeReadFixture(t)
			if test.mutate != nil {
				test.mutate(t, name, &ready)
			}
			var before, after unix.Stat_t
			if unix.Fstat(root, &before) != nil {
				t.Fatal("stat before read")
			}
			path := filepath.Join(name, runtimecontract.WorkspaceVolumeMetadataDirectory, runtimecontract.WorkspaceVolumeReadyFile)
			rawBefore, errBefore := os.ReadFile(path)
			err := verifyPreparedVolumeAt(t.Context(), root, &ready, owner, uint32(os.Geteuid()))
			if (err == nil) != test.valid {
				t.Fatalf("unexpected sealed volume admission: valid=%v err=%v", test.valid, err)
			}
			rawAfter, errAfter := os.ReadFile(path)
			if unix.Fstat(root, &after) != nil || before.Ino != after.Ino || before.Dev != after.Dev || before.Mode != after.Mode ||
				string(rawBefore) != string(rawAfter) || (errBefore == nil) != (errAfter == nil) {
				t.Fatal("read-only verification changed an uncertain volume")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if verifyPreparedVolumeAt(ctx, root, &ready, owner, uint32(os.Geteuid())) == nil {
				t.Fatal("canceled verification succeeded")
			}
		})
	}
}
