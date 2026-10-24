package roleimage

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestCatalogResolveKeepsSupplyChainValuesServerOwned checks environment resolution preserves
// pinned image/context/package data, isolates returned mutation and exposes only the safe
// recommended environment key.
// TestCatalogResolveKeepsSupplyChainValuesServerOwned проверяет сохранение pinned image, context и
// package данных при разрешении окружения, изоляцию изменений результата и выдачу только
// безопасного ключа рекомендованного окружения.
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
	selection, err := catalog.RecommendedSelection()
	if err != nil || selection.EnvironmentKey != "documents" || selection.Dockerfile != "" || len(selection.PackageKeys) != 0 {
		t.Fatalf("unsafe recommended selection: selection=%#v err=%v", selection, err)
	}
}

// TestCatalogRejectsUnavailableAndUnapprovedSelection rejects unavailable environments,
// unregistered package selections and installation text when the chosen environment does not allow
// customization.
// TestCatalogRejectsUnavailableAndUnapprovedSelection отклоняет недоступные окружения,
// незарегистрированные выбранные packages и installation текст при запрете customization выбранным
// окружением.
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

// TestCatalogRequiresExactlyOneAvailableRecommendation checks catalog creation fails for zero or
// multiple available recommendations rather than choosing a fallback entry.
// TestCatalogRequiresExactlyOneAvailableRecommendation проверяет отказ создания каталога при
// отсутствии либо нескольких доступных рекомендациях вместо выбора запасного элемента.
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

// validEnvironment builds the standard synthetic documents environment with requested
// recommendation/availability flags through the shared fixture factory.
// validEnvironment создаёт стандартное синтетическое окружение documents с запрошенными флагами
// рекомендации и доступности через общую фабрику оснастки.
func validEnvironment(recommended, available bool) Environment {
	return validEnvironmentWithKey("documents", recommended, available)
}

// validEnvironmentWithKey builds a pinned synthetic Linux recipe and OCR package with stable
// source/context/toolchain digests and localization keys for a chosen catalog key.
// validEnvironmentWithKey создаёт pinned синтетические Linux рецепт и OCR package с устойчивыми
// source, context и toolchain digests и ключами локализации выбранного catalog key.
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

// TestCatalogResolveSelectsPinnedEntries checks package/tool subset selection, unchanged empty
// selection defaults, result isolation and rejection of unknown keys through the catalog boundary.
// TestCatalogResolveSelectsPinnedEntries проверяет выбор подмножеств packages/tools, прежние defaults
// пустого выбора, изоляцию результата и отказ неизвестным ключам через границу каталога.
func TestCatalogResolveSelectsPinnedEntries(t *testing.T) {
	environment := validEnvironment(true, true)
	originalPackage := environment.Input.Packages[0]
	environment.Input.PackageKeys = []string{"alpha", "ocr", "zeta"}
	environment.Input.Packages = nil
	environment.Input.ToolKeys = []string{"alpha", "jq", "zeta"}
	for _, name := range environment.Input.PackageKeys {
		item := originalPackage
		item.Name = name
		environment.Input.Packages = append(environment.Input.Packages, item)
	}
	for _, name := range environment.Input.ToolKeys {
		environment.Input.Tools = append(environment.Input.Tools, entity.RoleImageTool{
			Name: name, Version: "1.0", SourceRef: "https://tools.invalid/" + name, SHA256: strings.Repeat("c", 64),
		})
	}
	catalog, err := NewCatalog([]Environment{environment})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: environment.Key})
	if err != nil || !slices.Equal(defaults.Packages, environment.Input.Packages) || !slices.Equal(defaults.Tools, environment.Input.Tools) {
		t.Fatalf("empty selection changed catalog defaults: %v", err)
	}
	selection := entity.RoleEnvironmentSelection{EnvironmentKey: environment.Key, PackageKeys: []string{"alpha", "zeta"}, ToolKeys: []string{"alpha", "zeta"}}
	selected, err := catalog.Resolve(selection)
	if err != nil {
		t.Fatal(err)
	}
	packages := []entity.RoleImagePackage{environment.Input.Packages[0], environment.Input.Packages[2]}
	tools := []entity.RoleImageTool{environment.Input.Tools[0], environment.Input.Tools[2]}
	if !slices.Equal(selected.PackageKeys, selection.PackageKeys) || !slices.Equal(selected.ToolKeys, selection.ToolKeys) ||
		!slices.Equal(selected.Packages, packages) || !slices.Equal(selected.Tools, tools) {
		t.Fatal("selection lost exact same-index package or tool pins")
	}
	selected.Packages[0].Digest = "tampered"
	selected.Tools[0].SHA256 = "tampered"
	again, err := catalog.Resolve(selection)
	if err != nil || !slices.Equal(again.Packages, packages) || !slices.Equal(again.Tools, tools) {
		t.Fatalf("returned entries changed catalog storage: %v", err)
	}
	for _, invalid := range []entity.RoleEnvironmentSelection{
		{EnvironmentKey: environment.Key, PackageKeys: []string{"unknown"}},
		{EnvironmentKey: environment.Key, ToolKeys: []string{"unknown"}},
	} {
		if _, err := catalog.Resolve(invalid); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("unknown selection reached recipe output")
		}
	}
}
