// Package integrationpackage loads versioned integration definitions under a closed adapter contract.
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
	// APIVersion pins the supported integration-package wire schema version.
	// APIVersion закрепляет поддержанную версию wire-схемы пакета интеграции.
	APIVersion = "integrations.kodex.io/v1"
	// Kind rejects documents that are not integration packages.
	// Kind отклоняет документы, не являющиеся пакетами интеграции.
	Kind = "IntegrationPackage"
	// Origin marks definitions shipped with the platform rather than managed by a tenant.
	// Origin отмечает определения, поставленные платформой, а не управляемые арендатором.
	Origin = "SHIPPED"
	// OriginUI identifies a managed revision created through the owner-authorized interface.
	// OriginUI определяет управляемую версию, созданную через интерфейс с полномочиями владельца.
	OriginUI = "UI"
	// OriginGit identifies a managed revision originating from a pinned Git source.
	// OriginGit определяет управляемую версию из закреплённого Git-источника.
	OriginGit = "GIT"
	// maxBytes bounds the untrusted package document before parsing.
	// maxBytes ограничивает недоверенный документ пакета до разбора.
	maxBytes = 256 << 10
	// maxObjectBytes bounds one tool input or output JSON object before decoding.
	// maxObjectBytes ограничивает один JSON-объект ввода или вывода инструмента до декодирования.
	maxObjectBytes = 512 << 10
	// maxFieldLength caps a declared string field so canonical validation stays bounded.
	// maxFieldLength ограничивает объявленное строковое поле при канонической проверке.
	maxFieldLength = 349528
)

var (
	// keyPattern accepts stable package, capability and field keys without path or whitespace syntax.
	// keyPattern допускает устойчивые ключи пакета, возможности и поля без синтаксиса пути или пробелов.
	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	// versionPattern restricts package versions to a positive major and numeric minor and patch.
	// versionPattern ограничивает версию пакета положительным major и числовыми minor и patch.
	versionPattern = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
)

// Package is one validated integration definition with a digest over its canonical JSON form.
// Package — проверенное определение интеграции с дайджестом канонической JSON-формы.
type Package struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
	Digest     string   `yaml:"-" json:"-"`
}

// Metadata carries the stable package key, version and server-controlled origin.
// Metadata хранит устойчивый ключ, версию пакета и origin, назначаемый сервером.
type Metadata struct {
	Key     string `yaml:"key" json:"key"`
	Version string `yaml:"version" json:"version"`
	Origin  string `yaml:"origin" json:"origin"`
}

// Spec binds the registered adapter to exact configuration, destinations, health and capabilities.
// Spec связывает зарегистрированный адаптер с точной конфигурацией, назначениями, health и возможностями.
type Spec struct {
	Name                string               `yaml:"name" json:"name"`
	Description         string               `yaml:"description" json:"description"`
	Category            string               `yaml:"category" json:"category"`
	Adapter             string               `yaml:"adapter" json:"adapter"`
	AdapterOwner        string               `yaml:"adapterOwner" json:"adapterOwner"`
	ExecutionRoute      string               `yaml:"executionRoute" json:"executionRoute"`
	Readiness           string               `yaml:"readiness" json:"readiness"`
	Credential          *Credential          `yaml:"credential,omitempty" json:"credential,omitempty"`
	ConfigurationFields []Field              `yaml:"configurationFields" json:"configurationFields"`
	NetworkDestinations []NetworkDestination `yaml:"networkDestinations" json:"networkDestinations"`
	HealthCheck         HealthCheck          `yaml:"healthCheck" json:"healthCheck"`
	Capabilities        []Capability         `yaml:"capabilities" json:"capabilities"`
}

// Credential describes a required secret key and kind, never the credential value itself.
// Credential описывает требуемые ключ и вид секрета, но не значение реквизита.
type Credential struct {
	SecretKey string `yaml:"secretKey" json:"secretKey"`
	Kind      string `yaml:"kind" json:"kind"`
}

