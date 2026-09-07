package app

import (
	"os"
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
}
