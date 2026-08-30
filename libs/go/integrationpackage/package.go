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
	"net"
	"net/mail"
	"net/url"
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
	Name                string               `yaml:"name" json:"name"`
	Description         string               `yaml:"description" json:"description"`
	Category            string               `yaml:"category" json:"category"`
	Adapter             string               `yaml:"adapter" json:"adapter"`
	Credential          *Credential          `yaml:"credential,omitempty" json:"credential,omitempty"`
	ConfigurationFields []Field              `yaml:"configurationFields" json:"configurationFields"`
	NetworkDestinations []NetworkDestination `yaml:"networkDestinations" json:"networkDestinations"`
	HealthCheck         HealthCheck          `yaml:"healthCheck" json:"healthCheck"`
	Capabilities        []Capability         `yaml:"capabilities" json:"capabilities"`
}

type Credential struct {
	SecretKey string `yaml:"secretKey" json:"secretKey"`
	Kind      string `yaml:"kind" json:"kind"`
}

type Field struct {
	Key           string   `yaml:"key" json:"key"`
	Type          string   `yaml:"type" json:"type"`
	Format        string   `yaml:"format,omitempty" json:"format,omitempty"`
	Required      bool     `yaml:"required" json:"required"`
	MaximumLength int      `yaml:"maximumLength,omitempty" json:"maximumLength,omitempty"`
	Minimum       int64    `yaml:"minimum,omitempty" json:"minimum,omitempty"`
	Maximum       int64    `yaml:"maximum,omitempty" json:"maximum,omitempty"`
	AllowedValues []string `yaml:"allowedValues,omitempty" json:"allowedValues,omitempty"`
}

type NetworkDestination struct {
	Key                string `yaml:"key" json:"key"`
	Source             string `yaml:"source" json:"source"`
	Hostname           string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ConfigurationField string `yaml:"configurationField,omitempty" json:"configurationField,omitempty"`
	Port               int    `yaml:"port" json:"port"`
	TLS                string `yaml:"tls" json:"tls"`
}

type HealthCheck struct {
	Operation      string `yaml:"operation" json:"operation"`
	TimeoutSeconds int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxAttempts    int    `yaml:"maxAttempts" json:"maxAttempts"`
}

type ResourceScope struct {
	Kind             string   `yaml:"kind" json:"kind"`
	ConnectionFields []string `yaml:"connectionFields" json:"connectionFields"`
}

type Capability struct {
	Key            string        `yaml:"key" json:"key"`
	Name           string        `yaml:"name" json:"name"`
	Description    string        `yaml:"description" json:"description"`
	Operation      string        `yaml:"operation" json:"operation"`
	Risk           string        `yaml:"risk" json:"risk"`
	ApprovalPolicy string        `yaml:"approvalPolicy" json:"approvalPolicy"`
	ResourceScope  ResourceScope `yaml:"resourceScope" json:"resourceScope"`
	InputFields    []Field       `yaml:"inputFields" json:"inputFields"`
	OutputFields   []Field       `yaml:"outputFields" json:"outputFields"`
	Execution      Execution     `yaml:"execution" json:"execution"`
}

