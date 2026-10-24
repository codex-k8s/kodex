package emailpolicy

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"go.yaml.in/yaml/v3"
)

const (
	// DiagnosticSyntax identifies mailbox syntax or field-shape rejection without embedding untrusted
	// document content in the diagnostic.
	// DiagnosticSyntax обозначает отказ синтаксису либо форме полей mailbox без включения
	// непроверенного содержимого документа в диагностику.
	DiagnosticSyntax = "EMAIL_MAILBOX_SYNTAX_INVALID"
	// DiagnosticConfiguration identifies an incomplete or invalid mailbox configuration independently
	// of editor syntax and credential availability.
	// DiagnosticConfiguration обозначает неполную либо неверную конфигурацию mailbox отдельно от
	// синтаксиса редактора и доступности credentials.
	DiagnosticConfiguration = "EMAIL_MAILBOX_CONFIGURATION_INVALID"
	// DiagnosticCredential identifies unavailable mailbox credential references without revealing
	// their values or copying the source document.
	// DiagnosticCredential обозначает недоступные ссылки credentials mailbox без раскрытия их значений
	// либо копирования исходного документа.
	DiagnosticCredential = "EMAIL_MAILBOX_CREDENTIAL_MISMATCH"
)

// Diagnostic maps known mailbox diagnostic codes to fixed English runtime messages; unknown codes
// retain their code with an empty message.
// Diagnostic отображает известные коды диагностики mailbox в фиксированные английские
// runtime-сообщения; неизвестный код сохраняется с пустым сообщением.
func Diagnostic(code string) entity.EmailMailboxDiagnostic {
	message := map[string]string{DiagnosticSyntax: "Mailbox document syntax or fields are invalid", DiagnosticConfiguration: "Mailbox configuration is incomplete or invalid", DiagnosticCredential: "Mailbox credential reference is unavailable"}[code]
	return entity.EmailMailboxDiagnostic{Code: code, Message: message}
}

// DecodeSpecification accepts one bounded YAML/JSON mapping, rejects aliases, duplicate or unknown
// fields and unsafe numeric/enum shapes, and permits an incomplete draft without making it
// executable.
// DecodeSpecification принимает один ограниченный YAML/JSON mapping, отклоняет aliases, повторные
// либо неизвестные поля и небезопасные числа и enum, допуская неполный черновик без превращения
// его в исполняемый.
func DecodeSpecification(format, content string) (entity.EmailMailboxSpecification, error) {
	var result entity.EmailMailboxSpecification
	if len(content) > MaxMailboxSpecificationBytes || strings.TrimSpace(content) == "" {
		return result, errs.ErrInvalid
	}
	raw := []byte(content)
	if format == "YAML" || format == "JSON" {
		decoder := yaml.NewDecoder(strings.NewReader(content))
		var node yaml.Node
		if decoder.Decode(&node) != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
			return result, errs.ErrInvalid
		}
		var trailing yaml.Node
		if decoder.Decode(&trailing) != io.EOF {
			return result, errs.ErrInvalid
		}
		if !boundedMailboxYAML(&node, 0) {
			return result, errs.ErrInvalid
		}
		if format == "YAML" {
			var document map[string]any
			if node.Decode(&document) != nil {
				return result, errs.ErrInvalid
			}
			var err error
			raw, err = json.Marshal(document)
			if err != nil || len(raw) > MaxMailboxSpecificationBytes {
				return result, errs.ErrInvalid
			}
		}
	} else {
		return result, errs.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return result, errs.ErrInvalid
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || BoundSpecification(result) != nil || !knownMailboxEnums(result) {
		return entity.EmailMailboxSpecification{}, errs.ErrInvalid
	}
	return result, nil
}

// boundedMailboxYAML recursively bounds depth and per-node content, forbids anchors/aliases and
// requires unique string mapping keys before conversion or typed decoding.
// boundedMailboxYAML рекурсивно ограничивает глубину и содержимое каждого узла, запрещает anchors
// и aliases и требует уникальные строковые ключи mapping до преобразования либо типизированного
// декодирования.
func boundedMailboxYAML(node *yaml.Node, depth int) bool {
	if depth > 16 || node.Kind == yaml.AliasNode || node.Anchor != "" || len(node.Content) > 4000 {
		return false
	}
	if node.Kind == yaml.MappingNode {
		keys := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || keys[key.Value] {
				return false
			}
			keys[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if !boundedMailboxYAML(child, depth+1) {
			return false
		}
	}
	return true
}

// knownMailboxEnums permits omitted draft enum values but rejects unknown receive protocols,
// endpoint authentication/TLS modes and operation policies.
// knownMailboxEnums допускает отсутствующие enum черновика, но отклоняет неизвестные протоколы
// приёма, authentication/TLS endpoints и operation policies.
func knownMailboxEnums(spec entity.EmailMailboxSpecification) bool {
	if spec.ReceiveProtocol != "" && !spec.ReceiveProtocol.Valid() {
		return false
	}
	for _, endpoint := range []*api.Endpoint{&spec.SMTP, spec.IMAP, spec.POP} {
		if endpoint != nil && (endpoint.AuthMethod != "" && !endpoint.AuthMethod.Valid() || endpoint.TlsMode != "" && !endpoint.TlsMode.Valid()) {
			return false
		}
	}
	for _, policy := range spec.Policies {
		if policy.Operation != "" && !policy.Operation.Valid() || policy.Policy != "" && !policy.Policy.Valid() {
			return false
		}
	}
	return true
}

// CanonicalYAML round-trips typed specification through JSON field names into YAML for reopening a
// draft, without validating publication readiness or preserving source formatting.
// CanonicalYAML преобразует типизированную спецификацию через JSON-имена полей в YAML для
// повторного открытия черновика без проверки готовности публикации и сохранения исходного
// форматирования.
func CanonicalYAML(spec entity.EmailMailboxSpecification) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", errs.ErrInvalid
	}
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		return "", errs.ErrInvalid
	}
	yamlBytes, err := yaml.Marshal(document)
	if err != nil {
		return "", errs.ErrInvalid
	}
	return string(yamlBytes), nil
}
