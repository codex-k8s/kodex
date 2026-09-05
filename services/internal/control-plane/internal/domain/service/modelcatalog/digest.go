// Package modelcatalog вычисляет identity наблюдаемого каталога без локального списка моделей.
package modelcatalog

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Digest связывает полный безопасный каталог, не его страницу или поисковый срез.
func Digest(models []entity.ModelCapability, sources ...string) (string, error) {
	items := make([]entity.ModelCapability, len(models))
	for i, item := range models {
		item.ReasoningEfforts = append([]string{}, item.ReasoningEfforts...)
		item.EligibleProviderAccountRefs = append([]string{}, item.EligibleProviderAccountRefs...)
		item.ReadinessBlockers = append([]string{}, item.ReadinessBlockers...)
		slices.Sort(item.EligibleProviderAccountRefs)
		slices.Sort(item.ReadinessBlockers)
		items[i] = item
	}
	slices.SortFunc(items, func(a, b entity.ModelCapability) int {
		if result := cmp.Compare(a.ProviderDefinitionKey, b.ProviderDefinitionKey); result != 0 {
			return result
		}
		return cmp.Compare(a.ID, b.ID)
	})
	sourceVersions := append([]string{}, sources...)
	slices.Sort(sourceVersions)
	raw, err := json.Marshal(struct {
		Version int
		Models  []entity.ModelCapability
		Sources []string
	}{1, items, sourceVersions})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
