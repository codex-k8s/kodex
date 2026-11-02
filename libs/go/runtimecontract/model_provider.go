package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// ModelProviderConfigurationSchema identifies the closed deployment-owned provider contract.
	// ModelProviderConfigurationSchema обозначает закрытый контракт провайдера, принадлежащий установке.
	ModelProviderConfigurationSchema = "kodex.model-provider/v1"
	// OpenCodeVersion pins the only OpenCode executable/API version accepted by this adapter.
	// OpenCodeVersion закрепляет единственную версию executable/API OpenCode, принимаемую адаптером.
	OpenCodeVersion = "1.19.5"
	// MaximumModelProviderConfigurationBytes bounds nonsecret provider configuration before parsing.
	// MaximumModelProviderConfigurationBytes ограничивает несекретную конфигурацию до разбора.
	MaximumModelProviderConfigurationBytes = 32 << 10
)

// ErrModelProviderConfiguration rejects invalid provider metadata without echoing endpoint or payload.
// ErrModelProviderConfiguration отклоняет неверные метаданные без вывода endpoint или содержимого.
var ErrModelProviderConfiguration = errors.New("model provider configuration is invalid")

// ModelProviderConfiguration is deployment-owned, nonsecret input pinned in each runtime revision.
// It grants no credentials or network access by itself; consumers also require their signed grants.
// ModelProviderConfiguration — несекретный вход установки, закрепляемый каждой runtime revision.
// Сам по себе он не выдаёт credentials или сетевой доступ: consumers также проверяют signed grants.
type ModelProviderConfiguration struct {
	Schema         string               `json:"schema"`
	ManagedBy      string               `json:"managed_by"`
	SourceRevision string               `json:"source_revision"`
	Engine         string               `json:"engine"`
	EngineVersion  string               `json:"engine_version"`
	ToolMode       string               `json:"tool_mode"`
	ProviderID     string               `json:"provider_id"`
	BaseURL        string               `json:"base_url"`
	AddressCIDRs   []string             `json:"address_cidrs"`
	Models         []ModelProviderModel `json:"models"`
}

// ModelProviderModel declares an allowlisted alias and conservative operator-owned request ceilings.
// These ceilings do not prove upstream capabilities, quota or model context size.
// ModelProviderModel задаёт разрешённый alias и консервативные пределы запросов владельца установки.
// Эти пределы не доказывают возможности upstream, квоту или размер контекста модели.
type ModelProviderModel struct {
	ID                  string `json:"id"`
	ContextWindowTokens int    `json:"context_window_tokens"`
	MaximumOutputTokens int    `json:"maximum_output_tokens"`
}

// ModelProviderRoute is the small nonsecret transport projection carried in a signed proxy grant.
// Its digest binds the complete provider configuration without copying the model catalog into headers.
// ModelProviderRoute — малая несекретная transport projection внутри signed proxy grant.
// Digest связывает полную конфигурацию без копирования каталога моделей в headers.
type ModelProviderRoute struct {
	BaseURL             string   `json:"base_url"`
	AddressCIDRs        []string `json:"address_cidrs"`
	ConfigurationSHA256 string   `json:"configuration_sha256"`
}

// DecodeModelProviderConfiguration accepts one bounded object, rejecting duplicate/unknown keys.
// It never reads environment variables, files, credentials or remote model catalogs.
// DecodeModelProviderConfiguration принимает один bounded object, отклоняя duplicate/unknown keys.
// Она не читает окружение, файлы, credentials или удалённые каталоги моделей.
func DecodeModelProviderConfiguration(raw []byte) (ModelProviderConfiguration, error) {
	var config ModelProviderConfiguration
	trimmed := bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > MaximumModelProviderConfigurationBytes || !utf8.Valid(raw) || len(trimmed) == 0 || trimmed[0] != '{' ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 8) != nil {
		return config, ErrModelProviderConfiguration
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || config.Validate() != nil {
		return ModelProviderConfiguration{}, ErrModelProviderConfiguration
	}
	return config, nil
}

