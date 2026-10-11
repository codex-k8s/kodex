package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

func workingInputFixture(t *testing.T) (model.Input, string, string) {
	t.Helper()
	return workingInputAtRoot(t, t.TempDir())
}

func workingInputAtRoot(t *testing.T, root string) (model.Input, string, string) {
	t.Helper()
	input, source, target := workingInputDescriptor(root)
	relative := "input/aset_abcdefgh/files/0001-brief.txt"
	for _, directory := range []string{filepath.Dir(relative), "work", ".kodex/outbox", "knowledge", "context"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source, []byte("bounded task input\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	return input, source, target
}

func workingInputDescriptor(root string) (model.Input, string, string) {
	payload := []byte("bounded task input\n")
	digest := sha256.Sum256(payload)
	artifact := runtimecontract.RunnerInputArtifact{Ref: "artifact_abcdefgh", FileName: "brief.txt", MediaType: "text/plain",
		Digest: "sha256:" + hex.EncodeToString(digest[:]), SizeBytes: int64(len(payload)), Revision: 1, Version: 1,
		Scope: runtimecontract.AttachmentScopeInput, Position: 1, Source: "CONTROL_CENTER", AttachmentSetRef: "aset_abcdefgh",
		AttachmentPurpose: "RUN_INPUT", Provenance: "CURRENT_TURN"}
	input := model.Input{WorkspaceRoot: root, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(), AttachmentSetRef: "aset_abcdefgh",
		AttachmentSets: []runtimecontract.RunnerAttachmentSet{{Ref: "aset_abcdefgh", ManifestDigest: strings.Repeat("a", 64), Purpose: "RUN_INPUT", Scope: runtimecontract.AttachmentScopeInput, Provenance: "CURRENT_TURN"}},
		InputArtifacts: []runtimecontract.RunnerInputArtifact{artifact}}
	relative := "input/aset_abcdefgh/files/0001-brief.txt"
	source := filepath.Join(root, relative)
	return input, source, filepath.Join(root, "work", relative)
}

// Запускается только в отдельном network-none container с tmpfs /workspace.
func TestWorkingDirectoryTwoUIDFixture(t *testing.T) {
	mode := os.Getenv("KODEX_WORKING_UID_FIXTURE")
	if mode == "" {
		t.Skip("isolated two-UID fixture was not requested")
	}
	var workspaceFS, rootFS unix.Statfs_t
	if unix.Statfs("/workspace", &workspaceFS) != nil || workspaceFS.Type != unix.TMPFS_MAGIC ||
		unix.Statfs("/", &rootFS) != nil || rootFS.Flags&unix.ST_RDONLY == 0 {
		t.Fatal("fixture requires disposable tmpfs workspace and read-only container root")
	}
	if mode == "COPY" {
		if os.Geteuid() != 10002 || os.Getegid() != 10001 {
			t.Fatal("wrong provider fixture identity")
		}
		input, source, target := workingInputDescriptor("/workspace")
		if err := prepareWorkingDirectory(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("provider edited"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("must deny"), 0o440); err == nil {
			t.Fatal("provider edited original")
		}
		private := filepath.Join("/workspace/work/clone/.git/objects")
		if err := os.MkdirAll(private, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(private, "stale"), []byte("scratch"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := prepareWorkingDirectory(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(target); err != nil || string(raw) != "bounded task input\n" {
			t.Fatal("two-UID reset changed original copy")
		}
		return
	}
	if mode != "SETUP" || os.Geteuid() != 0 {
		t.Fatal("wrong fixture setup")
	}
	_, source, _ := workingInputAtRoot(t, "/workspace")
	if err := filepath.Walk("/workspace", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, 10001, 10001)
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/workspace/work", "/workspace/.kodex/outbox"} {
		if err := os.Chmod(path, os.ModeSetgid|0o770); err != nil {
			t.Fatal(err)
		}
	}
	if info, err := os.Stat(source); err != nil || info.Mode().Perm() != 0o440 {
		t.Fatal("original fixture mode changed")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestWorkingDirectoryTwoUIDFixture$", "-test.v")
	command.Env = []string{"KODEX_WORKING_UID_FIXTURE=COPY", "PATH=/usr/bin:/bin", "HOME=/nonexistent"}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{10001}}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("two-UID child failed: %s", output)
	} else {
		t.Log(string(output))
	}
}

func TestWorkingDirectoryCopiesAndResetsOnlyTaskInputs(t *testing.T) {
	input, source, target := workingInputFixture(t)
	knowledge := runtimecontract.RunnerInputArtifact{Ref: "artifact_knowledge", FileName: "policy.md", MediaType: "text/markdown", Digest: "sha256:" + strings.Repeat("b", 64), SizeBytes: 1, Revision: 1, Version: 1, Scope: runtimecontract.AttachmentScopeKnowledge, Position: 1, Source: "KNOWLEDGE_SOURCE", AttachmentPurpose: "PROJECT_KNOWLEDGE", Provenance: "PROJECT_BINDING"}
	input.InputArtifacts = append(input.InputArtifacts, knowledge)
	history := input.InputArtifacts[0]
	history.Ref, history.AttachmentSetRef, history.Scope, history.Provenance, history.AttachmentPurpose = "artifact_history1", "aset_history1", runtimecontract.AttachmentScopeSession, "SESSION_HISTORY", "SESSION_TURN"
	input.AttachmentSets = append(input.AttachmentSets, runtimecontract.RunnerAttachmentSet{Ref: history.AttachmentSetRef, ManifestDigest: strings.Repeat("b", 64), Scope: history.Scope, Provenance: history.Provenance, Purpose: history.AttachmentPurpose})
	input.InputArtifacts = append(input.InputArtifacts, history)
	if err := prepareWorkingDirectoryOwned(t.Context(), input, uint32(os.Geteuid())); err != nil {
		t.Fatal(err)
	}
	guidePath := filepath.Join(input.WorkspaceRoot, "work", workspaceGuideName)
	if guide, err := os.ReadFile(guidePath); err != nil || string(guide) != workspaceGuide {
		t.Fatal("workspace navigation guide missing")
	}
	if err := os.WriteFile(guidePath, []byte("untrusted edited guide"), 0o640); err != nil {
		t.Fatal(err)
	}
	sourceInfo, _ := os.Stat(source)
	targetInfo, err := os.Stat(target)
	if err != nil || os.SameFile(sourceInfo, targetInfo) || targetInfo.Mode().Perm()&0o200 == 0 {
		t.Fatal("working copy must be separate and editable")
	}
	if err := os.WriteFile(target, []byte("edited"), 0o640); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil || string(raw) != "bounded task input\n" {
		t.Fatal("original changed")
	}
	stale := filepath.Join(input.WorkspaceRoot, "work", "clone", ".git", "objects")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "stale"), []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "sentinel"), []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(input.WorkspaceRoot, "work", "outside")); err != nil {
		t.Fatal(err)
	}
	if err := prepareWorkingDirectoryOwned(t.Context(), input, uint32(os.Geteuid())); err != nil {
		t.Fatal(err)
	}
	if guide, err := os.ReadFile(guidePath); err != nil || string(guide) != workspaceGuide {
		t.Fatal("edited guide survived isolated workspace reset")
	}
	raw, err = os.ReadFile(target)
	if err != nil || string(raw) != "bounded task input\n" {
		t.Fatal("copy was not reset")
	}
	if raw, err := os.ReadFile(filepath.Join(outside, "sentinel")); err != nil || string(raw) != "untouched" {
		t.Fatal("reset escaped work")
	}
	if _, err := os.Stat(filepath.Join(input.WorkspaceRoot, "work/input/aset_history1")); !os.IsNotExist(err) {
		t.Fatal("history was copied as editable input")
	}
	for _, name := range []string{"clone", "knowledge", "context", ".kodex"} {
		if _, err := os.Stat(filepath.Join(input.WorkspaceRoot, "work", name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected copied tree %s", name)
		}
	}
}

func TestWorkingDirectoryGuideExistsWithoutAttachmentsAndUsesQuota(t *testing.T) {
	input, _, _ := workingInputFixture(t)
	input.InputArtifacts, input.AttachmentSets, input.AttachmentSetRef = nil, nil, ""
	if err := prepareWorkingDirectoryOwned(t.Context(), input, uint32(os.Geteuid())); err != nil {
		t.Fatal(err)
	}
	guidePath := filepath.Join(input.WorkspaceRoot, "work", workspaceGuideName)
	if guide, err := os.ReadFile(guidePath); err != nil || string(guide) != workspaceGuide {
		t.Fatal("empty workspace lost navigation guide")
	}
	for _, guard := range []string{"/workspace/work/input/", "/workspace/input/", "/workspace/context/", "/workspace/knowledge/", "working_copy_path", "не требуй от пользователя", "текущими capabilities", "сбрасываются", "## Карта каталогов", "## Как начать", "при совпадении", "отсутствие файла не разрешает", "во внутреннем state"} {
		if !strings.Contains(workspaceGuide, guard) {
			t.Fatalf("workspace guide lost boundary %q", guard)
		}
	}
	policy, err := runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: int64(len(workspaceGuide)) - 1, MaxFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	input.WorkspacePolicy = policy
	if err := prepareWorkingDirectoryOwned(t.Context(), input, uint32(os.Geteuid())); err == nil {
		t.Fatal("guide bypassed byte quota")
	}
	input, _, _ = workingInputFixture(t)
	input.WorkspacePolicy, err = runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 1 << 20, MaxFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareWorkingDirectoryOwned(t.Context(), input, uint32(os.Geteuid())); err == nil {
		t.Fatal("guide and input bypassed file count quota")
	}
}

func TestWorkingDirectoryRejectsSourceTamperingAndUnsafeRoot(t *testing.T) {
	for _, mutation := range []string{"digest", "hardlink", "symlink", "owner", "root-symlink", "quota"} {
		t.Run(mutation, func(t *testing.T) {
			input, source, target := workingInputFixture(t)
			uid := uint32(os.Geteuid())
			switch mutation {
			case "digest":
				input.InputArtifacts[0].Digest = "sha256:" + strings.Repeat("b", 64)
			case "hardlink":
				if err := os.Link(source, filepath.Join(input.WorkspaceRoot, "linked")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/etc/passwd", source); err != nil {
					t.Fatal(err)
				}
			case "owner":
				uid++
			case "root-symlink":
				if err := os.Remove(filepath.Join(input.WorkspaceRoot, "work")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(input.WorkspaceRoot, "work")); err != nil {
					t.Fatal(err)
				}
			case "quota":
				policy, err := runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 1, MaxFiles: 100})
				if err != nil {
					t.Fatal(err)
				}
				input.WorkspacePolicy = policy
			}
			if err := prepareWorkingDirectoryOwned(t.Context(), input, uid); err == nil {
				t.Fatal("unsafe working input was accepted")
			}
			if mutation == "quota" {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("copy exceeded quota before rejecting input")
				}
			}
		})
	}
}
