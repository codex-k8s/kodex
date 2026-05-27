package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRunResolvesImagesFromServicesYamlWithEnvOverride(t *testing.T) {
	t.Setenv("KODEX_REGISTRY_IMAGE", "")
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	servicesFile := filepath.Join(tempDir, "services.yaml")
	if err := os.WriteFile(servicesFile, []byte(`spec:
  versions:
    registry:
      value: "2.9"
    agent-manager:
      value: "0.1.0"
  images:
    registry:
      from: 'registry:{{ version "registry" }}'
    agent-manager:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/{{ envOr "KODEX_AGENT_MANAGER_INTERNAL_IMAGE_REPOSITORY" "kodex/agent-manager" }}'
      tagTemplate: '{{ version "agent-manager" }}'
      imageEnv: KODEX_AGENT_MANAGER_IMAGE
`), 0o644); err != nil {
		t.Fatalf("write services file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.yaml.tpl"), []byte("image: {{ imageOr \"registry\" \"KODEX_REGISTRY_IMAGE\" }}\nservice: {{ image \"agent-manager\" }}"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	if err := runWithOptions(renderOptions{
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
	if string(rendered) != "image: registry:2.9\nservice: 127.0.0.1:5000/kodex/agent-manager:0.1.0" {
		t.Fatalf("unexpected rendered file: %q", string(rendered))
	}

	t.Setenv("KODEX_REGISTRY_IMAGE", "example.local/registry:override")
	t.Setenv("KODEX_AGENT_MANAGER_IMAGE", "example.local/agent-manager:override")
	if err := runWithOptions(renderOptions{
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
	if string(rendered) != "image: example.local/registry:override\nservice: example.local/agent-manager:override" {
		t.Fatalf("unexpected rendered file after override: %q", string(rendered))
	}
}

func TestRunFailsWhenImageFromReferencesMissingVersion(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	servicesFile := filepath.Join(tempDir, "services.yaml")
	if err := os.WriteFile(servicesFile, []byte(`spec:
  images:
    registry:
      from: 'registry:{{ index .Versions "registry" }}'
`), 0o644); err != nil {
		t.Fatalf("write services file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.yaml.tpl"), []byte(`image: {{ image "registry" }}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	err := runWithOptions(renderOptions{
		SourcePath:       sourceDir,
		OutputPath:       outputDir,
		ServicesFilePath: servicesFile,
	})
	if err == nil {
		t.Fatal("expected missing version error")
	}
	if !strings.Contains(err.Error(), `version "registry" is not defined in services.yaml`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFailsWhenImageTagTemplateReferencesMissingVersion(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	servicesFile := filepath.Join(tempDir, "services.yaml")
	if err := os.WriteFile(servicesFile, []byte(`spec:
  images:
    agent-manager:
      repository: '127.0.0.1:5000/kodex/agent-manager'
      tagTemplate: '{{ index .Versions "agent-manager" }}'
`), 0o644); err != nil {
		t.Fatalf("write services file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.yaml.tpl"), []byte(`image: {{ image "agent-manager" }}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	err := runWithOptions(renderOptions{
		SourcePath:       sourceDir,
		OutputPath:       outputDir,
		ServicesFilePath: servicesFile,
	})
	if err == nil {
		t.Fatal("expected missing version error")
	}
	if !strings.Contains(err.Error(), `version "agent-manager" is not defined in services.yaml`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