// Validate enforces the pinned engine, exact HTTPS authority, host-address pins and bounded aliases.
// It performs no DNS lookup and accepts no wildcard destination, secret or caller-selected header.
// Validate проверяет закреплённый engine, exact HTTPS authority, адресные pins и bounded aliases.
// DNS lookup не выполняется; wildcard destination, secret и заголовки вызывающего не принимаются.
func (config ModelProviderConfiguration) Validate() error {
	if config.Schema != ModelProviderConfigurationSchema || config.ManagedBy != "git" ||
		len(config.SourceRevision) != 40 || !lowerHex(config.SourceRevision) ||
		config.Engine != "opencode" || config.EngineVersion != OpenCodeVersion || config.ToolMode != "managed_mcp" ||
		!ValidDNSLabel(config.ProviderID) || len(config.ProviderID) > 63 ||
		len(config.AddressCIDRs) < 1 || len(config.AddressCIDRs) > 32 ||
		len(config.Models) < 1 || len(config.Models) > 128 {
		return ErrModelProviderConfiguration
	}
	if _, err := modelProviderEndpoint(config.BaseURL); err != nil {
		return err
	}
	if !validModelProviderAddresses(config.AddressCIDRs) {
		return ErrModelProviderConfiguration
	}
	models := map[string]bool{}
	for _, model := range config.Models {
		if model.ID == "" || len(model.ID) > 128 || strings.TrimSpace(model.ID) != model.ID ||
			strings.IndexFunc(model.ID, unicode.IsControl) >= 0 || !utf8.ValidString(model.ID) || models[model.ID] ||
			model.ContextWindowTokens < 1 || model.ContextWindowTokens > 2_000_000 ||
			model.MaximumOutputTokens < 1 || model.MaximumOutputTokens > model.ContextWindowTokens {
			return ErrModelProviderConfiguration
		}
		models[model.ID] = true
	}
	return nil
}

// Digest returns the canonical typed configuration hash after validation, excluding no source field.
// Digest возвращает hash канонической typed конфигурации после проверки, сохраняя все source fields.
func (config ModelProviderConfiguration) Digest() (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", ErrModelProviderConfiguration
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// Model returns only an exactly configured alias; it does not infer limits or select a fallback.
// Model возвращает только exact configured alias, не выводит пределы и не выбирает fallback.
func (config ModelProviderConfiguration) Model(id string) (ModelProviderModel, bool) {
	for _, model := range config.Models {
		if model.ID == id {
			return model, true
		}
	}
	return ModelProviderModel{}, false
}

// validateModelProvider closes alternate-engine inputs unless their exact descriptor and model match.
// An absent descriptor preserves the legacy validation path; it never enables OpenCode implicitly.
// validateModelProvider отклоняет alternate engine без exact descriptor и model.
// Отсутствующий descriptor сохраняет legacy проверку и не включает OpenCode неявно.
func (input RunnerInput) validateModelProvider() error {
	if input.ModelProvider == nil {
		if input.Provider == "opencode" {
			return ErrModelProviderConfiguration
		}
		return nil
	}
	if input.Provider != "opencode" || input.ModelProvider.Validate() != nil {
		return ErrModelProviderConfiguration
	}
	if input.CodexSessionID != "" || len(input.EnvironmentTools) != 0 || input.ConfigOverlay != "" ||
		input.EffectiveReasoningEffort != "" || input.ReasoningMode != ReasoningUnsupported {
		return ErrModelProviderConfiguration
	}
	if _, found := input.ModelProvider.Model(input.Model); !found {
		return ErrModelProviderConfiguration
	}
	return nil
}

// AllowsRequest matches only the configured Chat Completions and model-list HTTP tuples.
// Callers separately enforce HTTPS/443, exact address pins and provider-access authority.
// AllowsRequest сопоставляет только configured HTTP tuples Chat Completions и model list.
// Вызывающий отдельно проверяет HTTPS/443, exact address pins и provider-access authority.
func (config ModelProviderConfiguration) AllowsRequest(host, path, method string) bool {
	if config.Validate() != nil {
		return false
	}
	endpoint, err := modelProviderEndpoint(config.BaseURL)
	if err != nil || host != endpoint.Hostname() {
		return false
	}
	return method == "POST" && path == endpoint.Path+"/chat/completions" ||
		method == "GET" && path == endpoint.Path+"/models"
}

// Route copies the validated transport pins and complete configuration digest for a grant producer.
// Route копирует проверенные transport pins и полный configuration digest для grant producer.
func (config ModelProviderConfiguration) Route() (ModelProviderRoute, error) {
	digest, err := config.Digest()
	if err != nil {
		return ModelProviderRoute{}, err
	}
	return ModelProviderRoute{BaseURL: config.BaseURL, AddressCIDRs: append([]string(nil), config.AddressCIDRs...),
		ConfigurationSHA256: digest}, nil
}

// Validate checks route metadata; a valid route alone is not evidence of a verified proxy grant.
// Validate проверяет route metadata; корректный route сам по себе не доказывает verified proxy grant.
func (route ModelProviderRoute) Validate() error {
	if !sha256Pattern.MatchString(route.ConfigurationSHA256) || !validModelProviderAddresses(route.AddressCIDRs) {
		return ErrModelProviderConfiguration
	}
	_, err := modelProviderEndpoint(route.BaseURL)
	return err
}

// AllowsHost matches the exact validated gateway authority without resolving DNS or granting web access.
// AllowsHost сопоставляет exact gateway authority без DNS lookup или выдачи web access.
func (route ModelProviderRoute) AllowsHost(host string) bool {
	if route.Validate() != nil {
		return false
	}
	endpoint, err := modelProviderEndpoint(route.BaseURL)
	return err == nil && host == endpoint.Hostname()
}

// AllowsRequest limits an already authorized provider connection to Chat Completions and model list.
// AllowsRequest ограничивает уже разрешённое соединение маршрутами Chat Completions и model list.
func (route ModelProviderRoute) AllowsRequest(host, path, method string) bool {
	if !route.AllowsHost(host) {
		return false
	}
	endpoint, _ := modelProviderEndpoint(route.BaseURL)
	return method == "POST" && path == endpoint.Path+"/chat/completions" ||
		method == "GET" && path == endpoint.Path+"/models"
}

// AllowsAddress accepts only exact signed host-address pins; aliases and unlisted DNS answers fail.
// AllowsAddress принимает только exact signed address pins; aliases и лишние DNS answers отклоняются.
func (route ModelProviderRoute) AllowsAddress(address netip.Addr) bool {
	if route.Validate() != nil || !address.IsValid() || address.Is4In6() {
		return false
	}
	for _, value := range route.AddressCIDRs {
		prefix, _ := netip.ParsePrefix(value)
		if prefix.Addr() == address {
			return true
		}
	}
	return false
}

// validModelProviderAddresses accepts a bounded unique list of canonical unicast /32 or /128 pins.
// validModelProviderAddresses принимает bounded unique list canonical unicast /32 или /128 pins.
func validModelProviderAddresses(values []string) bool {
	if len(values) < 1 || len(values) > 32 {
		return false
	}
	addresses := map[netip.Addr]bool{}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.String() != value || prefix.Bits() != prefix.Addr().BitLen() ||
			!prefix.Addr().IsGlobalUnicast() || prefix.Addr().IsLoopback() ||
			prefix.Addr().IsLinkLocalUnicast() || prefix.Addr().Is4In6() || addresses[prefix.Addr()] {
			return false
		}
		addresses[prefix.Addr()] = true
	}
	return true
}