// Field constrains one named scalar configuration, input or output value before adapter use.
// Field ограничивает именованное скалярное значение конфигурации, ввода или вывода до вызова адаптера.
type Field struct {
	Key           string   `yaml:"key" json:"key"`
	Type          string   `yaml:"type" json:"type"`
	Format        string   `yaml:"format,omitempty" json:"format,omitempty"`
	Required      bool     `yaml:"required" json:"required"`
	MaximumLength int      `yaml:"maximumLength,omitempty" json:"maximumLength,omitempty"`
	AllowEmpty    bool     `yaml:"allowEmpty,omitempty" json:"allowEmpty,omitempty"`
	Minimum       int64    `yaml:"minimum,omitempty" json:"minimum,omitempty"`
	Maximum       int64    `yaml:"maximum,omitempty" json:"maximum,omitempty"`
	AllowedValues []string `yaml:"allowedValues,omitempty" json:"allowedValues,omitempty"`
}

// NetworkDestination pins a static host or validated configuration field to one port and TLS mode.
// NetworkDestination закрепляет статический host либо проверенное поле конфигурации за портом и режимом TLS.
type NetworkDestination struct {
	Key                string `yaml:"key" json:"key"`
	Source             string `yaml:"source" json:"source"`
	Hostname           string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	ConfigurationField string `yaml:"configurationField,omitempty" json:"configurationField,omitempty"`
	Port               int    `yaml:"port" json:"port"`
	TLS                string `yaml:"tls" json:"tls"`
}

// HealthCheck selects a bounded read operation for adapter readiness without creating an effect.
// HealthCheck выбирает ограниченную операцию чтения для проверки готовности адаптера без эффекта.
type HealthCheck struct {
	Operation      string `yaml:"operation" json:"operation"`
	TimeoutSeconds int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxAttempts    int    `yaml:"maxAttempts" json:"maxAttempts"`
}

// ResourceScope derives an operation's exact resource identity from validated connection fields.
// ResourceScope выводит точную идентичность ресурса операции из проверенных полей подключения.
type ResourceScope struct {
	Kind             string   `yaml:"kind" json:"kind"`
	ConnectionFields []string `yaml:"connectionFields" json:"connectionFields"`
}

// Capability binds one operation to risk, approval, scope, typed fields and execution limits.
// Capability связывает операцию с риском, подтверждением, областью, типизированными полями и лимитами исполнения.
type Capability struct {
	Key                     string        `yaml:"key" json:"key"`
	Name                    string        `yaml:"name" json:"name"`
	Description             string        `yaml:"description" json:"description"`
	Operation               string        `yaml:"operation" json:"operation"`
	Risk                    string        `yaml:"risk" json:"risk"`
	ApprovalPolicy          string        `yaml:"approvalPolicy" json:"approvalPolicy"`
	AllowedApprovalPolicies []string      `yaml:"allowedApprovalPolicies" json:"allowedApprovalPolicies"`
	ResourceScope           ResourceScope `yaml:"resourceScope" json:"resourceScope"`
	InputFields             []Field       `yaml:"inputFields" json:"inputFields"`
	OutputFields            []Field       `yaml:"outputFields" json:"outputFields"`
	Execution               Execution     `yaml:"execution" json:"execution"`
	OpenAPI                 *OpenAPIHTTP  `yaml:"openapi,omitempty" json:"openapi,omitempty"`
}

// OpenAPIHTTP pins an imported operation to a source digest, literal server, method and closed input;
// it does not accept a caller-selected URL, method or credential at invocation time.
// OpenAPIHTTP является закреплённым результатом import/admission, а не
// разрешением принимать URL, method или credential из вызова модели.
type OpenAPIHTTP struct {
	OperationID       string         `yaml:"operationId" json:"operationId"`
	SourceDigest      string         `yaml:"sourceDigest,omitempty" json:"sourceDigest,omitempty"`
	ServerOrigin      string         `yaml:"serverOrigin,omitempty" json:"serverOrigin,omitempty"`
	Method            string         `yaml:"method" json:"method"`
	Path              string         `yaml:"path" json:"path"`
	AuthScheme        string         `yaml:"authScheme" json:"authScheme"`
	AuthHeader        string         `yaml:"authHeader,omitempty" json:"authHeader,omitempty"`
	IdempotencyHeader string         `yaml:"idempotencyHeader,omitempty" json:"idempotencyHeader,omitempty"`
	InputSchema       map[string]any `yaml:"inputSchema" json:"inputSchema"`
}

