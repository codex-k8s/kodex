package imageinventory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestProbeDoesNotInventMissingAndIgnoresOutsideSymlinks(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "usr/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/PRIVATE_SENTINEL", filepath.Join(directory, "usr/bin/git")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	probe := runtimecontract.ImageToolProbe{Name: "git", Paths: []string{"/usr/bin/git"}, Args: []string{"--version"}, Required: true}
	item := observe(context.Background(), root, probe)
	if item.Status != "MISSING" || item.Path != "" || item.Version != "" || item.SHA256 != "" || !item.Required {
		t.Fatal("missing tool was fabricated")
	}
}

func TestVersionProjectionDropsRawOutputAndBounds(t *testing.T) {
	for _, raw := range []string{"go version go1.26.6 linux/amd64 PRIVATE_SENTINEL", "git version 2.53.0 PRIVATE_SENTINEL", "v1.2.3\nPRIVATE_SENTINEL"} {
		match := versionPattern.FindStringSubmatch(raw)
		if len(match) != 2 || strings.Contains(match[1], "PRIVATE") || strings.Contains(match[1], " ") {
			t.Fatal("raw probe output escaped version projection")
		}
	}
	var output boundedOutput
	raw := make([]byte, 8192)
	n, err := output.Write(raw)
	if n != len(raw) || err != nil || output.Len() != 4096 || !output.overflow {
		t.Fatal("output budget is not bounded")
	}
}

func TestProbeModeRejectsCallerArgumentsBeforeReadingRuntime(t *testing.T) {
	if Run(context.Background(), []string{"runner", Mode, "arbitrary"}) == nil {
		t.Fatal("unknown probe arguments accepted")
	}
}
