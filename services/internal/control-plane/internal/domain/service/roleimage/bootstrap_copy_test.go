package roleimage

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

func TestBootstrapCopyUsesExactCatalogImage(t *testing.T) {
	base := validEnvironmentWithKey("standard", true, true)
	base.Input.Packages = nil
	base.Input.Tools = nil
	base.Input.PackageKeys = nil
	base.Input.ToolKeys = nil
	catalog, err := NewCatalog([]Environment{base})
	if err != nil {
		t.Fatal(err)
	}
	input, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	input.EnvironmentKey = "system-base"
	selection, err := catalog.CopyBootstrapSelection(input)
	if err != nil || selection.EnvironmentKey != "standard" || selection.Dockerfile != input.Dockerfile {
		t.Fatalf("exact bootstrap copy: %v", err)
	}
	foreign := input
	foreign.BaseImageDigest = "sha256:" + string(make([]byte, 64))
	if _, err := catalog.CopyBootstrapSelection(foreign); err == nil {
		t.Fatal("foreign digest was matched")
	}
	other := base
	other.Key = "other"
	other.Recommended = false
	ambiguous, err := NewCatalog([]Environment{base, other})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ambiguous.CopyBootstrapSelection(input); err == nil {
		t.Fatal("ambiguous catalog guessed a key")
	}
	different := validEnvironment(true, true)
	withPackages, err := NewCatalog([]Environment{different})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withPackages.CopyBootstrapSelection(input); err == nil {
		t.Fatal("copy silently added packages")
	}
}
