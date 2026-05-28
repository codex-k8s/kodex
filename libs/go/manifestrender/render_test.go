package manifestrender

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderRendersTemplatesAndCopiesPlainFiles(t *testing.T) {
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

	if err := Render(Options{
		SourcePath:  sourceDir,
		OutputPath:  outputDir,
		EnvFilePath: filepath.Join(sourceDir, "env.conf"),
	}); err != nil {
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

func TestRenderUsesStackInventoryTemplateHelpers(t *testing.T) {
	t.Setenv("KODEX_TEST_APP_IMAGE", "")
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	servicesFile := filepath.Join(tempDir, "services.yaml")
	if err := os.WriteFile(servicesFile, []byte(`spec:
  versions:
    test-app:
      value: "0.1.0"
  images:
    test-app:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/test-app'
      tagTemplate: '{{ version "test-app" }}'
      imageEnv: KODEX_TEST_APP_IMAGE
`), 0o644); err != nil {
		t.Fatalf("write services file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.yaml.tpl"), []byte("version: {{ version \"test-app\" }}\nimage: {{ image \"test-app\" }}"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	if err := Render(Options{
		SourcePath:       sourceDir,
		OutputPath:       outputDir,
		ServicesFilePath: servicesFile,
	}); err != nil {
		t.Fatalf("run render: %v", err)
	}
	rendered, err := os.ReadFile(filepath.Join(outputDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	if string(rendered) != "version: 0.1.0\nimage: 127.0.0.1:5000/kodex/test-app:0.1.0" {
		t.Fatalf("unexpected rendered file: %q", string(rendered))
	}

	t.Setenv("KODEX_TEST_APP_IMAGE", "example.local/test-app:override")
	if err := Render(Options{
		SourcePath:       sourceDir,
		OutputPath:       outputDir,
		ServicesFilePath: servicesFile,
	}); err != nil {
		t.Fatalf("run render with env override: %v", err)
	}
	rendered, err = os.ReadFile(filepath.Join(outputDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read rendered file after override: %v", err)
	}
	if string(rendered) != "version: 0.1.0\nimage: example.local/test-app:override" {
		t.Fatalf("unexpected rendered file after override: %q", string(rendered))
	}
}

func TestPrepareOutputRootRejectsNonEmptyCallerDir(t *testing.T) {
	renderDir := t.TempDir()
	markerPath := filepath.Join(renderDir, "keep.txt")
	if err := os.WriteFile(markerPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	_, cleanup, err := PrepareOutputRoot(renderDir, "test-render-*")
	defer cleanup()
	if err == nil {
		t.Fatal("expected non-empty render dir error")
	}
	if !strings.Contains(err.Error(), "render dir must be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, err := os.ReadFile(markerPath); err != nil || string(got) != "keep" {
		t.Fatalf("marker file was changed or removed: content=%q err=%v", string(got), err)
	}
}
