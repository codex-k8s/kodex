package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// Отдельная точка запуска: root исполняет только изолированные синтетические ELF
// под двумя UID; учётные данные, runtime Pod и пользовательские файлы не нужны.
func TestHeavyWorkspacePublicationKernel(t *testing.T) {
	if stage := os.Getenv("KODEX_HEAVY_FIXTURE_STAGE"); stage != "" {
		heavyWorkspaceRunnerChild(t, stage)
		return
	}
	if os.Getenv("KODEX_HEAVY_KERNEL_TEST") != "1" {
		t.Skip("disposable root fixture entrypoint only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture identity required")
	}
	providerBinary := os.Getenv("KODEX_HEAVY_PROVIDER_TEST_BINARY")
	if !filepath.IsAbs(providerBinary) {
		t.Fatal("compiled provider fixture ELF required")
	}
	for _, kind := range []string{"clean", "count", "bytes", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			// testing создаёт дополнительный собственный ancestor с mode0700.
			if os.Chmod(filepath.Dir(parent), 0o711) != nil || os.Chmod(parent, 0o711) != nil {
				t.Fatal("prepare confined fixture parent")
			}
			root := filepath.Join(parent, "root")
			for _, relative := range []string{"", "work", ".kodex", ".kodex/outbox"} {
				path := filepath.Join(root, relative)
				if os.Mkdir(path, 0o700) != nil || os.Chown(path, 10001, 29000) != nil || unix.Chmod(path, 0o2770) != nil {
					t.Fatal("prepare exact dual-UID fixture roots")
				}
			}
			copyELF := func(sourcePath, name string) string {
				t.Helper()
				source, err := os.Open(sourcePath)
				if err != nil {
					t.Fatal("open fixture ELF")
				}
				defer source.Close()
				path := filepath.Join(parent, name)
				target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o500)
				if err != nil {
					t.Fatal("create fixture ELF copy")
				}
				if _, err := io.Copy(target, source); err != nil || target.Chmod(0o555) != nil || target.Close() != nil {
					t.Fatal("seal fixture ELF copy")
				}
				return path
			}
			provider := copyELF(providerBinary, "provider.test")
			runner := copyELF("/proc/self/exe", "runner.test")
			run := func(binary, test, stage string, uid uint32) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary, "-test.run=^"+test+"$", "-test.timeout=25s", "-test.v")
				command.Env = []string{"LANG=C", "KODEX_HEAVY_FIXTURE_STAGE=" + stage, "KODEX_HEAVY_FIXTURE_ROOT=" + root, "KODEX_HEAVY_FIXTURE_KIND=" + kind}
				command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{29000}}}
				command.Stdout, command.Stderr = os.Stdout, os.Stderr
				if err := command.Run(); err != nil {
					t.Fatal("bounded heavy fixture stage failed: " + stage)
				}
			}
			run(provider, "TestProviderHeavyWorkspaceKernel", "BUILD", 10002)
			run(runner, "TestHeavyWorkspacePublicationKernel", "BEFORE", 10001)
			run(provider, "TestProviderHeavyWorkspaceKernel", "FINISH", 10002)
			if kind == "clean" {
				run(runner, "TestHeavyWorkspacePublicationKernel", "COLLECT", 10001)
			}
		})
	}
}

func heavyWorkspaceRunnerChild(t *testing.T, stage string) {
	if os.Geteuid() != 10001 {
		t.Fatal("runner fixture identity mismatch")
	}
	root := os.Getenv("KODEX_HEAVY_FIXTURE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(root))), "TestHeavyWorkspacePublicationKernel") {
		t.Fatal("unconfined runner fixture root")
	}
	ctx, cancel := context.WithTimeout(t.Context(), workspaceCanaryBudget)
	defer cancel()
	started := time.Now()
	err := workspacepolicy.RunCanary(ctx, root, runtimecontract.RuntimeWorkspacePolicyV1())
	if stage == "BEFORE" {
		if err == nil || workspacepolicy.DenialReason(err) != runtimecontract.RuntimeWorkspaceReadOnly {
			t.Fatal("private cache did not reproduce traversal denial before normalization")
		}
		return
	}
	if stage != "COLLECT" || err != nil {
		t.Fatal("normalized heavy fixture failed runner canary")
	}
	t.Logf("synthetic canary_ms=%d", time.Since(started).Milliseconds())
	provenance := workspacepolicy.ResultProvenance{Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 1, ExecutionBindingDigest: strings.Repeat("b", 64)}
	if workspacepolicy.PublishResult(ctx, root, runtimecontract.RuntimeWorkspacePolicyV1(), provenance) != nil {
		t.Fatal("heavy fixture provenance publication failed")
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "synthetic heavy result")
	if err != nil || len(artifacts) != 9 {
		t.Fatal("heavy fixture seven files or service artifacts were not collected")
	}
	total := 0
	for _, artifact := range artifacts {
		sum := sha256.Sum256(artifact.Content)
		if artifact.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("heavy fixture artifact digest mismatch")
		}
		total += len(artifact.Content)
	}
	completion := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 1, Success: true, ResultSummary: "synthetic heavy result", Artifacts: artifacts}
	if completion.Validate() != nil {
		t.Fatal("heavy fixture successful completion failed canonical validation")
	}
	if _, err := os.ReadFile(filepath.Join(root, "work/dependency-cache/object-00000.cache")); !errors.Is(err, syscall.EACCES) {
		t.Fatal("normalization opened a private dependency cache body")
	}
	t.Logf("synthetic published_files=%d collected_files=%d collected_bytes=%d", 7, len(artifacts), total)
}
