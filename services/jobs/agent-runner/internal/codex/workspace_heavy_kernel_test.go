package codex

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestProviderHeavyWorkspaceKernel is an opt-in UID10002 child restricted to the disposable heavy
// fixture root. BUILD prepares synthetic files; FINISH checks publication and quota/type normalization,
// retaining closed failure outcomes. Without a stage it skips instead of testing privileged isolation.
// TestProviderHeavyWorkspaceKernel — opt-in UID10002 child под disposable heavy fixture root.
// BUILD готовит synthetic files; FINISH проверяет публикацию и quota/type normalization,
// сохраняя закрытые failure outcomes. Без stage она пропускается, не проверяя privileged isolation.
func TestProviderHeavyWorkspaceKernel(t *testing.T) {
	stage := os.Getenv("KODEX_HEAVY_FIXTURE_STAGE")
	if stage == "" {
		t.Skip("disposable dual-UID fixture entrypoint only")
	}
	if os.Geteuid() != 10002 {
		t.Fatal("provider fixture identity mismatch")
	}
	root, kind := os.Getenv("KODEX_HEAVY_FIXTURE_ROOT"), os.Getenv("KODEX_HEAVY_FIXTURE_KIND")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(root))), "TestHeavyWorkspacePublicationKernel") {
		t.Fatal("unconfined heavy fixture root")
	}
	if stage == "BUILD" {
		buildHeavyWorkspaceFixture(t, root, kind)
		return
	}
	if stage != "FINISH" {
		t.Fatal("unknown provider fixture stage")
	}
	outbox, err := os.Open(filepath.Join(root, ".kodex/outbox"))
	if err != nil {
		t.Fatal("open synthetic outbox")
	}
	defer outbox.Close()
	if err := publishOutboxDirectory(t.Context(), outbox, runtimecontract.RuntimeWorkspaceMaximumFiles, 10001, 10002, 29000); err != nil {
		t.Fatal("synthetic provider publication failed")
	}
	work, err := os.Open(filepath.Join(root, "work"))
	if err != nil {
		t.Fatal("open synthetic working directory")
	}
	defer work.Close()
	var entries, bytes int64
	if filepath.WalkDir(filepath.Join(root, "work"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			bytes += info.Size()
		}
		return nil
	}) != nil {
		t.Fatal("read synthetic workload metadata")
	}
	t.Logf("synthetic work_entries=%d work_bytes=%d", entries, bytes)
	started := time.Now()
	var guard error
	got := normalizeCompletedProviderWorkspace(t.Context(), model.Input{}, Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic heavy result"}, func(ctx context.Context, _ model.Input) error {
		guard = normalizeWorkspaceAccess(ctx, work, runtimecontract.RuntimeWorkspacePolicyV1(), 10001, 10002, 29000)
		return guard
	})
	t.Logf("synthetic kind=%s normalization_ms=%d max_files=%d max_bytes=%d budget_ms=%d", kind, time.Since(started).Milliseconds(), runtimecontract.RuntimeWorkspaceMaximumFiles, runtimecontract.RuntimeWorkspaceWritableBytes, workspaceAccessBudget.Milliseconds())
	if kind == "clean" {
		if guard != nil || got.Outcome != "SUCCEEDED" || entries < 7000 || entries > runtimecontract.RuntimeWorkspaceMaximumFiles || bytes > runtimecontract.RuntimeWorkspaceWritableBytes {
			t.Fatal("valid heavy fixture rejected")
		}
		return
	}
	want := map[string]providerFilesystemReason{"count": filesystemEntryCount, "bytes": filesystemByteQuota, "symlink": filesystemFileType}[kind]
	assertProviderFilesystemReason(t, guard, errProviderWorkspaceAccess, want)
	if got.Outcome != "FAILED" || got.FailureCode != "RUNTIME_ARTIFACT_INVALID" {
		t.Fatal("unsafe heavy fixture did not retain closed terminal failure")
	}
}

// buildHeavyWorkspaceFixture prepares disposable local Git/worktree data, private sparse cache
// entries for count/byte/type cases and seven bounded outbox files. Git has no remote and receives no
// inherited credentials; the caller must supply the isolated owned fixture root.
// buildHeavyWorkspaceFixture готовит disposable local Git/worktree data, private sparse cache
// для count/byte/type случаев и семь bounded outbox files. У Git нет remote и унаследованных
// credentials; caller обязан передать изолированный принадлежащий fixture root.
func buildHeavyWorkspaceFixture(t *testing.T, root, kind string) {
	t.Helper()
	work := filepath.Join(root, "work")
	repository := filepath.Join(work, "repository")
	if os.Mkdir(repository, 0o700) != nil {
		t.Fatal("create synthetic Git repository")
	}
	git := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "/usr/bin/git", append([]string{"-C", repository}, args...)...)
		command.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		if err := command.Run(); err != nil {
			t.Fatal("synthetic local Git operation failed")
		}
	}
	git("init", "-q")
	for index := range 150 {
		path := filepath.Join(repository, fmt.Sprintf("source-%03d.go", index))
		if os.WriteFile(path, []byte(fmt.Sprintf("package fixture\nconst Value%d = %d\n", index, index)), 0o600) != nil {
			t.Fatal("create synthetic Git source")
		}
	}
	git("add", ".")
	git("-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Синтетическая оснастка")
	git("worktree", "add", "-q", "-b", "fixture-work", filepath.Join(work, "checkout"))
	cache := filepath.Join(work, "dependency-cache")
	if os.Mkdir(cache, 0o700) != nil || os.Chown(cache, -1, 10002) != nil {
		t.Fatal("prepare private synthetic dependency cache")
	}
	count := 100
	if kind == "clean" {
		count = 7000
	} else if kind == "count" {
		count = 10001
	}
	for index := range count {
		path := filepath.Join(cache, fmt.Sprintf("object-%05d.cache", index))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal("create synthetic dependency cache entry")
		}
		// Sparse regular file: quota metadata без записи большого объёма на диск.
		if file.Truncate(16<<10) != nil || file.Close() != nil {
			t.Fatal("seal synthetic dependency cache entry")
		}
	}
	if kind == "bytes" {
		file, err := os.OpenFile(filepath.Join(cache, "oversized.cache"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil || file.Truncate(runtimecontract.RuntimeWorkspaceWritableBytes+1) != nil || file.Close() != nil {
			t.Fatal("prepare synthetic byte quota overflow")
		}
	}
	if kind == "symlink" && os.Symlink("object-00000.cache", filepath.Join(cache, "dependency-link")) != nil {
		t.Fatal("prepare synthetic dependency symlink")
	}
	for index, size := range []int{899757, 21792, 100000, 60000, 60000, 40000, 20000} {
		body := []byte(strings.Repeat(string(rune('a'+index)), size))
		if os.WriteFile(filepath.Join(root, ".kodex/outbox", fmt.Sprintf("output-%d.txt", index)), body, 0o600) != nil {
			t.Fatal("prepare seven synthetic flat outbox files")
		}
	}
}
