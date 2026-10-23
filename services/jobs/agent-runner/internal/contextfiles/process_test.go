package contextfiles

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// TestContextProcessFixture is the opt-in non-root child fixture: verifies the read-only context and exercises allowed workspace writes or rejected protected writes.
// Without its mode variable it returns immediately; that return alone does not prove mount isolation and starts no AI agent.
// TestContextProcessFixture является явно включаемым дочерним образцом без root: проверяет read-only контекст и допустимые записи рабочей области либо запрещённые защищённые записи.
// Без переменной режима сразу возвращается; такой возврат сам по себе не доказывает изоляцию монтирования и не запускает ИИ-агента.
func TestContextProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_CONTEXT_PROCESS_TEST")
	if mode == "" {
		return
	}
	if os.Geteuid() == 0 {
		t.Fatal("context acceptance fixture must run as non-root")
	}
	raw, err := os.ReadFile(runtimecontract.RuntimeContextRoot + "/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var current manifest
	if json.Unmarshal(raw, &current) != nil {
		t.Fatal("invalid test manifest")
	}
	input, _, _, _ := fixture()
	if err := verifyAt(runtimecontract.RuntimeContextRoot, input, current.Snapshot, current.Snapshot.Skills[0].Provenance.CreatedAt, true); err != nil {
		t.Fatal(err)
	}
	if mode == "writable-workspace" {
		if err := os.MkdirAll("/workspace/work/nested", 0o700); err != nil {
			t.Fatal(err)
		}
		file := "/workspace/work/nested/result.txt"
		if err := os.WriteFile(file, []byte("first"), 0o600); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(file); err != nil || string(raw) != "first" {
			t.Fatal("workspace read failed")
		}
		if err := os.WriteFile(file+".next", []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(file+".next", file); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("/workspace/result.txt", []byte("published fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	file := map[string]string{"skill": "/workspace/context/skills/sklb_abcdefgh/SKILL.md", "memory": "/workspace/context/memory/memr_abcdefgh.md",
		"new context": "/workspace/context/injected", "symlink": "/workspace/context-alias/memory/memr_abcdefgh.md",
		"traversal": "/workspace/work/../context/memory/memr_abcdefgh.md"}[mode]
	if file == "" {
		t.Fatal("unknown process fixture mode")
	}
	if err := os.WriteFile(file, []byte("forbidden"), 0o600); !errors.Is(err, unix.EROFS) && !errors.Is(err, os.ErrPermission) {
		t.Fatalf("protected write was not rejected: %v", err)
	}
}

// TestContextReadOnlyMountAndWritableWorkspaceProcesses runs six synthetic test-binary modes in bounded bwrap user namespaces with explicit writable workspace and read-only context mounts.
// Checks successful workspace I/O, denied direct/symlink/traversal context writes and unchanged context afterward; no model or cluster is involved.
// TestContextReadOnlyMountAndWritableWorkspaceProcesses запускает шесть синтетических режимов тестового бинарника в ограниченных пространствах пользователя bwrap с явно записываемой рабочей областью и read-only контекстом.
// Проверяет успешный ввод-вывод рабочей области, запрет прямой записи, через ссылки и выход по пути в контекст, его неизменность после; модель и кластер не используются.
func TestContextReadOnlyMountAndWritableWorkspaceProcesses(t *testing.T) {
	for _, mode := range []string{"writable-workspace", "skill", "memory", "new context", "symlink", "traversal"} {
		t.Run(mode, func(t *testing.T) {
			input, snapshot, source, now := fixture()
			root, workspace := t.TempDir(), t.TempDir()
			if err := materializeAt(t.Context(), root, input, snapshot, source, now); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(workspace, "work"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/workspace/context", filepath.Join(workspace, "context-alias")); err != nil {
				t.Fatal(err)
			}
			bwrap, err := exec.LookPath("bwrap")
			if err != nil {
				t.Fatal("bwrap is required for context process acceptance")
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, bwrap, "--unshare-user", "--uid", strconv.Itoa(os.Geteuid()), "--gid", strconv.Itoa(os.Getegid()),
				"--tmpfs", "/", "--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64",
				"--ro-bind", executable, "/context-test", "--bind", workspace, "/workspace", "--ro-bind", root, runtimecontract.RuntimeContextRoot,
				"--chdir", "/workspace", "--remount-ro", "/", "/context-test", "-test.run=^TestContextProcessFixture$")
			command.Env = []string{"KODEX_CONTEXT_PROCESS_TEST=" + mode, "PATH=/usr/bin:/bin"}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("context process failed: %v\n%s", err, output)
			}
			if err := verifyAt(root, input, snapshot, now, false); err != nil {
				t.Fatal("agent modified immutable context")
			}
		})
	}
}
