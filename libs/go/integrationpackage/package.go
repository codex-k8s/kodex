// Package integrationpackage загружает и проверяет versioned integration packages.
package integrationpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	APIVersion = "integrations.kodex.io/v1"
	Kind       = "IntegrationPackage"
	Origin     = "SHIPPED"
	maxBytes   = 256 << 10
)

var (
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
)

type Package struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
	Digest     string   `yaml:"-" json:"-"`
}

type Metadata struct {
	Key     string `yaml:"key" json:"key"`
	Version string `yaml:"version" json:"version"`
	Origin  string `yaml:"origin" json:"origin"`
}

type Spec struct {
	Name                string       `yaml:"name" json:"name"`
	Description         string       `yaml:"description" json:"description"`
	Category            string       `yaml:"category" json:"category"`
	Adapter             string       `yaml:"adapter" json:"adapter"`
	Credential          *Credential  `yaml:"credential,omitempty" json:"credential,omitempty"`
	ConfigurationFields []Field      `yaml:"configurationFields" json:"configurationFields"`
	Capabilities        []Capability `yaml:"capabilities" json:"capabilities"`
}

type Credential struct {
	SecretKey string `yaml:"secretKey" json:"secretKey"`
}

type Field struct {
	Key           string `yaml:"key" json:"key"`
	Type          string `yaml:"type" json:"type"`
	Required      bool   `yaml:"required" json:"required"`
	MaximumLength int    `yaml:"maximumLength,omitempty" json:"maximumLength,omitempty"`
	Minimum       int64  `yaml:"minimum,omitempty" json:"minimum,omitempty"`
	Maximum       int64  `yaml:"maximum,omitempty" json:"maximum,omitempty"`
}

type ResourceScope struct {
	Kind             string   `yaml:"kind" json:"kind"`
	ConnectionFields []string `yaml:"connectionFields" json:"connectionFields"`
}

type Capability struct {
	Key            string        `yaml:"key" json:"key"`
	Operation      string        `yaml:"operation" json:"operation"`
	Risk           string        `yaml:"risk" json:"risk"`
	ApprovalPolicy string        `yaml:"approvalPolicy" json:"approvalPolicy"`
	ResourceScope  ResourceScope `yaml:"resourceScope" json:"resourceScope"`
	InputFields    []Field       `yaml:"inputFields" json:"inputFields"`
}

func Parse(raw []byte) (Package, error) {
	if len(raw) == 0 || len(raw) > maxBytes {
		return Package{}, errors.New("integration package size is invalid")
	}
	var document yaml.Node
	nodeDecoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := nodeDecoder.Decode(&document); err != nil {
		return Package{}, errors.New("decode integration package YAML")
	}
	if err := rejectUnsafeYAML(&document); err != nil {
		return Package{}, err
	}
	var trailing yaml.Node
	if err := nodeDecoder.Decode(&trailing); err != io.EOF {
		return Package{}, errors.New("integration package must contain one YAML document")
	}

	var result Package
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return Package{}, errors.New("decode strict integration package YAML")
	}
	if err := validate(&result); err != nil {
		return Package{}, err
	}
	canonical, err := json.Marshal(result)
	if err != nil {
		return Package{}, errors.New("encode canonical integration package")
	}
	digest := sha256.Sum256(canonical)
	result.Digest = hex.EncodeToString(digest[:])
	return result, nil
}

func LoadShipped() (map[string]Package, error) {
	result := make(map[string]Package, len(shippedYAML))
	for filename, raw := range shippedYAML {
		definition, err := Parse([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("load shipped integration package %s: %w", filename, err)
		}
		if _, exists := result[definition.Metadata.Key]; exists {
			return nil, errors.New("duplicate shipped integration package key")
		}
		result[definition.Metadata.Key] = definition
	}
	return result, nil
}

func Sorted(packages map[string]Package) []Package {
	keys := make([]string, 0, len(packages))
	for key := range packages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Package, 0, len(keys))
	for _, key := range keys {
		result = append(result, packages[key])
	}
	return result
}

