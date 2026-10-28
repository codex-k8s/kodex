package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// TestWorkspaceOwnedTreeUsesBoundPeerForPrivateFile joins the real Unix FD protocol to the scanner.
// Fixture identities are unprivileged; fixed production UID/mount acceptance remains a separate contour.
// TestWorkspaceOwnedTreeUsesBoundPeerForPrivateFile соединяет реальный Unix FD protocol со scanner.
// Fixture identities непривилегированные; production UID/mount проверяются отдельным контуром.
func TestWorkspaceOwnedTreeUsesBoundPeerForPrivateFile(t *testing.T) {
	root, rootFD := workspaceTreeFixture(t)
	if os.WriteFile(filepath.Join(root, "private"), []byte("provider-source"), 0o600) != nil {
		t.Fatal("create private source")
	}
	var info unix.Stat_t
	if unix.Fstat(rootFD, &info) != nil {
		t.Fatal("stat root")
	}
	client, server := workspaceReaderConnections(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	request := workspaceReaderRequest{taskSHA256: [32]byte{1}, device: uint64(info.Dev), inode: info.Ino}
	joined := make(chan error, 1)
	go func() { joined <- serveWorkspaceReaderPeer(ctx, server, rootFD, request.taskSHA256, uid, uid, gid, 4) }()
	defer func() {
		_ = client.Close()
		select {
		case err := <-joined:
			if err != nil {
				t.Error("reader failed to join")
			}
		case <-time.After(3 * time.Second):
			t.Error("reader join exceeded budget")
		}
	}()
	if workspaceReaderHello(client, request, uid) != nil {
		t.Fatal("reader startup rejected")
	}
	opened := 0
	read := func(name string, expected unix.Stat_t) (*os.File, error) {
		opened++
		request.path = name
		return openWorkspaceReaderSource(client, request, expected, uid, uid, gid)
	}
	var output bytes.Buffer
	_, size, entries, _, err := scanWorkspaceOwnedTree(ctx, rootFD, &output, runtimecontract.RuntimeWorkspacePolicyV1(), uid, gid, uid, read)
	if err != nil || opened != 1 || size != 15 || entries != 1 {
		t.Fatal("exact private source did not reach canonical stream")
	}
	reader := tar.NewReader(bytes.NewReader(output.Bytes()))
	header, err := reader.Next()
	if err != nil || header.Uname != workspaceStreamProviderOwner || header.Uid != 0 || header.Gid != 0 || header.Mode != 0o600 {
		t.Fatal("private owner or permissions were lost")
	}
	raw, err := io.ReadAll(reader)
	if err != nil || string(raw) != "provider-source" {
		t.Fatal("private bytes changed")
	}
}

// TestWorkspaceOwnedTreeRejectsDescriptorSubstitution denies a valid FD for another inode and keeps sources.
// Neither a provider callback nor a matching size may replace the independently observed source identity.
// TestWorkspaceOwnedTreeRejectsDescriptorSubstitution отклоняет FD другого inode и сохраняет исходники.
// Provider callback и совпавший размер не заменяют независимо наблюдаемую identity исходника.
func TestWorkspaceOwnedTreeRejectsDescriptorSubstitution(t *testing.T) {
	root, rootFD := workspaceTreeFixture(t)
	if os.WriteFile(filepath.Join(root, "source"), []byte("kept"), 0o600) != nil {
		t.Fatal("create source")
	}
	other := filepath.Join(t.TempDir(), "other")
	if os.WriteFile(other, []byte("fake"), 0o600) != nil {
		t.Fatal("create foreign descriptor fixture")
	}
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	read := func(string, unix.Stat_t) (*os.File, error) { return os.Open(other) }
	if _, _, _, _, err := scanWorkspaceOwnedTree(t.Context(), rootFD, io.Discard, runtimecontract.RuntimeWorkspacePolicyV1(), uid, gid, uid, read); err == nil {
		t.Fatal("substituted inode became a complete checkpoint")
	}
	raw, err := os.ReadFile(filepath.Join(root, "source"))
	if err != nil || string(raw) != "kept" {
		t.Fatal("rejection modified source")
	}
}

// TestWorkspaceStreamLogicalOwnersAreClosed prevents arbitrary usernames from entering the restore protocol.
// A rejected owner poisons the stream, so later valid entries cannot turn the partial output into a checkpoint.
// TestWorkspaceStreamLogicalOwnersAreClosed запрещает произвольные usernames в restore protocol.
// Отказ закрывает поток: следующие valid entries не превращают partial output в checkpoint.
func TestWorkspaceStreamLogicalOwnersAreClosed(t *testing.T) {
	var output bytes.Buffer
	encoder, err := newWorkspaceStreamEncoder(t.Context(), &output, runtimecontract.RuntimeWorkspacePolicyV1())
	if err != nil {
		t.Fatal(err)
	}
	if encoder.appendOwnedEntry("file", false, 0o600, 0, bytes.NewReader(nil), "root") == nil || output.Len() != 0 {
		t.Fatal("unknown owner emitted archive bytes")
	}
	if _, _, _, _, err := encoder.finish(); err == nil {
		t.Fatal("rejected owner produced a successful commitment")
	}
}
