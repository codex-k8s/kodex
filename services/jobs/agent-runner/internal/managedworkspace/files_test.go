package managedworkspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// fileStoreFixture builds only synthetic local files with current-user metadata; it neither
// contacts a controller nor grants real execution authority or runs commands.
// fileStoreFixture создаёт только synthetic локальные файлы текущего пользователя; controller
// не вызывает, настоящих полномочий не выдаёт и команды не запускает.
func fileStoreFixture(t *testing.T) *fileStore {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"work", ".kodex", ".kodex/outbox"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	provider := runtimecontract.ModelProviderConfiguration{
		Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40),
		Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway",
		BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"},
		Models: []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}},
	}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, Provider: "opencode", CodexSandbox: "workspace-write", ModelProvider: &provider,
		AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_fixture", LeaseRef: "lea_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 1,
		InputDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64), Capabilities: []string{runtimecontract.ArtifactCapability},
		WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(), EnvironmentPolicy: runtimecontract.DefaultRuntimeEnvironmentPolicy()}
	input.EnvironmentPolicy.ManagedWorkspace = true
	if !runtimecontract.RuntimeWorkspaceToolsAvailable(input) {
		t.Fatal("invalid synthetic profile")
	}
	return &fileStore{input: input, root: root, runnerUID: uint32(os.Geteuid()), writerUID: uint32(os.Geteuid()), group: uint32(os.Getegid())}
}

// TestFilesCASAndUTF8Read exercises actual kernel-confined create/read/replace, stale hashes,
// absent-only replay and a multi-byte chunk boundary without any shell or network access.
// TestFilesCASAndUTF8Read проверяет настоящие confined create/read/replace, stale hash,
// absent-only replay и multi-byte chunk boundary без shell и сети.
func TestFilesCASAndUTF8Read(t *testing.T) {
	store := fileStoreFixture(t)
	ctx := context.Background()
	hash, err := store.write(ctx, "work/src/hello.go", "Привет\n", "ABSENT")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.read(ctx, "work/src/hello.go", 0, 5)
	if err != nil || got.Text != "Пр" || got.NextOffset != 4 || got.SHA256 != hash {
		t.Fatal(got, err)
	}
	if _, err := store.write(ctx, "work/src/hello.go", "lost", "ABSENT"); err == nil {
		t.Fatal("repeated create replaced bytes")
	}
	if _, err := store.write(ctx, "work/src/hello.go", "lost", strings.Repeat("0", 64)); err == nil {
		t.Fatal("stale CAS accepted")
	}
	if _, err := store.write(ctx, "work/src/hello.go", "package main\n", hash); err != nil {
		t.Fatal(err)
	}
	entries, more, err := store.list(ctx, "work/src", "")
	if err != nil || more || len(entries) != 1 || entries[0].Path != "work/src/hello.go" {
		t.Fatal(entries, more, err)
	}
	if _, err := store.read(ctx, "work/src/hello.go", 1, 4); err != nil {
		t.Fatal(err)
	}
}

// TestFilesRejectPathAliasesLinksAndSpecialEntries proves no read/write reaches a private
// sibling through traversal, symlink, hardlink or FIFO, and disabling the profile closes access.
// TestFilesRejectPathAliasesLinksAndSpecialEntries доказывает отказ обхода к private sibling
// через traversal, symlink, hardlink и FIFO; выключение профиля закрывает доступ.
func TestFilesRejectPathAliasesLinksAndSpecialEntries(t *testing.T) {
	store := fileStoreFixture(t)
	ctx := context.Background()
	private := filepath.Join(store.root, "private")
	if err := os.WriteFile(private, []byte("synthetic private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(store.root, "work/link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(private, filepath.Join(store.root, "work/hard")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(store.root, "work/fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work/../private", "/etc/passwd", "work/link", "work/hard", "work/fifo", "outbox/../private", "work//file", "work/a/../../private"} {
		if _, err := store.read(ctx, name, 0, 32); err == nil {
			t.Fatal("unsafe read accepted", name)
		}
		if _, err := store.write(ctx, name, "replacement", "ABSENT"); err == nil {
			t.Fatal("unsafe write accepted", name)
		}
	}
	store.input.EnvironmentPolicy.ManagedWorkspace = false
	if _, _, err := store.list(ctx, "work", ""); err == nil {
		t.Fatal("opt-out ignored")
	}
	data, err := os.ReadFile(private)
	if err != nil || string(data) != "synthetic private" {
		t.Fatal("private sibling modified")
	}
}

// TestFileQuotasApplyBeforeNewDirectories prevents failed writes from growing unbounded empty
// directories and requires all writes to include existing bytes in the shared quota.
// TestFileQuotasApplyBeforeNewDirectories не позволяет failed write плодить неограниченные пустые
// каталоги и требует учитывать существующие bytes в общей квоте каждой записи.
func TestFileQuotasApplyBeforeNewDirectories(t *testing.T) {
	store := fileStoreFixture(t)
	ctx := context.Background()
	policy, err := runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4, MaxFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	store.input.WorkspacePolicy = policy
	if _, err := store.write(ctx, "work/a/b/file", "x", "ABSENT"); err == nil {
		t.Fatal("directory quota ignored")
	}
	if _, err := os.Stat(filepath.Join(store.root, "work/a")); !os.IsNotExist(err) {
		t.Fatal("over-budget directory created")
	}
	hash, err := store.write(ctx, "work/file", "1234", "ABSENT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.write(ctx, "work/file", "12345", hash); err == nil {
		t.Fatal("byte quota ignored")
	}
	got, err := store.read(ctx, "work/file", 0, 32)
	if err != nil || got.Text != "1234" {
		t.Fatal(got, err)
	}
}