func (definition Package) Capability(key string) (Capability, bool) {
	for _, capability := range definition.Spec.Capabilities {
		if capability.Key == key {
			return capability, true
		}
	}
	return Capability{}, false
}

// ValidateConfiguration проверяет public configuration без credential values.
func (definition Package) ValidateConfiguration(configuration map[string]string) error {
	fields := make(map[string]Field, len(definition.Spec.ConfigurationFields))
	for _, field := range definition.Spec.ConfigurationFields {
		fields[field.Key] = field
	}
	for key, raw := range configuration {
		field, exists := fields[key]
		if !exists || validateStringValue(field, raw) != nil {
			return errors.New("integration configuration is invalid")
		}
	}
	for _, field := range definition.Spec.ConfigurationFields {
		if _, exists := configuration[field.Key]; field.Required && !exists {
			return errors.New("integration configuration required field is missing")
		}
	}
	return nil
}

// ResourceScope строит exact scope только из проверенной connection configuration.
func (capability Capability) ResourceScopeValues(configuration map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(capability.ResourceScope.ConnectionFields))
	for _, key := range capability.ResourceScope.ConnectionFields {
		value, exists := configuration[key]
		if !exists || value == "" {
			return nil, errors.New("integration resource scope is incomplete")
		}
		result[key] = value
	}
	return result, nil
}

// ValidateInput принимает только одно JSON object с закрытым набором primitive fields.
func (capability Capability) ValidateInput(raw []byte) ([]byte, error) {
	values, err := decodeJSONObject(raw)
	if err != nil {
		return nil, errors.New("integration input is invalid")
	}
	fields := make(map[string]Field, len(capability.InputFields))
	for _, field := range capability.InputFields {
		fields[field.Key] = field
	}
	normalized := make(map[string]any, len(values))
	for key, rawValue := range values {
		field, exists := fields[key]
		if !exists {
			return nil, errors.New("integration input contains unknown field")
		}
		value, valueErr := decodeFieldValue(field, rawValue)
		if valueErr != nil {
			return nil, errors.New("integration input field is invalid")
		}
		normalized[key] = value
	}
	for _, field := range capability.InputFields {
		if _, exists := values[field.Key]; field.Required && !exists {
			return nil, errors.New("integration input required field is missing")
		}
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return nil, errors.New("encode canonical integration input")
	}
	return canonical, nil
}

func decodeJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxBytes {
		return nil, errors.New("JSON object size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("JSON object is required")
	}
	result := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		key, ok := keyToken.(string)
		if keyErr != nil || !ok || !validKey(key) {
			return nil, errors.New("JSON object key is invalid")
		}
		if _, exists := result[key]; exists {
			return nil, errors.New("JSON object key is duplicated")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("JSON object is incomplete")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("JSON object has trailing content")
	}
	return result, nil
}

func decodeFieldValue(field Field, raw json.RawMessage) (any, error) {
	switch field.Type {
	case "STRING":
		var value string
		if json.Unmarshal(raw, &value) != nil || validateStringValue(field, value) != nil {
			return nil, errors.New("string field is invalid")
		}
		return value, nil
	case "INTEGER":
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&number) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return nil, errors.New("integer field is invalid")
		}
		value, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil || value < field.Minimum || (field.Maximum != 0 && value > field.Maximum) {
			return nil, errors.New("integer field is outside bounds")
		}
		return value, nil
	case "BOOLEAN":
		var value bool
		if json.Unmarshal(raw, &value) != nil || (string(raw) != "true" && string(raw) != "false") {
			return nil, errors.New("boolean field is invalid")
		}
		return value, nil
	default:
		return nil, errors.New("field type is invalid")
	}
}

func validateStringValue(field Field, value string) error {
	if value == "" || len(value) > field.MaximumLength || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("string field is outside bounds")
	}
	return nil
}

