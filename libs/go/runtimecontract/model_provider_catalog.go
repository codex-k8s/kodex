package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"slices"
	"unicode/utf8"
)

const (
	// ModelProviderCatalogSchema identifies the only installation catalog format; a standalone runtime descriptor is not a catalog.
	// ModelProviderCatalogSchema обозначает единственный формат каталога установки; отдельный runtime descriptor не является каталогом.
	ModelProviderCatalogSchema = "kodex.model-provider-catalog/v1"
	// MaximumModelProviderCatalogBytes bounds the complete nonsecret catalog before decoding.
	// MaximumModelProviderCatalogBytes ограничивает полный несекретный каталог до разбора.
	MaximumModelProviderCatalogBytes = 512 << 10
	// MaximumModelProviders limits installation routes independently of per-route model limits.
	// MaximumModelProviders ограничивает маршруты установки независимо от лимитов моделей каждого маршрута.
	MaximumModelProviders = 16
)

// ModelProviderCatalog binds one immutable Git source and explicit default to a closed set of independent provider definitions.
// It carries no credentials or execution grants; each runtime snapshot still selects exactly one descriptor.
// ModelProviderCatalog связывает immutable Git source и явный default с закрытым набором независимых provider definitions.
// Каталог не содержит credentials или execution grants; каждый runtime snapshot выбирает ровно один descriptor.
type ModelProviderCatalog struct {
	Schema          string                       `json:"schema"`
	ManagedBy       string                       `json:"managed_by"`
	SourceRevision  string                       `json:"source_revision"`
	DefaultProvider string                       `json:"default_provider"`
	DefaultModel    string                       `json:"default_model"`
	Providers       []ModelProviderConfiguration `json:"providers"`
}

// DecodeModelProviderCatalog rejects old standalone documents, duplicate/unknown keys, trailing data and incomplete default bindings.
// DecodeModelProviderCatalog отклоняет старые одиночные документы, повторные/неизвестные ключи, хвостовые данные и неполные default bindings.
func DecodeModelProviderCatalog(raw []byte) (ModelProviderCatalog, error) {
	var result ModelProviderCatalog
	trimmed := bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > MaximumModelProviderCatalogBytes || !utf8.Valid(raw) || len(trimmed) == 0 || trimmed[0] != '{' || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 10) != nil {
		return result, ErrModelProviderConfiguration
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.Validate() != nil {
		return ModelProviderCatalog{}, ErrModelProviderConfiguration
	}
	return result, nil
}

// Validate verifies all child descriptors and the exact default; shared TLS hosts must use identical operator address sets so static catalog CONNECT cannot widen a route.
// Validate проверяет дочерние descriptors и точный default; общий TLS host требует одинаковый набор адресов, чтобы static catalog CONNECT не расширял маршрут.
func (catalog ModelProviderCatalog) Validate() error {
	if catalog.Schema != ModelProviderCatalogSchema || catalog.ManagedBy != "git" || len(catalog.Providers) < 1 || len(catalog.Providers) > MaximumModelProviders {
		return ErrModelProviderConfiguration
	}
	keys := make(map[string]bool, len(catalog.Providers))
	hosts := make(map[string][]string)
	foundDefault := false
	for _, provider := range catalog.Providers {
		raw, err := json.Marshal(provider)
		if err != nil || len(raw) > MaximumModelProviderConfigurationBytes || provider.Validate() != nil || provider.SourceRevision != catalog.SourceRevision || keys[provider.DefinitionKey()] {
			return ErrModelProviderConfiguration
		}
		keys[provider.DefinitionKey()] = true
		if provider.DefinitionKey() == catalog.DefaultProvider {
			if _, found := provider.Model(catalog.DefaultModel); !found {
				return ErrModelProviderConfiguration
			}
			foundDefault = true
		}
		endpoint, _ := url.Parse(provider.BaseURL)
		addresses := slices.Clone(provider.AddressCIDRs)
		slices.Sort(addresses)
		if previous, found := hosts[endpoint.Hostname()]; found && !slices.Equal(previous, addresses) {
			return ErrModelProviderConfiguration
		}
		hosts[endpoint.Hostname()] = addresses
	}
	if !foundDefault {
		return ErrModelProviderConfiguration
	}
	return nil
}

// Provider returns a detached exact definition, never a default fallback or a credential selected by model spelling.
// Provider возвращает отдельную копию точного definition без default fallback или выбора credential по имени модели.
func (catalog ModelProviderCatalog) Provider(definitionKey string) (*ModelProviderConfiguration, bool) {
	if catalog.Validate() != nil {
		return nil, false
	}
	for _, provider := range catalog.Providers {
		if provider.DefinitionKey() == definitionKey {
			detached, err := provider.Clone()
			if err != nil {
				return nil, false
			}
			return &detached, true
		}
	}
	return nil, false
}

// Clone validates and detaches every nested model policy so repository and broker startup state cannot alias caller slices.
// Clone проверяет и копирует все вложенные model policies, исключая общие slices с вызывающим кодом.
func (catalog ModelProviderCatalog) Clone() (ModelProviderCatalog, error) {
	if catalog.Validate() != nil {
		return ModelProviderCatalog{}, ErrModelProviderConfiguration
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return ModelProviderCatalog{}, ErrModelProviderConfiguration
	}
	return DecodeModelProviderCatalog(raw)
}
