package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// configurationHomeFixture creates an independent temporary home and pinned synthetic authentication digest, leaving sandbox, volumes, overlays and secret projections explicit in each consuming test. It never reads live provider credentials.
// configurationHomeFixture создаёт независимый временный home и закреплённый дайджест synthetic authentication, оставляя sandbox, volumes, overlays и secret projections явными в каждом тесте. Живые provider-реквизиты не читает.
func configurationHomeFixture(t *testing.T) (model.Input, []byte) {
	t.Helper()
	workspace := t.TempDir()
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestValue := hex.EncodeToString(digest[:])
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(digestValue), 0o600); err != nil {
		t.Fatal(err)
	}
	return model.Input{WorkspaceRoot: workspace, NativeStateRoot: filepath.Join(workspace, ".kodex", "state", "opencode"),
		Model: "gpt-5", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
		WorkspaceAccess:        "workspace-write",
		ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: digestValue}, auth
}

// TestProviderSandboxProbeUsesBoundedBubblewrapBoundary captures the proposed bwrap command without executing it, checks exact identity/root/environment arguments and safe failure classification; it does not prove a live kernel sandbox.
// TestProviderSandboxProbeUsesBoundedBubblewrapBoundary перехватывает предполагаемую bwrap-команду без запуска, сверяет точные identity/root/environment arguments и safe failure classification; не доказывает живую kernel sandbox.
func TestProviderSandboxProbeUsesBoundedBubblewrapBoundary(t *testing.T) {
	previousEUID := os.Geteuid()
	called := false
	err := verifyProviderSandbox(context.Background(), func(command *exec.Cmd) error {
		called = true
		expected := []string{"/usr/bin/bwrap", "--unshare-user", "--uid", strconv.Itoa(previousEUID),
			"--gid", strconv.Itoa(previousEUID), "--ro-bind", "/", "/", "/usr/bin/true"}
		if !slices.Equal(command.Args, expected) || !slices.Equal(command.Env, []string{"PATH=/usr/local/bin:/usr/bin:/bin"}) {
			t.Fatalf("unexpected provider sandbox probe: args=%q env=%q", command.Args, command.Env)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("verifyProviderSandbox() error = %v, called = %t", err, called)
	}
	if err := verifyProviderSandbox(context.Background(), func(*exec.Cmd) error { return errors.New("denied") }); err == nil || err.Error() != "OpenCode provider sandbox is unavailable" {
		t.Fatalf("provider sandbox probe failure was not classified safely: %v", err)
	}
}