func validate(result *Package) error {
	if result.APIVersion != APIVersion || result.Kind != Kind || result.Metadata.Origin != Origin ||
		!validKey(result.Metadata.Key) || !versionPattern.MatchString(result.Metadata.Version) || len(result.Metadata.Version) > 32 ||
		len(result.Spec.Name) == 0 || len(result.Spec.Name) > 120 || len(result.Spec.Description) == 0 || len(result.Spec.Description) > 500 ||
		!validKey(result.Spec.Category) || !oneOf(result.Spec.Adapter, "SYNTHETIC_HTTP", "GITHUB", "MATTERMOST_INTERACTION") ||
		len(result.Spec.ConfigurationFields) > 16 || len(result.Spec.Capabilities) == 0 || len(result.Spec.Capabilities) > 32 {
		return errors.New("integration package metadata or bounds are invalid")
	}
	if result.Spec.Credential != nil && !validKey(result.Spec.Credential.SecretKey) {
		return errors.New("integration package credential is invalid")
	}
	configurationKeys, err := validateFields(result.Spec.ConfigurationFields)
	if err != nil {
		return err
	}
	capabilityKeys := map[string]struct{}{}
	for _, capability := range result.Spec.Capabilities {
		if !validKey(capability.Key) || !validKey(capability.Operation) ||
			!oneOf(capability.Risk, "READ", "WRITE", "SENSITIVE", "DESTRUCTIVE") ||
			!oneOf(capability.ApprovalPolicy, "NONE", "HUMAN_EACH_EFFECT") ||
			(capability.Risk == "READ") != (capability.ApprovalPolicy == "NONE") ||
			!oneOf(capability.ResourceScope.Kind, "SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "MATTERMOST_CHANNEL") ||
			len(capability.ResourceScope.ConnectionFields) == 0 || len(capability.ResourceScope.ConnectionFields) > 8 ||
			len(capability.InputFields) > 16 {
			return errors.New("integration package capability is invalid")
		}
		if _, exists := capabilityKeys[capability.Key]; exists {
			return errors.New("integration package capability key is duplicated")
		}
		capabilityKeys[capability.Key] = struct{}{}
		scopeFields := map[string]struct{}{}
		for _, key := range capability.ResourceScope.ConnectionFields {
			if _, exists := configurationKeys[key]; !exists {
				return errors.New("integration package scope references unknown configuration field")
			}
			if _, exists := scopeFields[key]; exists {
				return errors.New("integration package scope field is duplicated")
			}
			scopeFields[key] = struct{}{}
		}
		if _, err := validateFields(capability.InputFields); err != nil {
			return err
		}
	}
	return nil
}

func validateFields(fields []Field) (map[string]struct{}, error) {
	keys := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !validKey(field.Key) || !oneOf(field.Type, "STRING", "INTEGER", "BOOLEAN") ||
			field.MaximumLength < 0 || field.MaximumLength > 65536 || field.Minimum < 0 || field.Maximum < 0 ||
			(field.Maximum != 0 && field.Maximum < field.Minimum) ||
			(field.Type == "STRING" && field.MaximumLength == 0) ||
			(field.Type != "STRING" && field.MaximumLength != 0) ||
			(field.Type != "INTEGER" && (field.Minimum != 0 || field.Maximum != 0)) {
			return nil, errors.New("integration package field is invalid")
		}
		if _, exists := keys[field.Key]; exists {
			return nil, errors.New("integration package field key is duplicated")
		}
		keys[field.Key] = struct{}{}
	}
	return keys, nil
}

func rejectUnsafeYAML(node *yaml.Node) error {
	if node == nil {
		return errors.New("integration package YAML is empty")
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" || node.Alias != nil ||
		(node.Kind == yaml.ScalarNode && node.Value == "<<") {
		return errors.New("integration package YAML aliases, anchors, and merge keys are forbidden")
	}
	if node.Kind == yaml.MappingNode {
		keys := map[string]struct{}{}
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return errors.New("integration package YAML mapping key must be a string")
			}
			if _, exists := keys[key.Value]; exists {
				return errors.New("integration package YAML mapping key is duplicated")
			}
			keys[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := rejectUnsafeYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func validKey(value string) bool { return len(value) <= 120 && keyPattern.MatchString(value) }

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) && value == candidate {
			return true
		}
	}
	return false
}
