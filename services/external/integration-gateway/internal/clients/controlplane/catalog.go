package controlplane

import (
	"errors"
	"fmt"
	"sync"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

type Catalog struct {
	mu          sync.RWMutex
	definitions map[string]entity.Definition
	exposed     map[string]string
}

func NewCatalog() *Catalog {
	return &Catalog{definitions: make(map[string]entity.Definition), exposed: make(map[string]string)}
}

func (catalog *Catalog) Store(definition entity.Definition) error {
	if definition.ID == "" || definition.Version == 0 || definition.Digest == "" {
		return errors.New("integration definition is invalid")
	}
	key := catalogKey(definition.ID, definition.Version)
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if current, exists := catalog.definitions[key]; exists && current.Digest != definition.Digest {
		return errors.New("integration definition version is immutable")
	}
	for _, tool := range definition.Tools {
		if owner, exists := catalog.exposed[tool.Name]; exists && owner != key {
			return errors.New("integration exposed tool name is duplicated")
		}
	}
	for _, tool := range definition.Tools {
		catalog.exposed[tool.Name] = key
	}
	catalog.definitions[key] = definition
	return nil
}

func (catalog *Catalog) Get(reference string, version uint64) (entity.Definition, bool) {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	value, ok := catalog.definitions[catalogKey(reference, version)]
	return value, ok
}

func catalogKey(reference string, version uint64) string {
	return fmt.Sprintf("%s@%d", reference, version)
}
