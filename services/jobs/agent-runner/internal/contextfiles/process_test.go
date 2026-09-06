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
