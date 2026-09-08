package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectExecutable(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "binary")
	data := []byte("synthetic executable")
	if err := os.WriteFile(binary, data, 0o555); err != nil {
		t.Fatal(err)
	}
	add := func(pid string) {
		t.Helper()
		path := filepath.Join(root, pid)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(binary, filepath.Join(path, "exe")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "stat"), []byte(pid+" (test name) S "+strings.Repeat("0 ", 18)+"123 0\n"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	add("12")
	p, err := inspect(root, binary)
	if err != nil || p.PID != 12 || p.StartTicks != "123" || p.BinarySHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("unexpected executable proof: %v", err)
	}
	if _, err := inspect(root, binary+"-missing"); err == nil {
		t.Fatal("missing process accepted")
	}
	add("13")
	if _, err := inspect(root, binary); err == nil {
		t.Fatal("duplicate process accepted")
	}
	if err := os.RemoveAll(filepath.Join(root, "13")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "12", "stat"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "12", "stat"), []byte("invalid"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(root, binary); err == nil {
		t.Fatal("invalid identity accepted")
	}
}

func TestClosedArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--role", "publisher"}, {"--role", "issuer", "--file", "/private"}, {"--file", "/private"}} {
		if err := run(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}
