package app

import (
	"os"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
)

func TestWorkspaceNestedMountContainer(t *testing.T) {
	if os.Getenv("KODEX_WORKSPACE_CONTAINER_TEST") != "1" {
		t.Skip("disposable container entrypoint only")
	}
	if os.Geteuid() != 10001 || os.Getegid() != 10001 {
		t.Fatal("unexpected fixture identity")
	}
	if err := materializeWorkspaceDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"../outside", "/workspace/.kodex", ".kodex/../outside"} {
		if security.EnsureSharedWorkspaceDirectory(invalid) == nil {
			t.Fatal("invalid workspace path accepted")
		}
	}
	for _, path := range []string{"/workspace/.kodex/inbox/proof", "/workspace/.kodex/state/codex-home/proof", "/workspace/input/proof", "/workspace/knowledge/proof"} {
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll("/workspace/input/nested", 0770); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/workspace/input/nested/proof", []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceProtectionContainer(t *testing.T) {
	if os.Getenv("KODEX_WORKSPACE_CONTAINER_TEST") != "1" {
		t.Skip("disposable container entrypoint only")
	}
	if os.Geteuid() != 10001 {
		t.Fatal("unexpected fixture identity")
	}
	if err := protectReadOnlyWorkspaceTrees("/workspace", "input", "knowledge"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/workspace/input", "/workspace/knowledge"} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 0 || stat.Gid != 29000 || info.Mode().Perm() != 0777 {
			t.Fatal("volume root changed")
		}
	}
	for path, mode := range map[string]os.FileMode{"/workspace/input/nested": 0750 | os.ModeSetgid, "/workspace/input/nested/proof": 0440} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&(os.ModePerm|os.ModeSetgid) != mode {
			t.Fatal("descendant protection mismatch")
		}
	}
}
