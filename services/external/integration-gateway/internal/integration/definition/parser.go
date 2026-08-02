package definition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

const (
	MaximumDefinitionBytes  = 256 << 10
	MaximumTools            = 128
	MaximumSchemaBytes      = 64 << 10
	MaximumDescriptionBytes = 2 << 10
)

var (
	identifierPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	permissionPattern  = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,126}$`)
	environmentPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	reservedHeaders    = map[string]struct{}{
		"accept": {}, "authorization": {}, "connection": {}, "content-length": {},
		"content-type": {}, "forwarded": {}, "host": {}, "proxy-authenticate": {},
		"proxy-authorization": {}, "te": {}, "trailer": {}, "transfer-encoding": {},
		"upgrade": {}, "x-mattercodex-target": {},
	}
)

type document struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
	Spec       spec     `yaml:"spec"`
}

type metadata struct {
	Name    string `yaml:"name"`
	Version uint64 `yaml:"version"`
}

type spec struct {
	Tools []tool `yaml:"tools"`
}

type tool struct {
	Name              string               `yaml:"name"`
	Version           uint64               `yaml:"version"`
	Description       string               `yaml:"description"`
	Capability        string               `yaml:"capability"`
	Risk              enum.RiskLevel       `yaml:"risk"`
	Permission        string               `yaml:"permission"`
	Approval          enum.ApprovalPolicy  `yaml:"approval"`
	Idempotency       enum.IdempotencyMode `yaml:"idempotency"`
	InputSchema       rawSchema            `yaml:"inputSchema"`
	OutputSchema      rawSchema            `yaml:"outputSchema"`
	RedactionPointers []string             `yaml:"redactionPointers"`
	Direct            *directDelivery      `yaml:"direct"`
	HTTP              httpAdapter          `yaml:"http"`
}

type directDelivery struct {
	Reference        string   `yaml:"reference"`
	CLINames         []string `yaml:"cliNames"`
	EnvironmentNames []string `yaml:"environmentNames"`
}

type httpAdapter struct {
	Method            string            `yaml:"method"`
	Path              string            `yaml:"path"`
	Timeout           string            `yaml:"timeout"`
	IdempotencyHeader string            `yaml:"idempotencyHeader"`
	CredentialHeaders map[string]string `yaml:"credentialHeaders"`
}

type rawSchema struct {
	value json.RawMessage
}

func (value *rawSchema) UnmarshalYAML(node *yaml.Node) error {
	var decoded any
	if err := node.Decode(&decoded); err != nil {
		return errors.New("decode JSON schema")
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		return errors.New("marshal JSON schema")
	}
	value.value = raw
	return nil
}

func Parse(source []byte) (entity.Definition, error) {
	if len(source) == 0 || len(source) > MaximumDefinitionBytes {
		return entity.Definition{}, errors.New("integration definition size is invalid")
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	decoder.KnownFields(true)
	if err := decoder.Decode(&root); err != nil {
		return entity.Definition{}, errors.New("decode integration definition")
	}
	if err := validateYAMLTree(&root); err != nil {
		return entity.Definition{}, err
	}
	var value document
	decoder = yaml.NewDecoder(bytes.NewReader(source))
	decoder.KnownFields(true)
	if err := decoder.Decode(&value); err != nil {
		return entity.Definition{}, errors.New("decode strict integration definition")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return entity.Definition{}, errors.New("trailing YAML data is forbidden")
	}
	if value.APIVersion != "mattercodex.io/v1" || value.Kind != "IntegrationDefinition" ||
		!identifierPattern.MatchString(value.Metadata.Name) || value.Metadata.Version == 0 ||
		len(value.Spec.Tools) == 0 || len(value.Spec.Tools) > MaximumTools {
		return entity.Definition{}, errors.New("integration definition metadata is invalid")
	}
	definition := entity.Definition{ID: value.Metadata.Name, Version: value.Metadata.Version, Source: append([]byte(nil), source...)}
	seen := make(map[string]struct{}, len(value.Spec.Tools))
	for _, candidate := range value.Spec.Tools {
		parsed, err := parseTool(candidate)
		if err != nil {
			return entity.Definition{}, fmt.Errorf("tool %q: %w", candidate.Name, err)
		}
		key := parsed.Name + "@" + fmt.Sprint(parsed.Version)
		if _, duplicate := seen[key]; duplicate {
			return entity.Definition{}, errors.New("integration tool is duplicated")
		}
		seen[key] = struct{}{}
		definition.Tools = append(definition.Tools, parsed)
	}
	canonicalPackage, err := json.Marshal(struct {
		ID      string        `json:"id"`
		Version uint64        `json:"version"`
		Tools   []entity.Tool `json:"tools"`
	}{ID: definition.ID, Version: definition.Version, Tools: definition.Tools})
	if err != nil {
		return entity.Definition{}, errors.New("canonicalize integration definition")
	}
	digest := sha256.Sum256(canonicalPackage)
	definition.Digest = hex.EncodeToString(digest[:])
	return definition, nil
}

func parseTool(value tool) (entity.Tool, error) {
	if !identifierPattern.MatchString(value.Name) || strings.HasPrefix(value.Name, "mattercodex-") || value.Version == 0 ||
		len(value.Description) == 0 || len(value.Description) > MaximumDescriptionBytes ||
		!identifierPattern.MatchString(value.Capability) || !value.Risk.Valid() ||
		!permissionPattern.MatchString(value.Permission) ||
		!value.Approval.ValidFor(value.Risk) || !value.Idempotency.Valid() {
		return entity.Tool{}, errors.New("metadata is invalid")
	}
	if err := validateSchema(value.InputSchema.value); err != nil {
		return entity.Tool{}, fmt.Errorf("input schema: %w", err)
	}
	if err := validateSchema(value.OutputSchema.value); err != nil {
		return entity.Tool{}, fmt.Errorf("output schema: %w", err)
	}
	method := strings.ToUpper(value.HTTP.Method)
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPut &&
		method != http.MethodPatch && method != http.MethodDelete {
		return entity.Tool{}, errors.New("HTTP method is not allowed")
	}
	if value.HTTP.Path == "" || !strings.HasPrefix(value.HTTP.Path, "/") ||
		strings.Contains(value.HTTP.Path, "..") || path.Clean(value.HTTP.Path) != value.HTTP.Path ||
		strings.ContainsAny(value.HTTP.Path, "?#") {
		return entity.Tool{}, errors.New("HTTP path is invalid")
	}
	timeout, err := time.ParseDuration(value.HTTP.Timeout)
	if err != nil || timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return entity.Tool{}, errors.New("HTTP timeout is invalid")
	}
	if value.Idempotency == enum.IdempotencyProviderHeader &&
		(!validHeaderName(value.HTTP.IdempotencyHeader) || reservedHeader(value.HTTP.IdempotencyHeader)) {
		return entity.Tool{}, errors.New("idempotency header is invalid")
	}
	if value.Idempotency == enum.IdempotencyNone && value.Risk != enum.RiskRead {
		return entity.Tool{}, errors.New("effectful tool requires provider idempotency")
	}
	credentialHeaders := make(map[string]string, len(value.HTTP.CredentialHeaders))
	for header, reference := range value.HTTP.CredentialHeaders {
		if !validHeaderName(header) || reservedHeader(header) || !identifierPattern.MatchString(reference) ||
			strings.EqualFold(header, value.HTTP.IdempotencyHeader) {
			return entity.Tool{}, errors.New("credential header mapping is invalid")
		}
		credentialHeaders[header] = reference
	}
	for _, pointer := range value.RedactionPointers {
		if !validJSONPointer(pointer) {
			return entity.Tool{}, errors.New("redaction pointer is invalid")
		}
	}
	direct, err := parseDirectDelivery(value)
	if err != nil {
		return entity.Tool{}, err
	}
	return entity.Tool{
		Name: value.Name, Version: value.Version, Description: value.Description,
		Capability: value.Capability, Risk: value.Risk, Permission: value.Permission,
		ApprovalPolicy: value.Approval, Idempotency: value.Idempotency,
		InputSchema:       append(json.RawMessage(nil), value.InputSchema.value...),
		OutputSchema:      append(json.RawMessage(nil), value.OutputSchema.value...),
		RedactionPointers: append([]string(nil), value.RedactionPointers...),
		DirectDelivery:    direct,
		HTTP: entity.HTTPAdapter{Method: method, Path: value.HTTP.Path, Timeout: timeout,
			IdempotencyHeader: value.HTTP.IdempotencyHeader, CredentialHeaders: credentialHeaders},
	}, nil
}

func parseDirectDelivery(value tool) (*entity.DirectDelivery, error) {
	if value.Direct == nil {
		return nil, nil
	}
	if value.Risk != enum.RiskRead || value.Approval != enum.ApprovalNever ||
		len(value.HTTP.CredentialHeaders) != 0 || !permissionPattern.MatchString(value.Direct.Reference) ||
		len(value.Direct.CLINames) > 32 || len(value.Direct.EnvironmentNames) > 32 ||
		len(value.Direct.CLINames)+len(value.Direct.EnvironmentNames) == 0 {
		return nil, errors.New("direct delivery is not safe")
	}
	cliNames := make([]string, 0, len(value.Direct.CLINames))
	environmentNames := make([]string, 0, len(value.Direct.EnvironmentNames))
	seen := make(map[string]struct{}, len(value.Direct.CLINames)+len(value.Direct.EnvironmentNames))
	for _, name := range value.Direct.CLINames {
		if !identifierPattern.MatchString(name) {
			return nil, errors.New("direct CLI name is invalid")
		}
		key := "cli:" + name
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("direct CLI name is duplicated")
		}
		seen[key] = struct{}{}
		cliNames = append(cliNames, name)
	}
	for _, name := range value.Direct.EnvironmentNames {
		if !environmentPattern.MatchString(name) {
			return nil, errors.New("direct environment name is invalid")
		}
		key := "env:" + name
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("direct environment name is duplicated")
		}
		seen[key] = struct{}{}
		environmentNames = append(environmentNames, name)
	}
	return &entity.DirectDelivery{Reference: value.Direct.Reference, CLINames: cliNames, EnvironmentNames: environmentNames}, nil
}

func reservedHeader(value string) bool {
	canonical := strings.ToLower(value)
	if _, reserved := reservedHeaders[canonical]; reserved {
		return true
	}
	return strings.HasPrefix(canonical, "x-forwarded-") || strings.HasPrefix(canonical, "x-mattercodex-")
}

func validateYAMLTree(node *yaml.Node) error {
	if node == nil {
		return errors.New("YAML document is empty")
	}
	if node.Kind == yaml.AliasNode {
		return errors.New("YAML aliases are forbidden")
	}
	if node.Tag == "!!null" {
		return errors.New("YAML null values are forbidden")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Value == "" {
				return errors.New("YAML mapping key is invalid")
			}
			if _, duplicate := seen[key.Value]; duplicate {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLTree(child); err != nil {
			return err
		}
	}
	return nil
}

func validateSchema(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > MaximumSchemaBytes || !json.Valid(raw) {
		return errors.New("JSON schema is invalid")
	}
	var schema map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil || schema["type"] != "object" {
		return errors.New("top-level JSON schema type must be object")
	}
	if additional, exists := schema["additionalProperties"]; !exists || additional != false {
		return errors.New("JSON schema must close additional properties")
	}
	var compiled jsonschema.Schema
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compiled); err != nil {
		return errors.New("JSON schema is unsupported")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON schema data is forbidden")
	}
	if _, err := compiled.Resolve(nil); err != nil {
		return errors.New("JSON schema cannot be resolved")
	}
	return nil
}

func validHeaderName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') &&
			!(char >= '0' && char <= '9') && !strings.ContainsRune("!#$%&'*+-.^_`|~", char) {
			return false
		}
	}
	return true
}

func validJSONPointer(value string) bool {
	if value == "" || value[0] != '/' || len(value) > 512 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] == '~' && (index+1 >= len(value) || (value[index+1] != '0' && value[index+1] != '1')) {
			return false
		}
	}
	return true
}