// Execution records retry, timeout and backoff limits under the declared idempotency guarantee.
// Execution хранит лимиты повтора, времени и задержки согласно заявленной гарантии идемпотентности.
type Execution struct {
	Idempotency              string `yaml:"idempotency" json:"idempotency"`
	TimeoutSeconds           int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxAttempts              int    `yaml:"maxAttempts" json:"maxAttempts"`
	RetryBackoffMilliseconds int    `yaml:"retryBackoffMilliseconds" json:"retryBackoffMilliseconds"`
}

// Parse rejects oversized, ambiguous or unknown-field YAML/JSON and returns a validated package
// with a digest of canonical JSON rather than trusting a digest supplied by the document.
// Parse отклоняет чрезмерный, неоднозначный YAML/JSON и неизвестные поля, возвращая пакет
// с дайджестом канонического JSON вместо доверия дайджесту из документа.
func Parse(raw []byte) (Package, error) {
	if len(raw) == 0 || len(raw) > maxBytes {
		return Package{}, errors.New("integration package size is invalid")
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
		return parsePackageJSON(trimmed)
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

// LoadShipped parses every embedded definition, checks its registered adapter binding and rejects
// non-shipped origins or duplicate keys before exposing the complete map.
// LoadShipped разбирает каждое встроенное определение, проверяет привязку к адаптеру и отклоняет
// чужой origin или повторный ключ до выдачи полного набора.
func LoadShipped() (map[string]Package, error) {
	result := make(map[string]Package, len(shippedYAML))
	for filename, raw := range shippedYAML {
		definition, err := Parse([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("load shipped integration package %s: %w", filename, err)
		}
		if err := ValidateAdapterBinding(definition); err != nil {
			return nil, fmt.Errorf("load shipped integration package %s: %w", filename, err)
		}
		if definition.Metadata.Origin != Origin {
			return nil, errors.New("shipped integration package origin is invalid")
		}
		if _, exists := result[definition.Metadata.Key]; exists {
			return nil, errors.New("duplicate shipped integration package key")
		}
		result[definition.Metadata.Key] = definition
	}
	return result, nil
}

// Sorted returns package values in stable key order without mutating the caller's map.
// Sorted возвращает пакеты в устойчивом порядке ключей, не изменяя карту вызывающего кода.
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

// Capability resolves a declared public capability key without inventing a fallback operation.
// Capability разрешает объявленный публичный ключ возможности без выдуманной запасной операции.
func (definition Package) Capability(key string) (Capability, bool) {
	for _, capability := range definition.Spec.Capabilities {
		if capability.Key == key {
			return capability, true
		}
	}
	return Capability{}, false
}

// CapabilityByOperation resolves the pinned adapter operation, which may differ from the public
// capability key of an imported integration.
// CapabilityByOperation разрешает закреплённую операцию, которая у импортированных
// интеграций может отличаться от публичного ключа возможности.
func (definition Package) CapabilityByOperation(operation string) (Capability, bool) {
	for _, capability := range definition.Spec.Capabilities {
		if capability.Operation == operation {
			return capability, true
		}
	}
	return Capability{}, false
}

// ValidateConfiguration checks public connection fields and an imported server pin without reading secrets.
// ValidateConfiguration проверяет public configuration без credential values.
func (definition Package) ValidateConfiguration(configuration map[string]string) error {
	return definition.validateConfiguration(configuration, false)
}

// ValidateConnectionBootstrapConfiguration permits initial public configuration of a shipped
// OpenAPI template without making that unbound template executable.
// ValidateConnectionBootstrapConfiguration разрешает создать ещё не привязанное
// OpenAPI-подключение по поставленному шаблону. Исполнение такого шаблона
// остаётся запрещённым до owner-привязки опубликованной ревизии.
func (definition Package) ValidateConnectionBootstrapConfiguration(configuration map[string]string) error {
	return definition.validateConfiguration(configuration, definition.Metadata.Origin == Origin && definition.Spec.Adapter == string(AdapterOpenAPIMCP))
}

// validateConfiguration rejects unknown or missing fields and enforces the pinned OpenAPI server,
// except during explicit bootstrap of a non-executable shipped template.
// validateConfiguration отклоняет неизвестные и отсутствующие поля и проверяет закреплённый сервер
// OpenAPI, кроме явного начального подключения неисполняемого поставленного шаблона.
func (definition Package) validateConfiguration(configuration map[string]string, allowUnboundTemplate bool) error {
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
	if definition.Spec.Adapter == string(AdapterOpenAPIMCP) && !allowUnboundTemplate {
		for _, capability := range definition.Spec.Capabilities {
			if capability.OpenAPI == nil ||
				(capability.OpenAPI.ServerOrigin != "" || definition.Spec.Readiness == string(ReadinessReady)) &&
					capability.OpenAPI.ServerOrigin != configuration["base_url"] {
				return errors.New("OpenAPI server origin does not match connection")
			}
		}
	}
	return nil
}

// ResourceScopeValues derives every required resource locator from validated connection configuration;
// absent or empty fields close the operation rather than broadening its scope.
// ResourceScopeValues выводит все требуемые координаты ресурса из проверенной конфигурации;
// отсутствующее или пустое поле закрывает операцию, не расширяя её область.
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

// ValidateInput canonicalizes one closed JSON object against either the imported OpenAPI schema
// or the package's declared scalar input fields.
// ValidateInput канонизирует один закрытый JSON-объект по импортированной OpenAPI-схеме
// либо по объявленным скалярным полям ввода пакета.
// ValidateInput принимает только одно JSON object с закрытым набором primitive fields.
func (capability Capability) ValidateInput(raw []byte) ([]byte, error) {
	if capability.OpenAPI != nil {
		_, compiled, err := validateOpenAPIInputSchema(capability.OpenAPI.InputSchema)
		if err != nil {
			return nil, err
		}
		return validateOpenAPIInput(raw, compiled)
	}
	return validateObject(raw, capability.InputFields, "input")
}

// ValidateOutput canonicalizes only fields declared in the safe output projection.
// ValidateOutput канонизирует лишь поля, объявленные в безопасной проекции вывода.
// ValidateOutput принимает только безопасную проекцию с закрытым набором полей.
func (capability Capability) ValidateOutput(raw []byte) ([]byte, error) {
	return validateObject(raw, capability.OutputFields, "output")
}

// InputSchema builds the closed input JSON Schema from validated fields or a pinned OpenAPI binding;
// unsupported types cannot silently widen the tool input.
// InputSchema строит закрытую JSON Schema из проверенных полей или закреплённой OpenAPI-привязки;
// неподдержанный тип не расширяет ввод инструмента молча.
// InputSchema возвращает закрытую JSON Schema; её отдельный дайджест позволяет
// закрепить точную форму ввода при вызове инструмента.
func (capability Capability) InputSchema() ([]byte, error) {
	if capability.OpenAPI != nil {
		encoded, _, err := validateOpenAPIInputSchema(capability.OpenAPI.InputSchema)
		return encoded, err
	}
	properties := make(map[string]any, len(capability.InputFields))
	required := make([]string, 0, len(capability.InputFields))
	for _, field := range capability.InputFields {
		property := map[string]any{}
		switch field.Type {
		case "STRING":
			property["type"] = "string"
			property["minLength"] = 1
			if field.AllowEmpty {
				property["minLength"] = 0
			}
			property["maxLength"] = field.MaximumLength
			if len(field.AllowedValues) != 0 {
				property["enum"] = append([]string(nil), field.AllowedValues...)
			}
			if field.Format == "EMAIL" {
				property["format"] = "email"
			} else if field.Format == "HTTPS_URL" || field.Format == "HTTPS_ORIGIN" {
				property["format"] = "uri"
			}
		case "INTEGER":
			property["type"] = "integer"
			property["minimum"] = field.Minimum
			if field.Maximum != 0 {
				property["maximum"] = field.Maximum
			}
		case "BOOLEAN":
			property["type"] = "boolean"
		default:
			return nil, errors.New("integration input schema field type is invalid")
		}
		properties[field.Key] = property
		if field.Required {
			required = append(required, field.Key)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) != 0 {
		schema["required"] = required
	}
	return json.Marshal(schema)
}

// InputSchemaDigest hashes the canonical input schema for exact tool-contract pinning.
// InputSchemaDigest хэширует каноническую схему ввода для точной привязки контракта инструмента.
func (capability Capability) InputSchemaDigest() (string, error) {
	schema, err := capability.InputSchema()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(schema)
	return hex.EncodeToString(digest[:]), nil
}

// validateObject rejects undeclared, invalid or missing fields before emitting canonical JSON;
// the supplied kind changes only the safe diagnostic class, not field eligibility.
// validateObject отклоняет необъявленные, неверные и отсутствующие поля до канонического JSON;
// kind меняет лишь класс безопасной диагностики, но не допустимость полей.
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

// decodeJSONObject reads one bounded JSON object while rejecting duplicate keys, malformed keys
// and trailing content before semantic field validation.
// decodeJSONObject читает один ограниченный JSON-объект, отклоняя повторные и неверные ключи
// и хвостовые данные до смысловой проверки полей.
func decodeJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxObjectBytes {
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

// decodeFieldValue decodes one declared scalar without coercion and checks its string or numeric bounds.
// decodeFieldValue разбирает объявленный скаляр без приведения типов и проверяет строковые либо числовые границы.
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

// validateStringValue enforces length, allowed values and format-specific restrictions, including
// exact HTTPS host rules; multiline text is allowed only for explicitly plain input.
// validateStringValue проверяет длину, разрешённые значения и ограничения формата, включая
// точный HTTPS host; многострочный текст допустим лишь для явно простого ввода.
func validateStringValue(field Field, value string, allowPlainMultiline bool) error {
	if value == "" && !(field.AllowEmpty && allowPlainMultiline) || len(value) > field.MaximumLength || strings.ContainsRune(value, '\x00') ||
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
	case "HTTPS_PATH":
		if !ValidHTTPSResourcePath(value) {
			return errors.New("HTTPS resource path is invalid")
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

// ValidHTTPSResourcePath accepts an absolute literal path without query, fragment, percent encoding
// or traversal segments; the caller separately pins the path in the published operation.
// ValidHTTPSResourcePath принимает только абсолютный путь без query, fragment,
// percent-encoding и сегментов обхода; вызывающий код отдельно закрепляет путь в операции.
func ValidHTTPSResourcePath(value string) bool {
	if value == "" || value[0] != '/' || strings.HasPrefix(value, "//") || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("/-._~", character) {
			continue
		}
		return false
	}
	return true
}

// emailMailboxApproval allows selected email effects to defer to authoritative mailbox policy
// only for the exact managed email adapter and sender resource, not for unrelated packages.
// emailMailboxApproval разрешает выбранным почтовым эффектам обратиться к авторитетной политике ящика
// лишь для точного управляемого почтового адаптера и ресурса отправителя, а не чужих пакетов.
// Проверка авторитетной mailbox policy перед каждым эффектом принадлежит
// почтовому сервису; этот предикат лишь ограничивает исключение из package policy.
func emailMailboxApproval(result *Package, capability Capability) bool {
	if result.Metadata.Key != "email" ||
		result.Spec.Adapter != "EMAIL_HTTPS" || result.Spec.AdapterOwner != "integration-gateway" ||
		result.Spec.ExecutionRoute != "MANAGED_MCP" || capability.ResourceScope.Kind != "EMAIL_SENDER" ||
		capability.ApprovalPolicy != "NONE" {
		return false
	}
	switch capability.Operation {
	case "email.message.send", "email.message.reply", "email.message.reply_all", "email.message.forward",
		"email.message.delete", "email.message.mark_read", "email.message.mark_unread", "email.message.move",
		"email.message.archive", "email.draft.create", "email.draft.update", "email.draft.delete":
		return true
	default:
		return false
	}
}

// validate checks package metadata, adapter ownership, typed fields, exact network destinations,
// operation risk/approval/idempotency and a bounded read-only health capability as one closed contract.
// validate проверяет метаданные пакета, владельца адаптера, типизированные поля, точные сетевые
// назначения, риск/подтверждение/повторы операций и ограниченную health-операцию чтения как закрытый контракт.
func validate(result *Package) error {
	if result.APIVersion != APIVersion || result.Kind != Kind || !oneOf(result.Metadata.Origin, Origin, OriginUI, OriginGit) ||
		!validKey(result.Metadata.Key) || !versionPattern.MatchString(result.Metadata.Version) || len(result.Metadata.Version) > 32 ||
		len(result.Spec.Name) == 0 || len(result.Spec.Name) > 120 || len(result.Spec.Description) == 0 || len(result.Spec.Description) > 500 ||
		!validKey(result.Spec.Category) || !validAdapter(result.Spec.Adapter) || ValidateAdapterBinding(*result) != nil ||
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
		if field.AllowEmpty {
			return errors.New("integration configuration cannot allow empty values")
		}
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
	openAPIOperationIDs := map[string]struct{}{}
	for _, capability := range result.Spec.Capabilities {
		if !validKey(capability.Key) || len(capability.Name) == 0 || len(capability.Name) > 120 ||
			len(capability.Description) == 0 || len(capability.Description) > 500 || !validKey(capability.Operation) ||
			!validRisk(capability.Risk) || !validCapabilityApprovalPolicies(result, capability) ||
			!validResourceKind(capability.ResourceScope.Kind) ||
			len(capability.ResourceScope.ConnectionFields) == 0 || len(capability.ResourceScope.ConnectionFields) > 8 ||
			len(capability.InputFields) > 24 || len(capability.OutputFields) == 0 || len(capability.OutputFields) > 24 ||
			!validIdempotency(capability.Execution.Idempotency) ||
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
		if result.Spec.Adapter == string(AdapterOpenAPIMCP) {
			if err := validateOpenAPICapability(result, capability); err != nil {
				return err
			}
			if _, duplicate := openAPIOperationIDs[capability.OpenAPI.OperationID]; duplicate {
				return errors.New("OpenAPI operationId is duplicated")
			}
			openAPIOperationIDs[capability.OpenAPI.OperationID] = struct{}{}
		} else if capability.OpenAPI != nil || capability.Execution.Idempotency == string(IdempotencyOneAttempt) {
			return errors.New("integration capability exceeds adapter contract")
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
	if result.Spec.Adapter == string(AdapterOpenAPIMCP) {
		_, healthInputError := healthCapability.ValidateInput([]byte("{}"))
		if healthCapability.ApprovalPolicy != string(ApprovalNone) || healthInputError != nil {
			return errors.New("OpenAPI health operation must be a safe parameterless read")
		}
	}
	return nil
}

// validateFields rejects duplicate names and values, inconsistent types and formats, and widening
// bounds before any configuration, tool input or output is accepted.
// validateFields отклоняет повторные имена и значения, несовместимые типы и форматы и расширяющие
// границы до допуска конфигурации, ввода или вывода инструмента.
func validateFields(fields []Field) (map[string]struct{}, error) {
	keys := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !validKey(field.Key) || !validFieldType(field.Type) || !validFieldFormat(field.Format) ||
			(field.AllowEmpty && (field.Type != "STRING" || field.Format != "PLAIN" || len(field.AllowedValues) > 0)) ||
			field.MaximumLength < 0 || field.MaximumLength > maxFieldLength || field.Minimum < 0 || field.Maximum < 0 ||
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

// rejectUnsafeYAML recursively rejects aliases, anchors, merge keys, duplicate mapping keys
// and non-string keys before strict decoding of the integration package.
// rejectUnsafeYAML рекурсивно отклоняет alias, anchor, merge-ключи, повторные ключи карты
// и нестроковые ключи до строгого декодирования пакета интеграции.
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

// validKey confines package identifiers to bounded lowercase tokens with explicit separators.
// validKey ограничивает идентификаторы пакета строчными токенами с явными разделителями.
func validKey(value string) bool { return len(value) <= 120 && keyPattern.MatchString(value) }

// validHostname rejects IP literals, mixed-case names and invalid DNS labels for exact destinations.
// validHostname отклоняет IP-литералы, имена с заглавными буквами и неверные DNS-метки для точных назначений.
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

// contains checks exact case-sensitive membership in a field's declared value set.
// contains проверяет точное с учётом регистра вхождение в объявленный набор значений поля.
func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// oneOf accepts only byte-identical members of a closed enum; case-folded similarity is insufficient.
// oneOf допускает лишь побайтно совпадающее значение закрытого enum; похожего регистра недостаточно.
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) && value == candidate {
			return true
		}
	}
	return false
}
