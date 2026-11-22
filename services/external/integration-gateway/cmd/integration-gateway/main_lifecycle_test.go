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

// TestMainFailsOnCancelledLifecycleFailure builds the exact production main against an isolated app port that joins cancellation with fatal unjoined work; a background goroutine must never turn this into exit zero.
// TestMainFailsOnCancelledLifecycleFailure собирает exact production main с isolated app port, объединяющим cancellation и fatal unjoined work; background goroutine не превращает это в exit zero.
func TestMainFailsOnCancelledLifecycleFailure(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate production main")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if os.MkdirAll(filepath.Join(directory, "internal", "app"), 0700) != nil {
		t.Fatal("create isolated app port")
	}
	files := map[string][]byte{
		"go.mod":              []byte("module github.com/codex-k8s/kodex/services/external/integration-gateway\n\ngo 1.26.10\n"),
		"main.go":             source,
		"internal/app/app.go": []byte("package app\nimport(\"context\";\"errors\")\nfunc Run(context.Context,context.Context,string)error{go func(){select{}}();return errors.Join(context.Canceled,errors.New(\"synthetic unjoined work\"))}\n"),
	}
	for name, raw := range files {
		if os.WriteFile(filepath.Join(directory, name), raw, 0600) != nil {
			t.Fatal("write isolated main fixture")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	binary := filepath.Join(directory, "gateway-entrypoint")
	build := exec.CommandContext(ctx, "go", "build", "-p=1", "-o", binary, ".")
	build.Dir = directory
	build.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated entrypoint: %v: %s", err, output)
	}
	output, err := exec.CommandContext(ctx, binary).CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || strings.TrimSpace(string(output)) != "integration-gateway stopped with an error" || ctx.Err() != nil {
		t.Fatal("cancelled fatal app failure did not exit one")
	}
	t.Log(fmt.Sprintf("production main SHA256 %x; isolated cancelled fatal failure exited one", sha256.Sum256(source)))
}
