package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// TestRunnerOutboxPublicationContainer is inert without an explicit publication mode and requires runner UID 10001 before checking private-file denial or exact dual-UID modes and collected bytes/digests inside a disposable container; a direct inactive return is not container evidence.
// TestRunnerOutboxPublicationContainer неактивен без явного publication mode и требует runner UID 10001 до проверки отказа private-file либо точных dual-UID modes и collected bytes/digests в одноразовом контейнере; прямой неактивный выход не доказывает контейнерный сценарий.
func TestRunnerOutboxPublicationContainer(t *testing.T) {
	mode := os.Getenv("KODEX_OUTBOX_PUBLICATION_TEST")
	if mode == "" {
		return
	}
	if os.Geteuid() != 10001 {
		t.Fatal("collector fixture identity is invalid")
	}
	if mode == "MANAGED_WRITE" || mode == "MANAGED_COLLECT" || mode == "MANAGED_REJECT_FOREIGN" || mode == "MANAGED_GUARDS" {
		testManagedOutboxCollection(t, mode)
		return
	}
	if mode != "UNPUBLISHED" && mode != "COLLECT" {
		t.Fatal("collector fixture binding is invalid")
	}
	root := "/workspace"
	for _, name := range []string{"private.md", "atomic.md"} {
		path := filepath.Join(root, ".kodex/outbox", name)
		if mode == "UNPUBLISHED" {
			file, err := os.Open(path)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, syscall.EACCES) {
				t.Fatal("private provider artifact did not reproduce kernel EACCES for runner")
			}
		} else {
			var stat unix.Stat_t
			if unix.Stat(path, &stat) != nil || stat.Uid != 10002 || stat.Gid != 29000 || stat.Mode&0o7777 != 0o640 {
				t.Fatal("published artifact does not retain dual-UID ABI")
			}
		}
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "synthetic completion")
	if mode == "UNPUBLISHED" {
		if err == nil {
			t.Fatal("unpublished private artifacts were collected")
		}
		return
	}
	if err != nil || len(artifacts) != 3 {
		t.Fatal("published artifacts did not reach the canonical collector")
	}
	want := map[string]string{"result.md": "synthetic completion", "private.md": "private synthetic artifact", "atomic.md": "atomic synthetic artifact"}
	for _, artifact := range artifacts {
		body, ok := want[artifact.FileName]
		digest := sha256.Sum256([]byte(body))
		if !ok || string(artifact.Content) != body || artifact.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("published artifact bytes or digest changed")
		}
	}
}

// testManagedOutboxCollection uses the real runner UID to create/read a role-owned artifact,
// checks the canonical quota canary after writer join, and rejects a readable provider-owned inode.
// testManagedOutboxCollection создаёт и читает role-owned artifact с настоящим runner UID,
// проверяет canonical quota canary после writer join и отклоняет читаемый inode provider.
func testManagedOutboxCollection(t *testing.T, mode string) {
	t.Helper()
	root := "/workspace"
	path := filepath.Join(root, ".kodex/outbox/managed-report.md")
	body := []byte("synthetic managed report")
	if mode == "MANAGED_WRITE" {
		if os.WriteFile(path, body, 0o640) != nil {
			t.Fatal("create managed role-owned report")
		}
		return
	}
	input := model.Input{WorkspaceRoot: root, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1()}
	input.EnvironmentPolicy.ManagedWorkspace = true
	if mode == "MANAGED_GUARDS" {
		testManagedOutboxGuards(t, input, path)
		return
	}
	if err := workspacepolicy.RunCanary(t.Context(), root, input.WorkspacePolicy); err != nil {
		t.Fatal("managed completion quota guard rejected fixture")
	}
	artifacts, err := collectArtifacts(input, "synthetic managed completion")
	if mode == "MANAGED_REJECT_FOREIGN" {
		if err == nil || len(artifacts) != 0 {
			t.Fatal("managed collector accepted a provider-owned file")
		}
		return
	}
	if err != nil || len(artifacts) != 2 {
		t.Fatal("managed role-owned report was not collected")
	}
	var metadata unix.Stat_t
	if unix.Stat(path, &metadata) != nil || metadata.Uid != 10001 || metadata.Gid != 29000 || metadata.Mode&0o7777 != 0o640 {
		t.Fatal("managed artifact metadata changed")
	}
	digest := sha256.Sum256(body)
	if artifacts[1].FileName != "managed-report.md" || string(artifacts[1].Content) != string(body) || artifacts[1].SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("managed artifact bytes or digest changed")
	}
}

// testManagedOutboxGuards checks real role-UID rejection of links and unsafe modes,
// then exceeds the published byte quota with a sparse file without allocating its payload.
// testManagedOutboxGuards проверяет отказ ссылок и небезопасных прав с настоящим role UID,
// затем превышает опубликованную квоту байтов sparse-файлом без выделения его payload.
func testManagedOutboxGuards(t *testing.T, input model.Input, report string) {
	t.Helper()
	link := filepath.Join(filepath.Dir(report), "link.md")
	for _, kind := range []string{"symlink", "hardlink", "mode"} {
		t.Run(kind, func(t *testing.T) {
			switch kind {
			case "symlink":
				if err := os.Symlink("managed-report.md", link); err != nil {
					t.Fatal("create synthetic symlink")
				}
				defer os.Remove(link)
			case "hardlink":
				if err := os.Link(report, link); err != nil {
					t.Fatal("create synthetic hardlink")
				}
				defer os.Remove(link)
			case "mode":
				if err := os.Chmod(report, 0o660); err != nil {
					t.Fatal("change synthetic report mode")
				}
				defer os.Chmod(report, 0o640)
			}
			if artifacts, err := collectArtifacts(input, "synthetic completion"); err == nil || len(artifacts) != 0 {
				t.Fatal("managed collector accepted unsafe metadata")
			}
		})
	}
	quotaFile := filepath.Join(filepath.Dir(report), "quota.bin")
	file, err := os.OpenFile(quotaFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal("create synthetic quota fixture")
	}
	defer os.Remove(quotaFile)
	truncateErr := file.Truncate(input.WorkspacePolicy.MaximumWritableBytes + 1)
	closeErr := file.Close()
	if truncateErr != nil || closeErr != nil {
		t.Fatal("prepare synthetic quota fixture")
	}
	if err := workspacepolicy.RunCanary(t.Context(), input.WorkspaceRoot, input.WorkspacePolicy); workspacepolicy.DenialReason(err) != runtimecontract.RuntimeWorkspaceQuotaExceeded {
		t.Fatal("managed completion accepted quota overflow")
	}
}