// modelProviderEndpoint accepts one canonical HTTPS DNS base URL without credential or route aliases.
// modelProviderEndpoint принимает canonical HTTPS DNS base URL без credentials и route aliases.
func modelProviderEndpoint(value string) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil || len(value) > 512 || endpoint.Scheme != "https" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.RawFragment != "" ||
		endpoint.Opaque != "" || endpoint.RawPath != "" || strings.ContainsAny(value, "%\\\r\n\t\x00") ||
		endpoint.Port() != "" && endpoint.Port() != "443" || endpoint.Hostname() != strings.ToLower(endpoint.Hostname()) ||
		endpoint.Host != endpoint.Hostname() && endpoint.Host != endpoint.Hostname()+":443" ||
		net.ParseIP(endpoint.Hostname()) != nil || !strings.Contains(endpoint.Hostname(), ".") ||
		!validRuntimeDomainPattern(endpoint.Hostname()) || strings.HasPrefix(endpoint.Hostname(), "*.") ||
		strings.HasSuffix(endpoint.Hostname(), ".") || strings.HasSuffix(endpoint.Path, "/") ||
		strings.Contains(endpoint.Path, "//") || strings.Contains(endpoint.Path, "/.") || strings.Contains(endpoint.Path, ";") {
		return nil, ErrModelProviderConfiguration
	}
	return endpoint, nil
}

// lowerHex accepts nonempty lowercase hexadecimal strings; length is checked by the caller.
// lowerHex принимает непустой lowercase hex; длину отдельно проверяет вызывающий.
func lowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
