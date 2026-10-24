package platform

import (
	"errors"
	"path"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// hydrateArtifactToolInventory preserves historical absence when both raw inventory and its digest
// are empty; otherwise strictly decodes and checks the raw digest and artifact binding before
// assigning ToolInventory. Recipe declarations never synthesize missing evidence, and failure
// leaves the existing inventory pointer unchanged.
// hydrateArtifactToolInventory сохраняет историческое отсутствие evidence, когда пусты inventory и
// его digest; иначе строго декодирует данные и проверяет raw digest и связь с артефактом до
// присваивания ToolInventory. Декларации рецепта не создают отсутствующее evidence, отказ
// оставляет прежний inventory pointer.
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

// verifiedImageToolCommands returns a fresh set of actual executable basenames verified on every
// inventory platform, for environments without a platform pin. Nil or invalid inventory yields an
// empty set; display names, missing probes and recipe declarations do not select commands.
// verifiedImageToolCommands возвращает новый набор basename исполняемых файлов, проверенных на
// каждой платформе inventory, для окружений без platform pin. Nil либо неверный inventory даёт
// пустой набор; display names, отсутствующие probes и декларации рецепта не выбирают команды.
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

// inventoryMatchesArtifact checks image/provenance, platform count and each observed platform
// membership plus specification/build/runtime digests, requiring every declared executable
// name/version/hash to be VERIFIED there. It does not validate inventory shape or uniqueness
// itself; strict decoding is a separate caller requirement.
// inventoryMatchesArtifact проверяет image/provenance, количество платформ и принадлежность каждой
// наблюдаемой платформы, а также specification/build/runtime digests; требует VERIFIED для имени,
// версии и hash каждого заявленного executable. Самостоятельно не проверяет форму либо
// уникальность inventory; строгий decoder остаётся отдельным требованием вызывающей стороны.
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
