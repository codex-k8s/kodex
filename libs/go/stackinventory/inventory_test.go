package stackinventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStackResolvesVersionAndImages(t *testing.T) {
	t.Setenv("KODEX_REGISTRY_IMAGE", "")
	t.Setenv("KODEX_AGENT_MANAGER_IMAGE", "")
	stack := mustParseStack(t, `spec:
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
`)

	version, err := stack.Version("registry")
	if err != nil {
		t.Fatalf("resolve version: %v", err)
	}
	if version != "2.9" {
		t.Fatalf("unexpected version: %q", version)
	}
	registry, err := stack.ImageOr("registry", "KODEX_REGISTRY_IMAGE")
	if err != nil {
		t.Fatalf("resolve registry image: %v", err)
	}
	if registry != "registry:2.9" {
		t.Fatalf("unexpected registry image: %q", registry)
	}
	agentManager, err := stack.Image("agent-manager")
	if err != nil {
		t.Fatalf("resolve agent-manager image: %v", err)
	}
	if agentManager != "127.0.0.1:5000/kodex/agent-manager:0.1.0" {
		t.Fatalf("unexpected agent-manager image: %q", agentManager)
	}

	t.Setenv("KODEX_REGISTRY_IMAGE", "example.local/registry:override")
	t.Setenv("KODEX_AGENT_MANAGER_IMAGE", "example.local/agent-manager:override")
	registry, err = stack.ImageOr("registry", "KODEX_REGISTRY_IMAGE")
	if err != nil {
		t.Fatalf("resolve registry image with override: %v", err)
	}
	if registry != "example.local/registry:override" {
		t.Fatalf("unexpected overridden registry image: %q", registry)
	}
	agentManager, err = stack.Image("agent-manager")
	if err != nil {
		t.Fatalf("resolve agent-manager image with override: %v", err)
	}
	if agentManager != "example.local/agent-manager:override" {
		t.Fatalf("unexpected overridden agent-manager image: %q", agentManager)
	}
}

func TestStackFailsWhenImageTemplateReferencesMissingVersion(t *testing.T) {
	tests := []struct {
		name       string
		imageName  string
		source     string
		wantReason string
	}{
		{
			name:      "from",
			imageName: "registry",
			source: `spec:
  images:
    registry:
      from: 'registry:{{ index .Versions "registry" }}'
`,
			wantReason: `version "registry" is not defined in services.yaml`,
		},
		{
			name:      "tag template",
			imageName: "agent-manager",
			source: `spec:
  images:
    agent-manager:
      repository: '127.0.0.1:5000/kodex/agent-manager'
      tagTemplate: '{{ index .Versions "agent-manager" }}'
`,
			wantReason: `version "agent-manager" is not defined in services.yaml`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := mustParseStack(t, tt.source)
			_, err := stack.Image(tt.imageName)
			if err == nil {
				t.Fatal("expected missing version error")
			}
			if !strings.Contains(err.Error(), tt.wantReason) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStackFailsWhenImageIsMissing(t *testing.T) {
	stack := mustParseStack(t, `spec:
  versions:
    registry:
      value: "2"
`)

	_, err := stack.Image("registry")
	if err == nil {
		t.Fatal("expected missing image error")
	}
	if !strings.Contains(err.Error(), `image "registry" is not defined in services.yaml`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadEnvFileDoesNotExposeValues(t *testing.T) {
	t.Setenv("KODEX_STACKINVENTORY_TEST_ONE", "")
	t.Setenv("KODEX_STACKINVENTORY_TEST_TWO", "")
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte("KODEX_STACKINVENTORY_TEST_ONE='quoted value'\nKODEX_STACKINVENTORY_TEST_TWO=\"escaped\\nvalue\"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := LoadEnvFile(envFile); err != nil {
		t.Fatalf("load env file: %v", err)
	}
	if got := os.Getenv("KODEX_STACKINVENTORY_TEST_ONE"); got != "quoted value" {
		t.Fatalf("unexpected single quoted value: %q", got)
	}
	if got := os.Getenv("KODEX_STACKINVENTORY_TEST_TWO"); got != "escaped\nvalue" {
		t.Fatalf("unexpected double quoted value: %q", got)
	}
}

func mustParseStack(t *testing.T, source string) Stack {
	t.Helper()
	stack, err := Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	return stack
}
