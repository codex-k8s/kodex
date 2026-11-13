package grpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestImageInputFeatureProjectionPreservesAdmittedEvidence checks the typed projection carries the full platform feature proof without inventing historical support.
// TestImageInputFeatureProjectionPreservesAdmittedEvidence проверяет сохранение полного доказательства платформы в typed projection без выдумывания исторической поддержки.
func TestImageInputFeatureProjectionPreservesAdmittedEvidence(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64", InputFeatures: []string{runtimecontract.ContextWindowInputFeature}}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	raw, _ := json.Marshal(manifest)
	inventory := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema, ImageDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: "sha256:" + digest, ManifestSHA256: runtimecontract.ImageInventorySHA256(raw), Manifest: manifest}}}
	if inventory.Validate() != nil {
		t.Fatal("invalid synthetic evidence")
	}
	projected := castImageToolInventory(&inventory, digest)
	if len(projected.Platforms) != 1 || len(projected.Platforms[0].InputFeatures) != 1 || projected.Platforms[0].InputFeatures[0] != runtimecontract.ContextWindowInputFeature || projected.Platforms[0].ManifestSha256 != inventory.Platforms[0].ManifestSHA256 {
		t.Fatal("admitted proof lost in typed inventory")
	}
	projected.Platforms[0].InputFeatures[0] = "changed"
	if !inventory.SupportsInputFeature(runtimecontract.ContextWindowInputFeature) {
		t.Fatal("projection aliased immutable owner evidence")
	}
	if len(castImageToolInventory(nil, "").GetPlatforms()) != 0 {
		t.Fatal("historical absence became support")
	}
}
