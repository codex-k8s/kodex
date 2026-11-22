package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMainFailsOnUnjoinedHandlers builds the exact production entrypoint with a synthetic failing app port, then verifies exit 1 for RPC/upload join failures even with a live background goroutine. App handler/dependency join is tested separately in the real composition package.
// TestMainFailsOnUnjoinedHandlers собирает exact production entrypoint с synthetic failing app port и проверяет exit 1 для RPC/upload join failures также при live background goroutine. Handler/dependency join отдельно проверяется в actual composition package.
func TestMainFailsOnUnjoinedHandlers(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate production entrypoint")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	appDirectory := filepath.Join(directory, "internal", "app")
	if os.MkdirAll(appDirectory, 0700) != nil {
		t.Fatal("create isolated app port")
	}
	files := map[string][]byte{
		"go.mod":              []byte("module github.com/codex-k8s/kodex/services/internal/secret-broker\n\ngo 1.26.10\n"),
		"main.go":             source,
		"internal/app/app.go": []byte("package app\nimport(\"context\";\"errors\";\"os\")\nfunc Run(context.Context,context.Context,string)error{go func(){select{}}();return errors.New(os.Getenv(\"KODEX_SYNTHETIC_JOIN_FAILURE\"))}\n"),
	}
	for name, raw := range files {
		if os.WriteFile(filepath.Join(directory, name), raw, 0600) != nil {
			t.Fatal("write isolated entrypoint fixture")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	binary := filepath.Join(directory, "broker-entrypoint")
	build := exec.CommandContext(ctx, "go", "build", "-p=1", "-o", binary, ".")
	build.Dir = directory
	build.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated production entrypoint: %v: %s", err, output)
	}
	for _, diagnostic := range []string{"managed Kubernetes upload handlers did not join", "SSH identity upload handlers did not join", "secret broker RPC handlers or transport did not join"} {
		process := exec.CommandContext(ctx, binary)
		process.Env = append(os.Environ(), "KODEX_SYNTHETIC_JOIN_FAILURE="+diagnostic)
		output, err := process.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || strings.TrimSpace(string(output)) != diagnostic || ctx.Err() != nil {
			t.Fatal("entrypoint did not terminate with fixed failure")
		}
	}
	t.Log(fmt.Sprintf("production main SHA256 %x; isolated app failure exited 1", sha256.Sum256(source)))
}
