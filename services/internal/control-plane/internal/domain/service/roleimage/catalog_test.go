package roleimage

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestCatalogResolveKeepsSupplyChainValuesServerOwned(t *testing.T) {
	catalog, err := NewCatalog([]Environment{validEnvironment(true, true)})
	if err != nil {
		t.Fatalf("construct catalog: %v", err)
	}

	resolved, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "documents"})
	if err != nil {
		t.Fatalf("resolve environment: %v", err)
	}
	if resolved.BaseImageReference != "registry.internal/role-base-documents" ||
		resolved.ContextRef != "oci://registry.internal/role-input@sha256:"+strings.Repeat("b", 64) ||
		len(resolved.Packages) != 1 || resolved.Packages[0].Name != "tesseract-ocr" {
		t.Fatalf("resolved supply-chain input is incomplete: %#v", resolved)
	}

	resolved.BaseImageReference = "caller.invalid/override"
	again, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "documents"})
	if err != nil {
		t.Fatalf("resolve environment again: %v", err)
	}
	if again.BaseImageReference != "registry.internal/role-base-documents" {
		t.Fatal("catalog leaked mutable recipe state")
	}
}

func TestCatalogRejectsUnavailableAndUnapprovedSelection(t *testing.T) {
	catalog, err := NewCatalog([]Environment{
		validEnvironment(true, true),
		validEnvironmentWithKey("accounting", false, false),
	})
	if err != nil {
		t.Fatalf("construct catalog: %v", err)
	}

	for _, selection := range []entity.RoleEnvironmentSelection{
		{EnvironmentKey: "accounting"},
		{EnvironmentKey: "documents", PackageKeys: []string{"unknown"}},
		{EnvironmentKey: "documents", InstallationBlock: "RUN curl https://example.invalid"},
	} {
		if _, err := catalog.Resolve(selection); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("expected invalid selection for %#v, got %v", selection, err)
		}
	}
}

func TestCatalogRequiresExactlyOneAvailableRecommendation(t *testing.T) {
	if _, err := NewCatalog([]Environment{validEnvironment(false, true)}); err == nil {
		t.Fatal("catalog without recommendation must fail")
	}
	if _, err := NewCatalog([]Environment{
		validEnvironment(true, true),
		validEnvironmentWithKey("accounting", true, true),
	}); err == nil {
		t.Fatal("catalog with multiple recommendations must fail")
	}
}

func validEnvironment(recommended, available bool) Environment {
	return validEnvironmentWithKey("documents", recommended, available)
}

func validEnvironmentWithKey(key string, recommended, available bool) Environment {
	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	return Environment{
		Key: key, NameMessageKey: "role-environments." + key + ".name",
		DescriptionMessageKey: "role-environments." + key + ".description",
		SoftwareMessageKeys:   []string{"role-environments.software.ocr"},
		Recommended:           recommended, Available: available,
		Input: entity.RoleImageRecipeInput{
			BaseImageReference: "registry.internal/role-base-documents",
			BaseImageDigest:    "sha256:" + digestA,
			SourceRef:          "https://source.invalid/kodex",
			SourceRevision:     "revision-1",
			SourceSHA256:       digestA,
			ContextRef:         "oci://registry.internal/role-input@sha256:" + digestB,
			ContextSHA256:      digestB,
			BuilderSHA256:      digestA,
			FrontendSHA256:     digestA,
			ToolchainSHA256:    digestA,
			EnvironmentKey:     key,
			PackageKeys:        []string{"ocr"},
			Platforms:          []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
			Packages: []entity.RoleImagePackage{{
				Manager: "apt", Name: "tesseract-ocr", Version: "1.0",
				Digest: "sha256:" + digestA, SourceRef: "https://packages.invalid/tesseract-ocr",
			}},
		},
	}
}
