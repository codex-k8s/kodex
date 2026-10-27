package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// imageInventoryFixture builds a synthetic one-platform inventory with every registered probe
// MISSING, binding canonical manifest and outer JSON digests to supplied artifact pins; it neither
// probes a real image nor creates verified tools.
// imageInventoryFixture строит синтетический inventory одной платформы, где все зарегистрированные
// probes имеют MISSING, связывая digest manifest и внешнего JSON с переданными artifact pins;
// реальный образ не проверяется, VERIFIED tools не создаются.
func imageInventoryFixture(artifact entity.ImageArtifact) (string, string) {
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, SpecSHA256: artifact.SpecSHA256, ImmutableBuildSHA256: artifact.ImmutableBuildSHA256, RuntimeContractSHA256: artifact.RoleRuntimeContractSHA256, Platform: "linux/amd64"}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	raw, _ := json.Marshal(manifest)
	value := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema, ImageDigest: artifact.ManifestDigest, ProvenanceSHA256: artifact.ProvenanceSHA256, Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: artifact.ManifestDigest, ManifestSHA256: runtimecontract.ImageInventorySHA256(raw), Manifest: manifest}}}
	raw, _ = json.Marshal(value)
	return string(raw), runtimecontract.ImageInventorySHA256(raw)
}

// TestArtifactInventoryDoesNotTrustDeclaredToolsOrForeignBinding accepts the exact synthetic
// inventory but rejects foreign image/spec/runtime/hash and fabricated declared tools, and checks
// that historical absence remains absent without recipe-based evidence.
// TestArtifactInventoryDoesNotTrustDeclaredToolsOrForeignBinding принимает точный синтетический
// inventory, отклоняет чужие image/spec/runtime/hash и выдуманные declared tools и проверяет
// сохранение исторического отсутствия без evidence из деклараций рецепта.
func TestArtifactInventoryDoesNotTrustDeclaredToolsOrForeignBinding(t *testing.T) {
	digest := strings.Repeat("a", 64)
	artifact := entity.ImageArtifact{SpecSHA256: digest, ImmutableBuildSHA256: digest, RoleRuntimeContractSHA256: digest, ManifestDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}}}
	raw, hash := imageInventoryFixture(artifact)
	artifact.ToolInventorySHA256 = hash
	if hydrateArtifactToolInventory(&artifact, raw) != nil || artifact.ToolInventory == nil {
		t.Fatal("trusted bound inventory rejected")
	}
	for _, mutate := range []func(*entity.ImageArtifact){
		func(v *entity.ImageArtifact) { v.ManifestDigest = "sha256:" + strings.Repeat("b", 64) },
		func(v *entity.ImageArtifact) { v.SpecSHA256 = strings.Repeat("b", 64) },
		func(v *entity.ImageArtifact) { v.RoleRuntimeContractSHA256 = strings.Repeat("b", 64) },
		func(v *entity.ImageArtifact) { v.ToolInventorySHA256 = strings.Repeat("b", 64) },
		func(v *entity.ImageArtifact) {
			v.DeclaredTools = []entity.RoleImageTool{{Name: "git", Version: "2.0", SHA256: digest}}
		},
	} {
		v := artifact
		mutate(&v)
		if hydrateArtifactToolInventory(&v, raw) == nil {
			t.Fatal("foreign or fake declared tool accepted")
		}
	}
	legacy := entity.ImageArtifact{DeclaredTools: []entity.RoleImageTool{{Name: "git", Version: "2.0"}}}
	if hydrateArtifactToolInventory(&legacy, "") != nil || legacy.ToolInventory != nil {
		t.Fatal("declarations became inventory")
	}
}

// TestEnvironmentSelectedToolsUseVerifiedPlatformIntersection checks that a verified ripgrep probe
// selects executable rg, not its display name, and that a missing observation on another platform
// or nil inventory removes command eligibility.
// TestEnvironmentSelectedToolsUseVerifiedPlatformIntersection проверяет выбор executable rg по
// VERIFIED probe ripgrep без выдачи полномочий display name и удаление команды при MISSING на
// другой платформе либо nil inventory.
func TestEnvironmentSelectedToolsUseVerifiedPlatformIntersection(t *testing.T) {
	digest := strings.Repeat("a", 64)
	artifact := entity.ImageArtifact{SpecSHA256: digest, ImmutableBuildSHA256: digest, RoleRuntimeContractSHA256: digest, ManifestDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}}}
	raw, _ := imageInventoryFixture(artifact)
	inventory, err := runtimecontract.DecodeImageToolInventory([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	for index, probe := range runtimecontract.ImageToolProbes() {
		if probe.Name == "ripgrep" {
			inventory.Platforms[0].Manifest.Tools[index] = runtimecontract.ImageToolObservation{Name: probe.Name, Status: "VERIFIED", Path: probe.Paths[0], Version: "15.0.0", SHA256: digest, Required: probe.Required}
		}
	}
	manifest, _ := json.Marshal(inventory.Platforms[0].Manifest)
	inventory.Platforms[0].ManifestSHA256 = runtimecontract.ImageInventorySHA256(manifest)
	commands := verifiedImageToolCommands(&inventory)
	if _, ok := commands["rg"]; !ok || len(commands) != 1 {
		t.Fatal("real executable alias not selected")
	}
	if _, ok := commands["ripgrep"]; ok {
		t.Fatal("display tool name became executable authority")
	}
	artifact.DeclaredTools = []entity.RoleImageTool{{Name: "rg", Version: "15.0.0", SHA256: digest}}
	if !inventoryMatchesArtifact(inventory, artifact) {
		t.Fatal("exact installed OCI executable alias rejected")
	}
	artifact.DeclaredTools[0].Name = "ripgrep"
	if inventoryMatchesArtifact(inventory, artifact) {
		t.Fatal("display name became installed OCI executable")
	}
	second := inventory.Platforms[0]
	second.Manifest.Platform = "linux/arm64"
	second.Manifest.Tools = append([]runtimecontract.ImageToolObservation(nil), second.Manifest.Tools...)
	for index, tool := range second.Manifest.Tools {
		if tool.Name == "ripgrep" {
			second.Manifest.Tools[index] = runtimecontract.ImageToolObservation{Name: "ripgrep", Status: "MISSING", Required: true}
		}
	}
	manifest, _ = json.Marshal(second.Manifest)
	second.ManifestSHA256 = runtimecontract.ImageInventorySHA256(manifest)
	inventory.Platforms = append(inventory.Platforms, second)
	if len(verifiedImageToolCommands(&inventory)) != 0 || len(verifiedImageToolCommands(nil)) != 0 {
		t.Fatal("missing platform or historical declaration became selected tool")
	}
}
