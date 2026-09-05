// Package revision валидирует immutable configuration revisions до публикации.
package revision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	domainvalue "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"go.yaml.in/yaml/v3"
)

var ErrInvalid = errors.New("invalid configuration revision")

const (
	KindPromptTemplate        = "PROMPT_TEMPLATE"
	KindRoleImage             = "ROLE_IMAGE"
	KindIntegrationDefinition = "INTEGRATION_DEFINITION"
	KindSystemSTT             = "SYSTEM_STT"
	KindEmailMailbox          = "EMAIL_MAILBOX"
)

type Diagnostic struct{ Code, Message string }

type document struct {
	Name        string                  `json:"name" yaml:"name" toml:"name"`
	Description string                  `json:"description,omitempty" yaml:"description,omitempty" toml:"description,omitempty"`
	Template    string                  `json:"template,omitempty" yaml:"template,omitempty" toml:"template,omitempty"`
	BaseImage   string                  `json:"baseImage,omitempty" yaml:"baseImage,omitempty" toml:"baseImage,omitempty"`
	Packages    []string                `json:"packages,omitempty" yaml:"packages,omitempty" toml:"packages,omitempty"`
	Definition  *integrationDefinition  `json:"definition,omitempty" yaml:"definition,omitempty" toml:"definition,omitempty"`
	STT         *STTConfiguration       `json:"stt,omitempty" yaml:"stt,omitempty" toml:"stt,omitempty"`
	RoleImage   *RoleImageConfiguration `json:"roleImage,omitempty" yaml:"roleImage,omitempty" toml:"roleImage,omitempty"`
}

type integrationDefinition struct {
	Key        string                 `json:"key" yaml:"key" toml:"key"`
	Version    string                 `json:"version" yaml:"version" toml:"version"`
	Adapter    string                 `json:"adapter" yaml:"adapter" toml:"adapter"`
	Operations []integrationOperation `json:"operations" yaml:"operations" toml:"operations"`
}
type integrationOperation struct {
	Key          string `json:"key" yaml:"key" toml:"key"`
	Operation    string `json:"operation" yaml:"operation" toml:"operation"`
	Risk         string `json:"risk" yaml:"risk" toml:"risk"`
	Approval     string `json:"approval" yaml:"approval" toml:"approval"`
	ResourceKind string `json:"resourceKind" yaml:"resourceKind" toml:"resourceKind"`
}
type STTConfiguration struct {
	Enabled                          bool                      `json:"enabled" yaml:"enabled" toml:"enabled"`
	ProviderAccountRef               string                    `json:"providerAccountRef" yaml:"providerAccountRef" toml:"providerAccountRef"`
	Model                            string                    `json:"model" yaml:"model" toml:"model"`
	Language                         string                    `json:"language" yaml:"language" toml:"language"`
	PermissionKey                    string                    `json:"permissionKey" yaml:"permissionKey" toml:"permissionKey"`
	Parameters                       domainvalue.STTParameters `json:"parameters,omitempty" yaml:"parameters,omitempty" toml:"parameters,omitempty"`
	MaximumAudioBytes                uint64                    `json:"maximumAudioBytes,omitempty" yaml:"maximumAudioBytes,omitempty" toml:"maximumAudioBytes,omitempty"`
	MaximumAudioDurationMilliseconds uint64                    `json:"maximumAudioDurationMilliseconds,omitempty" yaml:"maximumAudioDurationMilliseconds,omitempty" toml:"maximumAudioDurationMilliseconds,omitempty"`
	ProviderTimeoutMilliseconds      uint64                    `json:"providerTimeoutMilliseconds,omitempty" yaml:"providerTimeoutMilliseconds,omitempty" toml:"providerTimeoutMilliseconds,omitempty"`
}

func ParseSystemSTT(content string) (STTConfiguration, error) {
	var document document
	if err := decodeStrict("JSON", content, &document); err != nil {
		return STTConfiguration{}, ErrInvalid
	}
	if err := validateDocument(KindSystemSTT, document); err != nil {
		return STTConfiguration{}, err
	}
	result := *document.STT
	if result.MaximumAudioBytes == 0 {
		result.MaximumAudioBytes = modelprofile.RecommendedMaximumBytes
	}
	if result.MaximumAudioDurationMilliseconds == 0 {
		result.MaximumAudioDurationMilliseconds = uint64(modelprofile.RecommendedMaximumDuration.Milliseconds())
	}
	if result.ProviderTimeoutMilliseconds == 0 {
		result.ProviderTimeoutMilliseconds = uint64(modelprofile.MaximumProviderTimeout.Milliseconds())
	}
	return result, nil
}

func Validate(kind, format, content string) (string, []Diagnostic, error) {
	kind, format, content = strings.ToUpper(strings.TrimSpace(kind)), strings.ToUpper(strings.TrimSpace(format)), strings.TrimSpace(content)
	if content == "" || len(content) > 256<<10 {
		return "", []Diagnostic{{Code: "REVISION_CONTENT_INVALID", Message: "Revision content is empty or exceeds the size limit"}}, ErrInvalid
	}
	if kind == KindIntegrationDefinition {
		definition, err := IntegrationPackage(format, content)
		if err != nil {
			return "", []Diagnostic{{Code: "REVISION_SEMANTICS_INVALID", Message: "Integration package is not registered and ready"}}, ErrInvalid
		}
		return definition.Digest, nil, nil
	}
	if kind == KindPromptTemplate && format == "TEXT" {
		diagnostics := promptservice.Validate(content, promptservice.Catalog())
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == "ERROR" {
				return "", []Diagnostic{{Code: diagnostic.Code, Message: diagnostic.Message}}, ErrInvalid
			}
		}
		return digest(content), nil, nil
	}
	var value document
	if err := decodeStrict(format, content, &value); err != nil {
		return "", []Diagnostic{{Code: "REVISION_SYNTAX_INVALID", Message: "Revision syntax or fields are invalid"}}, ErrInvalid
	}
	if err := validateDocument(kind, value); err != nil {
		return "", []Diagnostic{{Code: "REVISION_SEMANTICS_INVALID", Message: err.Error()}}, ErrInvalid
	}
	return digest(content), nil, nil
}

