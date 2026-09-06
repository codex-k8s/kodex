package contextfiles

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMaterializeWithPrivateUmask(t *testing.T) {
	const child = "KODEX_CONTEXT_UMASK_TEST"
	if os.Getenv(child) != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestMaterializeWithPrivateUmask$")
		command.Env = append(os.Environ(), child+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("private umask subprocess failed: %v\n%s", err, output)
		}
		return
	}
	// Только выделенный subprocess меняет process-wide umask.
	unix.Umask(0o077)
	input, snapshot, source, now := fixture()
	for _, name := range []string{"references/deep/example.md", "references/second.md"} {
		pin := snapshot.Skills[0].Files[0]
		pin.Path = name
		snapshot.Skills[0].Files = append(snapshot.Skills[0].Files, pin)
	}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	root := t.TempDir()
	if err := materializeAt(t.Context(), root, input, snapshot, source, now); err != nil {
		t.Fatal(err)
	}
	if source.calls != 3 {
		t.Fatal("nested skill files were not fetched")
	}
	if err := verifyAt(root, input, snapshot, now, false); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == root {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		want := os.FileMode(0o440)
		if entry.IsDir() {
			want = 0o750
		}
		if info.Mode().Perm() != want {
			t.Fatalf("materialized mode = %o, want %o", info.Mode().Perm(), want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Ошибка повторной материализации закрывает старый manifest и partial tree.
	source.fail = true
	if err := materializeAt(t.Context(), root, input, snapshot, source, now); !errors.Is(err, ErrContextFiles) {
		t.Fatalf("failed source accepted: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed materialization retained partial context")
	}
}

func TestExactModeRejectsClosedDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "mode")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exactMode(file, 0o440); !errors.Is(err, ErrContextFiles) {
		t.Fatal("chmod failure was ignored")
	}
}
