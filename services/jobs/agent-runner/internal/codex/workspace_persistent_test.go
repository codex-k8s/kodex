package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// persistentWorkingFixture creates real files and an opt-in binding without simulating owner BIND.
// These tests exercise the locked preparation helper; mount/authority need separate native proof.
// persistentWorkingFixture создаёт настоящие файлы и opt-in binding, не имитируя owner BIND.
// Здесь проверяется preparation helper; mount/authority требуют отдельного native proof.
func persistentWorkingFixture(t *testing.T) (model.Input, string, string) {
	t.Helper()
	input, source, target := workingInputFixture(t)
	input.Mode, input.AssistantScope = runtimecontract.RunnerModeTurn, runtimecontract.AssistantScopeNone
	input.OrganizationRef, input.ProjectRef = "org_fixture000", "prj_fixture000"
	input.SessionRef, input.AgentRef = "ses_fixture000", "agt_fixture000"
	input.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	input.WorkspaceStorage = &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope:        runtimecontract.WorkspaceStorageScope{OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, SessionRef: input.SessionRef, AgentRef: input.AgentRef},
		WorkspaceRef: "wks_fixture000", Generation: 1}
	return input, source, target
}

// preparePersistentFixture invokes real preparation with owned descriptors but no fake mount authority.
// The production opener must still validate its read-only exact-subPath proof separately.
// preparePersistentFixture вызывает настоящую подготовку с owned descriptors, не fake mount authority.
// Production opener отдельно проверяет read-only exact-subPath proof.
func preparePersistentFixture(t *testing.T, input model.Input) error {
	t.Helper()
	directory, err := os.Open(filepath.Join(input.WorkspaceRoot, "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	root, err := os.OpenRoot(filepath.Join(input.WorkspaceRoot, "work"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return preparePersistentWorkingTree(t.Context(), input, root, directory, uint32(os.Geteuid()))
}

// TestPersistentWorkingTreeKeepsDraftAndEditedInput proves preservation and current-original checks.
// A corrupted original must fail even though an independently edited working copy already exists.
// TestPersistentWorkingTreeKeepsDraftAndEditedInput доказывает сохранение draft и проверку оригинала.
// Повреждённый оригинал отклоняется и при уже существующей изменённой рабочей копии.
func TestPersistentWorkingTreeKeepsDraftAndEditedInput(t *testing.T) {
	input, source, target := persistentWorkingFixture(t)
	if err := preparePersistentFixture(t, input); err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(input.WorkspaceRoot, "work", "draft.go")
	for name, value := range map[string]string{draft: "package draft\n", target: "edited source copy\n"} {
		if err := os.WriteFile(name, []byte(value), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := preparePersistentFixture(t, input); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{draft: "package draft\n", target: "edited source copy\n", source: "bounded task input\n"} {
		if raw, err := os.ReadFile(name); err != nil || string(raw) != value {
			t.Fatal("persistent preparation replaced existing work or original bytes")
		}
	}
	guide, err := os.ReadFile(filepath.Join(input.WorkspaceRoot, "work", workspaceGuideName))
	if err != nil || strings.Contains(string(guide), "рабочие файлы\nсбрасываются") || !strings.Contains(string(guide), "не дают\nполномочий на повтор внешнего действия") {
		t.Fatal("persistent guide contradicts preservation or grants external replay")
	}
	if err := os.Chmod(source, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("tampered original\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o440); err != nil {
		t.Fatal(err)
	}
	if err := preparePersistentFixture(t, input); err == nil {
		t.Fatal("edited working copy concealed current original tampering")
	}
	if raw, _ := os.ReadFile(draft); string(raw) != "package draft\n" {
		t.Fatal("refusal deleted saved work")
	}
}

// TestPersistentWorkingTreeRefusesUnsafeCopyWithoutDeletingData checks real link and quota failures.
// The directory budget counts missing INPUT parent directories before materialization starts.
// TestPersistentWorkingTreeRefusesUnsafeCopyWithoutDeletingData проверяет link и quota отказы.
// Directory budget учитывает недостающие INPUT parents до начала материализации.
func TestPersistentWorkingTreeRefusesUnsafeCopyWithoutDeletingData(t *testing.T) {
	for _, kind := range []string{"hardlink", "symlink", "entry-quota", "byte-quota"} {
		t.Run(kind, func(t *testing.T) {
			input, _, target := persistentWorkingFixture(t)
			draft := filepath.Join(input.WorkspaceRoot, "work", "saved.txt")
			if err := os.WriteFile(draft, []byte("keep"), 0o640); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "hardlink", "symlink":
				if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "hardlink" {
					err = os.Link(draft, target)
				} else {
					err = os.Symlink(draft, target)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "entry-quota":
				input.WorkspacePolicy.MaximumFileCount = 7
			case "byte-quota":
				input.WorkspacePolicy.MaximumWritableBytes = int64(len(materializedWorkspaceGuide(input)))
			}
			if err := preparePersistentFixture(t, input); err == nil {
				t.Fatal("unsafe persistent preparation accepted")
			}
			if raw, err := os.ReadFile(draft); err != nil || string(raw) != "keep" {
				t.Fatal("closed refusal deleted saved bytes")
			}
			if _, err := os.Lstat(filepath.Join(input.WorkspaceRoot, "work", workspaceGuideName)); !os.IsNotExist(err) {
				t.Fatal("preflight refusal wrote the platform guide")
			}
		})
	}
}