// IntegrationDefinitionKey возвращает server-validated stable key typed
// definition; caller не может подменить его отдельным полем rebind-команды.
func IntegrationDefinitionKey(format, content string) (string, error) {
	definition, err := IntegrationPackage(format, content)
	if err != nil {
		return "", err
	}
	return definition.Metadata.Key, nil
}

// Управляемый package может сужать исполняемый contract поставленного adapter.
func IntegrationPackage(format, content string) (integrationpackage.Package, error) {
	if format != "JSON" && format != "YAML" {
		return integrationpackage.Package{}, ErrInvalid
	}
	definition, err := integrationpackage.Parse([]byte(content))
	if err != nil || !(definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) ||
		definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction)) {
		return integrationpackage.Package{}, ErrInvalid
	}
	registered, err := integrationpackage.LoadShipped()
	if err != nil {
		return integrationpackage.Package{}, ErrInvalid
	}
	profile, ok := registered[definition.Metadata.Key]
	if !ok || integrationpackage.ValidateExecutableRevision(definition, profile) != nil {
		return integrationpackage.Package{}, ErrInvalid
	}
	return definition, nil
}

func decodeStrict(format, content string, target any) error {
	switch format {
	case "JSON":
		decoder := json.NewDecoder(strings.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(target); err != nil {
			return err
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return ErrInvalid
		}
		return nil
	case "YAML":
		decoder := yaml.NewDecoder(strings.NewReader(content))
		decoder.KnownFields(true)
		if err := decoder.Decode(target); err != nil {
			return err
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return ErrInvalid
		}
		return nil
	case "TOML":
		metadata, err := toml.Decode(content, target)
		if err != nil || len(metadata.Undecoded()) != 0 {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}

func validateDocument(kind string, value document) error {
	if strings.TrimSpace(value.Name) == "" || len(value.Name) > 160 {
		return errors.New("revision name is invalid")
	}
	switch kind {
	case KindRoleImage:
		if value.RoleImage != nil {
			if value.BaseImage != "" || len(value.Packages) != 0 || value.Template != "" || value.Definition != nil || value.STT != nil ||
				value.RoleImage.RoleDefinitionRef == "" || value.RoleImage.Environment.EnvironmentKey == "" {
				return errors.New("role image specification is invalid")
			}
			return nil
		}
		if value.BaseImage == "" || len(value.Packages) > 128 || value.Template != "" || value.Definition != nil || value.STT != nil {
			return errors.New("role image specification is invalid")
		}
	case KindIntegrationDefinition:
		if value.Definition == nil || value.Template != "" || value.BaseImage != "" || value.STT != nil || !validIntegration(*value.Definition) {
			return errors.New("integration definition is invalid")
		}
	case KindSystemSTT:
		if value.STT == nil || value.Template != "" || value.BaseImage != "" || value.Definition != nil ||
			value.STT.ProviderAccountRef == "" || value.STT.Model == "" || value.STT.PermissionKey != "platform.stt.use" {
			return errors.New("system STT configuration is invalid")
		}
		if err := value.STT.Parameters.Validate(value.STT.Model, value.STT.Language); err != nil {
			return err
		}
		if value.STT.MaximumAudioBytes != 0 && (value.STT.MaximumAudioBytes < modelprofile.MinimumAudioBytes || value.STT.MaximumAudioBytes > modelprofile.MaximumAudioBytes) ||
			value.STT.MaximumAudioDurationMilliseconds != 0 && (value.STT.MaximumAudioDurationMilliseconds < uint64(modelprofile.MinimumAudioDuration.Milliseconds()) || value.STT.MaximumAudioDurationMilliseconds > uint64(modelprofile.MaximumAudioDuration.Milliseconds())) ||
			value.STT.ProviderTimeoutMilliseconds != 0 && (value.STT.ProviderTimeoutMilliseconds < uint64(modelprofile.MinimumProviderTimeout.Milliseconds()) || value.STT.ProviderTimeoutMilliseconds > uint64(modelprofile.MaximumProviderTimeout.Milliseconds())) {
			return errors.New("system STT limits are invalid")
		}
	default:
		return errors.New("revision kind is invalid")
	}
	return nil
}

func validIntegration(value integrationDefinition) bool {
	if value.Key == "" || value.Version == "" || value.Adapter == "" || len(value.Operations) == 0 || len(value.Operations) > 128 {
		return false
	}
	seen := make(map[string]struct{}, len(value.Operations))
	for _, operation := range value.Operations {
		if operation.Key == "" || operation.Operation == "" || operation.ResourceKind == "" ||
			!member(operation.Risk, "READ", "WRITE", "SENSITIVE", "DESTRUCTIVE") ||
			!member(operation.Approval, "NONE", "HUMAN_EACH_EFFECT") {
			return false
		}
		if _, exists := seen[operation.Key]; exists {
			return false
		}
		seen[operation.Key] = struct{}{}
	}
	return true
}

func member(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func digest(value string) string {
	sum := sha256.Sum256(bytes.TrimSpace([]byte(value)))
	return hex.EncodeToString(sum[:])
}
