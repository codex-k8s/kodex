// Package provider загружает закрытый version-pinned registry providers.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

const maximumCatalogBytes = 128 << 10

var stableKey = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type Catalog struct {
	path      string
	providers []entity.ProviderDescriptor
}

func LoadCatalog(path string) (*Catalog, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("provider catalog path is invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return nil, errors.New("provider catalog file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read provider catalog")
	}
	var file struct {
		Providers []entity.ProviderDescriptor `json:"providers"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || len(file.Providers) == 0 || len(file.Providers) > 16 {
		return nil, errors.New("provider catalog is invalid")
	}
	seen := make(map[string]struct{}, len(file.Providers))
	for _, descriptor := range file.Providers {
		if !stableKey.MatchString(descriptor.ID) || descriptor.Version == 0 || descriptor.Digest != "" || descriptor.DisplayName == "" || len(descriptor.AuthorizationModes) != 1 || descriptor.AuthorizationModes[0] != "CHATGPT_DEVICE_CODE" || len(descriptor.Capabilities) == 0 || len(descriptor.Capabilities) > 64 {
			return nil, errors.New("provider catalog entry is invalid")
		}
		if _, duplicate := seen[descriptor.ID]; duplicate {
			return nil, errors.New("provider catalog entry is duplicated")
		}
		seen[descriptor.ID] = struct{}{}
		for _, capability := range descriptor.Capabilities {
			if !stableKey.MatchString(capability.Name) || (capability.Risk != "LOW" && capability.Risk != "MEDIUM" && capability.Risk != "HIGH") {
				return nil, errors.New("provider capability is invalid")
			}
		}
	}
	return &Catalog{path: path, providers: slices.Clone(file.Providers)}, nil
}

func (catalog *Catalog) Providers() []entity.ProviderDescriptor {
	return slices.Clone(catalog.providers)
}

func (catalog *Catalog) Check(context.Context) error {
	info, err := os.Stat(catalog.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return errors.New("provider catalog is unavailable")
	}
	return nil
}
