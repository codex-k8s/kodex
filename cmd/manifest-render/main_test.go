package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRendersTemplatesAndCopiesPlainFiles(t *testing.T) {
	t.Setenv("KODEX_TEST_VALUE", "")
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "env.conf"), []byte("KODEX_TEST_VALUE='from env file'\n"), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.yaml.tpl"), []byte(`value: {{ envOr "KODEX_TEST_VALUE" "fallback" }}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plain.txt"), []byte("plain"), 0o644); err != nil {
		t.Fatalf("write plain file: %v", err)
	}

	if err := run(sourceDir, outputDir, filepath.Join(sourceDir, "env.conf")); err != nil {
		t.Fatalf("run render: %v", err)
	}

	rendered, err := os.ReadFile(filepath.Join(outputDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	if string(rendered) != "value: from env file" {
		t.Fatalf("unexpected rendered file: %q", string(rendered))
	}
	copied, err := os.ReadFile(filepath.Join(outputDir, "plain.txt"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(copied) != "plain" {
		t.Fatalf("unexpected copied file: %q", string(copied))
	}
}
