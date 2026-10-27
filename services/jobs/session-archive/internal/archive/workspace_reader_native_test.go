package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// TestWorkspaceReaderTwoUIDNative runs only in the explicit disposable local Docker fixture.
// The root coordinator has no source-read role; both real consumers run without capabilities on a read-only volume.
// TestWorkspaceReaderTwoUIDNative запускается только в явной disposable local Docker fixture.
// Root coordinator не читает исходник; оба настоящих consumer работают без capabilities на read-only томе.
func TestWorkspaceReaderTwoUIDNative(t *testing.T) {
	mode := os.Getenv("KODEX_WORKSPACE_READER_NATIVE")
	if mode == "" {
		t.Skip("requires explicit disposable two-UID fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if mode == "coordinator" {
		workspaceReaderNativeChildren(t, ctx)
		return
	}
	task, _ := workspaceSnapshotFixture(t)
	restored := os.Getenv("KODEX_WORKSPACE_READER_NATIVE_TREE") == "restored"
	if restored {
		joined, err := runtimecontract.DecodeWorkspaceQuiescence([]byte(task.Workspace.Quiescence))
		if err != nil {
			t.Fatal("read native snapshot writer fixture")
		}
		joined.Generation++
		task.Workspace.Target.Generation++
		raw, err := runtimecontract.EncodeWorkspaceQuiescence(joined)
		if err != nil {
			t.Fatal("bind native snapshot to published generation")
		}
		task.Workspace.Quiescence = string(raw)
		identity, err := task.Workspace.SnapshotIdentity()
		if err != nil {
			t.Fatal("derive native published generation identity")
		}
		task.TargetObjectKey = identity.ObjectPrefix() + "manifest.json"
	}
	var caps [2]unix.CapUserData
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if unix.Capget(&header, &caps[0]) != nil || caps[0].Effective != 0 || caps[1].Effective != 0 || caps[0].Permitted != 0 || caps[1].Permitted != 0 {
		t.Fatal("native consumer retained capabilities")
	}
	if mode == "reader" {
		if os.Geteuid() != 10002 || ServeWorkspaceReader(ctx, task) != nil {
			t.Fatal("fixed provider reader failed")
		}
		return
	}
	if mode != "scanner" || os.Geteuid() != 10001 {
		t.Fatal("native fixture identity is invalid")
	}
	fd, err := unix.Open(WorkspaceSnapshotRoot+"/role.txt", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if fd >= 0 {
		_ = unix.Close(fd)
	}
	if !errors.Is(err, unix.EROFS) {
		t.Fatal("source mount is not read-only")
	}
	providerPath := "/provider.txt"
	if restored {
		providerPath = "/private/state"
	}
	fd, err = unix.Open(WorkspaceSnapshotRoot+providerPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if fd >= 0 {
		_ = unix.Close(fd)
	}
	if !errors.Is(err, unix.EACCES) {
		t.Fatal("main consumer bypassed provider-private permissions")
	}
	var output bytes.Buffer
	digest, size, count, _, err := EncodeWorkspaceSnapshot(ctx, &output, task)
	if err != nil {
		t.Fatal("fixed-UID source scan returned an error")
	}
	reader := tar.NewReader(bytes.NewReader(output.Bytes()))
	expected := map[string]struct {
		owner, body string
		mode        int64
	}{
		"provider.txt": {workspaceStreamProviderOwner, "provider-source", 0o600},
		"role.txt":     {workspaceStreamRoleOwner, "role-src", 0o600},
	}
	wantCount, wantBytes := int64(2), int64(23)
	if restored {
		wantCount, wantBytes = 3, 9
		expected = map[string]struct {
			owner, body string
			mode        int64
		}{"private/": {workspaceStreamProviderOwner, "", 0o700},
			"private/state": {workspaceStreamProviderOwner, "draft", 0o600}, "role.txt": {workspaceStreamRoleOwner, "role", 0o600}}
		restore, _ := workspaceWriterNativeCheckpoint(t)
		if digest != restore.Workspace.Checkpoint.TreeSHA256 {
			t.Fatal("restored private tree did not retain its canonical source digest")
		}
	}
	if count != wantCount || size != wantBytes {
		t.Fatal("fixed-UID source scan counts changed")
	}
	for range wantCount {
		header, err := reader.Next()
		if err != nil {
			t.Fatal("native archive entry missing")
		}
		entry, exists := expected[header.Name]
		raw, readErr := io.ReadAll(reader)
		if !exists || readErr != nil || header.Uname != entry.owner || header.Mode != entry.mode || string(raw) != entry.body {
			t.Fatal("native archive owner or bytes changed")
		}
		delete(expected, header.Name)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) || len(expected) != 0 {
		t.Fatal("native archive contains unexpected entries")
	}
}

// workspaceReaderNativeChildren drops the fixture's two identity-switch capabilities before either consumer starts.
// Each child is bounded and joined; failed source checks cannot leave a listener or a detached worker.
// workspaceReaderNativeChildren сбрасывает две identity-switch capabilities до запуска каждого consumer.
// Каждый child ограничен и завершён; отказ не оставляет listener или detached worker.
func workspaceReaderNativeChildren(t *testing.T, ctx context.Context) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("disposable fixture coordinator must switch child identities")
	}
	command := func(mode string, uid uint32) *exec.Cmd {
		child := exec.CommandContext(ctx, "/proc/self/exe", "-test.run=^TestWorkspaceReaderTwoUIDNative$", "-test.timeout=25s", "-test.v")
		child.Env = []string{"KODEX_WORKSPACE_READER_NATIVE=" + mode, "GOMAXPROCS=1", "GOMEMLIMIT=12MiB", "TMPDIR=/tmp"}
		if os.Getenv("KODEX_WORKSPACE_READER_NATIVE_TREE") == "restored" {
			child.Env = append(child.Env, "KODEX_WORKSPACE_READER_NATIVE_TREE=restored")
		}
		child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 29000}}
		child.WaitDelay = time.Second
		return child
	}
	server := command("reader", 10002)
	var serverOutput bytes.Buffer
	server.Stdout, server.Stderr = &serverOutput, &serverOutput
	if server.Start() != nil {
		t.Fatal("start native reader")
	}
	client := command("scanner", 10001)
	clientOutput, clientErr := client.CombinedOutput()
	if clientErr != nil {
		_ = server.Process.Kill()
	}
	serverErr := server.Wait()
	if server.ProcessState != nil {
		if usage, ok := server.ProcessState.SysUsage().(*syscall.Rusage); ok {
			t.Logf("reader_peak_rss_kib=%d", usage.Maxrss)
		}
	}
	if clientErr != nil || serverErr != nil {
		t.Logf("scanner: %s", clientOutput)
		t.Logf("reader: %s", serverOutput.Bytes())
		t.Fatal("native identity consumers did not complete")
	}
}