type Execution struct {
	Idempotency              string `yaml:"idempotency" json:"idempotency"`
	TimeoutSeconds           int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxAttempts              int    `yaml:"maxAttempts" json:"maxAttempts"`
	RetryBackoffMilliseconds int    `yaml:"retryBackoffMilliseconds" json:"retryBackoffMilliseconds"`
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
		if !exists || validateStringValue(field, raw, false) != nil {
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
	return validateObject(raw, capability.InputFields, "input")
}

// ValidateOutput принимает только безопасную проекцию с закрытым набором полей.
func (capability Capability) ValidateOutput(raw []byte) ([]byte, error) {
	return validateObject(raw, capability.OutputFields, "output")
}

func validateObject(raw []byte, declared []Field, kind string) ([]byte, error) {
	values, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("integration %s is invalid", kind)
	}
	fields := make(map[string]Field, len(declared))
	for _, field := range declared {
		fields[field.Key] = field
	}
	normalized := make(map[string]any, len(values))
	for key, rawValue := range values {
		field, exists := fields[key]
		if !exists {
			return nil, fmt.Errorf("integration %s contains unknown field", kind)
		}
		value, valueErr := decodeFieldValue(field, rawValue)
		if valueErr != nil {
			return nil, fmt.Errorf("integration %s field is invalid", kind)
		}
		normalized[key] = value
	}
	for _, field := range declared {
		if _, exists := values[field.Key]; field.Required && !exists {
			return nil, fmt.Errorf("integration %s required field is missing", kind)
		}
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode canonical integration %s", kind)
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
		if json.Unmarshal(raw, &value) != nil || validateStringValue(field, value, true) != nil {
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

func validateStringValue(field Field, value string, allowPlainMultiline bool) error {
	if value == "" || len(value) > field.MaximumLength || strings.ContainsRune(value, '\x00') ||
		strings.ContainsRune(value, '\r') || (!allowPlainMultiline || field.Format != "PLAIN") && strings.ContainsRune(value, '\n') {
		return errors.New("string field is outside bounds")
	}
	if len(field.AllowedValues) > 0 && !contains(field.AllowedValues, value) {
		return errors.New("string field is outside allowed values")
	}
	switch field.Format {
	case "", "PLAIN":
	case "IDENTIFIER":
		for _, character := range value {
			if character < 0x21 || character > 0x7e {
				return errors.New("identifier field is invalid")
			}
		}
	case "HTTPS_ORIGIN", "HTTPS_URL":
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
			parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") || net.ParseIP(parsed.Hostname()) != nil ||
			!validHostname(strings.ToLower(parsed.Hostname())) || parsed.Hostname() != strings.ToLower(parsed.Hostname()) ||
			(field.Format == "HTTPS_ORIGIN" && (parsed.RawQuery != "" || parsed.Path != "" && parsed.Path != "/")) {
			return errors.New("HTTPS URL field is invalid")
		}
	case "EMAIL":
		parsed, err := mail.ParseAddress(value)
		if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
			return errors.New("email field is invalid")
		}
	case "HOST":
		if net.ParseIP(value) != nil || !validHostname(value) {
			return errors.New("host field is invalid")
		}
	default:
		return errors.New("string field format is invalid")
	}
	return nil
}

func validate(result *Package) error {
	if result.APIVersion != APIVersion || result.Kind != Kind || result.Metadata.Origin != Origin ||
		!validKey(result.Metadata.Key) || !versionPattern.MatchString(result.Metadata.Version) || len(result.Metadata.Version) > 32 ||
		len(result.Spec.Name) == 0 || len(result.Spec.Name) > 120 || len(result.Spec.Description) == 0 || len(result.Spec.Description) > 500 ||
		!validKey(result.Spec.Category) || !oneOf(result.Spec.Adapter, "SYNTHETIC_HTTP", "GITHUB", "GITLAB", "JIRA", "CONFLUENCE", "EMAIL_HTTPS", "MATTERMOST_INTERACTION") ||
		len(result.Spec.ConfigurationFields) > 24 || len(result.Spec.NetworkDestinations) == 0 || len(result.Spec.NetworkDestinations) > 16 ||
		len(result.Spec.Capabilities) == 0 || len(result.Spec.Capabilities) > 48 {
		return errors.New("integration package metadata or bounds are invalid")
	}
	if result.Spec.Credential != nil && (!validKey(result.Spec.Credential.SecretKey) || !oneOf(result.Spec.Credential.Kind, "TOKEN", "PASSWORD")) {
		return errors.New("integration package credential is invalid")
	}
	configurationKeys, err := validateFields(result.Spec.ConfigurationFields)
	if err != nil {
		return err
	}
	configurationByKey := make(map[string]Field, len(result.Spec.ConfigurationFields))
	for _, field := range result.Spec.ConfigurationFields {
		configurationByKey[field.Key] = field
	}
	destinationKeys := map[string]struct{}{}
	for _, destination := range result.Spec.NetworkDestinations {
		if !validKey(destination.Key) || !oneOf(destination.Source, "STATIC", "CONFIGURATION") ||
			destination.Port < 1 || destination.Port > 65535 || !oneOf(destination.TLS, "REQUIRED", "NONE") {
			return errors.New("integration package network destination is invalid")
		}
		if _, exists := destinationKeys[destination.Key]; exists {
			return errors.New("integration package network destination key is duplicated")
		}
		destinationKeys[destination.Key] = struct{}{}
		switch destination.Source {
		case "STATIC":
			if destination.ConfigurationField != "" || !validHostname(destination.Hostname) {
				return errors.New("integration package static network destination is invalid")
			}
		case "CONFIGURATION":
			field, exists := configurationByKey[destination.ConfigurationField]
			if destination.Hostname != "" || !exists || field.Type != "STRING" || !oneOf(field.Format, "HTTPS_ORIGIN", "HOST") {
				return errors.New("integration package configured network destination is invalid")
			}
		}
		if (destination.TLS == "REQUIRED" && destination.Port != 443) ||
			(destination.TLS == "NONE" && destination.Port == 443) {
			return errors.New("integration package network TLS policy is invalid")
		}
	}
	capabilityKeys := map[string]struct{}{}
	capabilityOperations := map[string]Capability{}
	for _, capability := range result.Spec.Capabilities {
		if !validKey(capability.Key) || len(capability.Name) == 0 || len(capability.Name) > 120 ||
			len(capability.Description) == 0 || len(capability.Description) > 500 || !validKey(capability.Operation) ||
			!oneOf(capability.Risk, "READ", "WRITE", "SENSITIVE", "DESTRUCTIVE") ||
			!oneOf(capability.ApprovalPolicy, "NONE", "HUMAN_EACH_EFFECT") ||
			(capability.Risk == "READ") != (capability.ApprovalPolicy == "NONE") ||
			!oneOf(capability.ResourceScope.Kind, "SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "GITLAB_PROJECT", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "MATTERMOST_CHANNEL") ||
			len(capability.ResourceScope.ConnectionFields) == 0 || len(capability.ResourceScope.ConnectionFields) > 8 ||
			len(capability.InputFields) > 24 || len(capability.OutputFields) == 0 || len(capability.OutputFields) > 24 ||
			!oneOf(capability.Execution.Idempotency, "READ_ONLY", "EFFECT_KEY", "PROVIDER_NATIVE") ||
			capability.Execution.TimeoutSeconds < 1 || capability.Execution.TimeoutSeconds > 120 ||
			capability.Execution.MaxAttempts < 1 || capability.Execution.MaxAttempts > 4 ||
			capability.Execution.RetryBackoffMilliseconds < 50 || capability.Execution.RetryBackoffMilliseconds > 5000 ||
			(capability.Risk == "READ") != (capability.Execution.Idempotency == "READ_ONLY") {
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
		if _, err := validateFields(capability.OutputFields); err != nil {
			return err
		}
		if _, exists := capabilityOperations[capability.Operation]; exists {
			return errors.New("integration package operation is duplicated")
		}
		capabilityOperations[capability.Operation] = capability
	}
	healthCapability, exists := capabilityOperations[result.Spec.HealthCheck.Operation]
	if !exists || healthCapability.Risk != "READ" || result.Spec.HealthCheck.TimeoutSeconds < 1 ||
		result.Spec.HealthCheck.TimeoutSeconds > 60 || result.Spec.HealthCheck.MaxAttempts < 1 || result.Spec.HealthCheck.MaxAttempts > 3 {
		return errors.New("integration package health check is invalid")
	}
	return nil
}

func validateFields(fields []Field) (map[string]struct{}, error) {
	keys := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !validKey(field.Key) || !oneOf(field.Type, "STRING", "INTEGER", "BOOLEAN") ||
			(field.Format != "" && !oneOf(field.Format, "PLAIN", "HTTPS_ORIGIN", "HTTPS_URL", "EMAIL", "HOST", "IDENTIFIER")) ||
			field.MaximumLength < 0 || field.MaximumLength > 65536 || field.Minimum < 0 || field.Maximum < 0 ||
			(field.Maximum != 0 && field.Maximum < field.Minimum) ||
			(field.Type == "STRING" && field.MaximumLength == 0) ||
			(field.Type != "STRING" && field.MaximumLength != 0) ||
			(field.Type != "INTEGER" && (field.Minimum != 0 || field.Maximum != 0)) ||
			(field.Type != "STRING" && (field.Format != "" || len(field.AllowedValues) > 0)) ||
			len(field.AllowedValues) > 32 {
			return nil, errors.New("integration package field is invalid")
		}
		seenValues := map[string]struct{}{}
		for _, allowed := range field.AllowedValues {
			if allowed == "" || len(allowed) > 120 || strings.ContainsAny(allowed, "\x00\r\n") {
				return nil, errors.New("integration package allowed field value is invalid")
			}
			if _, exists := seenValues[allowed]; exists {
				return nil, errors.New("integration package allowed field value is duplicated")
			}
			seenValues[allowed] = struct{}{}
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

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || value != strings.ToLower(value) || net.ParseIP(value) != nil || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) && value == candidate {
			return true
		}
	}
	return false
}
