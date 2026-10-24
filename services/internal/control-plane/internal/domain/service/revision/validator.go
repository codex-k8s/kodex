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

// ErrInvalid marks rejected revision syntax, kind or semantics with a fixed runtime error that
// does not contain the source document.
// ErrInvalid обозначает отклонённые синтаксис, вид либо семантику ревизии фиксированной
// runtime-ошибкой без исходного документа.
var ErrInvalid = errors.New("invalid configuration revision")

const (
	// KindPromptTemplate identifies prompt-template revisions routed to the dedicated text-template
	// validation path when format is TEXT.
	// KindPromptTemplate обозначает ревизии prompt template, передаваемые отдельной проверке
	// текстового шаблона при формате TEXT.
	KindPromptTemplate = "PROMPT_TEMPLATE"
	// KindRoleImage identifies structured role-image revisions with typed environment or supported
	// legacy image fields.
	// KindRoleImage обозначает структурированные ревизии образа роли с типизированным окружением либо
	// поддерживаемыми legacy полями образа.
	KindRoleImage = "ROLE_IMAGE"
	// KindIntegrationDefinition identifies integration package revisions validated against the shipped
	// executable adapter registry.
	// KindIntegrationDefinition обозначает ревизии integration package, проверяемые по реестру
	// поставляемых исполняемых adapters.
	KindIntegrationDefinition = "INTEGRATION_DEFINITION"
	// KindSystemSTT identifies immutable system speech-to-text configuration revisions containing
	// provider references rather than credential values.
	// KindSystemSTT обозначает immutable ревизии системной speech-to-text конфигурации со ссылками
	// provider вместо значений credentials.
	KindSystemSTT = "SYSTEM_STT"
	// KindEmailMailbox names the email-mailbox revision kind whose dedicated mailbox parser is
	// separate from this generic document validator.
	// KindEmailMailbox задаёт вид ревизии почтового mailbox, отдельный parser которого находится вне
	// этого общего валидатора документов.
	KindEmailMailbox = "EMAIL_MAILBOX"
)

// Diagnostic carries a stable revision validation code and runtime message without retaining the
// untrusted source document.
// Diagnostic несёт устойчивый код проверки ревизии и runtime-сообщение без сохранения
// непроверенного исходного документа.
type Diagnostic struct{ Code, Message string }

// document defines known structured revision fields across JSON, YAML and TOML; kind-specific
// validation selects the allowed semantic branch.
// document определяет известные поля структурированной ревизии JSON, YAML и TOML; проверка
// конкретного вида выбирает допустимую семантическую ветвь.
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

// integrationDefinition models the legacy typed integration header and operation list checked by
// the internal structured-document semantics validator.
// integrationDefinition моделирует legacy типизированные заголовок интеграции и список операций
// для внутренней семантической проверки структурированного документа.
type integrationDefinition struct {
	Key        string                 `json:"key" yaml:"key" toml:"key"`
	Version    string                 `json:"version" yaml:"version" toml:"version"`
	Adapter    string                 `json:"adapter" yaml:"adapter" toml:"adapter"`
	Operations []integrationOperation `json:"operations" yaml:"operations" toml:"operations"`
}

// integrationOperation records a stable operation key, target operation, risk, approval mode and
// resource kind for legacy integration semantics.
// integrationOperation хранит устойчивый ключ операции, целевую операцию, риск, режим approval и
// вид ресурса для legacy семантики интеграции.
type integrationOperation struct {
	Key          string `json:"key" yaml:"key" toml:"key"`
	Operation    string `json:"operation" yaml:"operation" toml:"operation"`
	Risk         string `json:"risk" yaml:"risk" toml:"risk"`
	Approval     string `json:"approval" yaml:"approval" toml:"approval"`
	ResourceKind string `json:"resourceKind" yaml:"resourceKind" toml:"resourceKind"`
}

// STTConfiguration stores immutable provider/model references, transcription parameters and
// bounded audio/timeout limits without accepting a provider credential value.
// STTConfiguration хранит immutable ссылки provider и модели, параметры транскрипции и
// ограниченные audio/timeout лимиты без принятия значения provider credential.
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

// ParseSystemSTT decodes and validates a JSON system-STT document and fills omitted audio size,
// duration and provider timeout with model-profile defaults.
// ParseSystemSTT декодирует и проверяет JSON документ system STT и заполняет отсутствующие размер
// аудио, длительность и provider timeout значениями model profile.
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

// Validate normalizes kind/format and bounds trimmed content, uses specialized integration or
// text-prompt validation, and otherwise returns a digest only after structured syntax and
// semantics pass.
// Validate нормализует вид и формат, ограничивает обрезанное содержимое, использует отдельную
// проверку integration либо text prompt и иначе возвращает digest лишь после структурных
// синтаксиса и семантики.
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

// IntegrationDefinitionKey returns the stable key of a server-validated executable integration
// package so a caller cannot override it with a separate rebind payload field.
// IntegrationDefinitionKey возвращает устойчивый ключ проверенного сервером исполняемого
// integration package без возможности подмены отдельным полем rebind payload caller.
func IntegrationDefinitionKey(format, content string) (string, error) {
	definition, err := IntegrationPackage(format, content)
	if err != nil {
		return "", err
	}
	return definition.Metadata.Key, nil
}

// IntegrationPackage accepts JSON/YAML packages only for registered gateway routes and validates
// each revision against the exact shipped ready adapter profile.
// IntegrationPackage принимает JSON/YAML packages только для зарегистрированных gateway routes и
// проверяет каждую ревизию по точному поставляемому готовому adapter profile.
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

// decodeStrict decodes JSON/YAML with unknown-field rejection and exactly one document, or TOML
// with no undecoded fields; unsupported formats fail before semantic validation.
// decodeStrict декодирует JSON/YAML с отказом неизвестным полям и требованием ровно одного
// документа либо TOML без undecoded полей; неподдерживаемый формат отклоняется до семантической
// проверки.
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

// validateDocument requires a bounded nonblank name and applies role-image, legacy integration or
// STT branch rules, rejecting unsupported kinds and out-of-profile STT parameters/limits.
// validateDocument требует ограниченное непустое имя и применяет правила ветви образа роли, legacy
// integration либо STT, отклоняя неподдерживаемые виды и параметры либо лимиты STT вне профиля.
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

// validIntegration checks a nonempty bounded legacy operation list with unique keys, required
// operation/resource fields and closed risk/approval sets.
// validIntegration проверяет непустой ограниченный список legacy операций с уникальными ключами,
// обязательными полями операции и ресурса и закрытыми наборами риска и approval.
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

// member performs exact case-sensitive membership over the supplied closed string set without
// normalization or fallback acceptance.
// member выполняет точную регистрозависимую проверку в переданном закрытом наборе строк без
// нормализации либо допуска запасного значения.
func member(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// digest returns lowercase SHA-256 of whitespace-trimmed revision bytes without canonicalizing
// structured fields or template semantics.
// digest возвращает строчный SHA-256 байтов ревизии с обрезанными крайними пробелами без
// канонизации структурированных полей либо семантики шаблона.
func digest(value string) string {
	sum := sha256.Sum256(bytes.TrimSpace([]byte(value)))
	return hex.EncodeToString(sum[:])
}
