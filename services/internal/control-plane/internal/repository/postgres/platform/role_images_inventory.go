package platform

import (
	"errors"
	"path"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Историческое отсутствие evidence не заполняется recipe declarations.
func hydrateArtifactToolInventory(artifact *entity.ImageArtifact, raw string) error {
	if raw == "" && artifact.ToolInventorySHA256 == "" {
		return nil
	}
	value, err := runtimecontract.DecodeImageToolInventory([]byte(raw))
	if err != nil || runtimecontract.ImageInventorySHA256([]byte(raw)) != artifact.ToolInventorySHA256 || !inventoryMatchesArtifact(value, *artifact) {
		return errors.New("image artifact tool inventory binding is invalid")
	}
	artifact.ToolInventory = &value
	return nil
}

// Environment без platform pin выбирает только verified intersection всех платформ.
// Command — реальный basename из закрытого probe path, не произвольное имя recipe.
func verifiedImageToolCommands(value *runtimecontract.ImageToolInventory) map[string]struct{} {
	result := map[string]struct{}{}
	if value == nil || value.Validate() != nil {
		return result
	}
	for index, platform := range value.Platforms {
		current := map[string]struct{}{}
		for _, tool := range platform.Manifest.Tools {
			if tool.Status == "VERIFIED" {
				current[path.Base(tool.Path)] = struct{}{}
			}
		}
		if index == 0 {
			result = current
			continue
		}
		for command := range result {
			if _, ok := current[command]; !ok {
				delete(result, command)
			}
		}
	}
	return result
}

func inventoryMatchesArtifact(value runtimecontract.ImageToolInventory, artifact entity.ImageArtifact) bool {
	if value.ImageDigest != artifact.ManifestDigest || value.ProvenanceSHA256 != artifact.ProvenanceSHA256 || len(value.Platforms) != len(artifact.Platforms) {
		return false
	}
	expected := map[string]bool{}
	for _, platform := range artifact.Platforms {
		name := platform.OS + "/" + platform.Architecture
		if platform.Variant != "" {
			name += "/" + platform.Variant
		}
		expected[name] = true
	}
	for _, platform := range value.Platforms {
		manifest := platform.Manifest
		if !expected[manifest.Platform] || manifest.SpecSHA256 != artifact.SpecSHA256 || manifest.ImmutableBuildSHA256 != artifact.ImmutableBuildSHA256 || manifest.RuntimeContractSHA256 != artifact.RoleRuntimeContractSHA256 {
			return false
		}
		// OCI inputs должны пройти те же fixed probes на каждой платформе.
		for _, declared := range artifact.DeclaredTools {
			found := false
			for _, tool := range manifest.Tools {
				found = found || (path.Base(tool.Path) == declared.Name && tool.Status == "VERIFIED" && tool.Version == declared.Version && tool.SHA256 == declared.SHA256)
			}
			if !found {
				return false
			}
		}
	}
	return true
}
