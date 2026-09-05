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
	DiagnosticSyntax        = "EMAIL_MAILBOX_SYNTAX_INVALID"
	DiagnosticConfiguration = "EMAIL_MAILBOX_CONFIGURATION_INVALID"
	DiagnosticCredential    = "EMAIL_MAILBOX_CREDENTIAL_MISMATCH"
)

func Diagnostic(code string) entity.EmailMailboxDiagnostic {
	message := map[string]string{DiagnosticSyntax: "Mailbox document syntax or fields are invalid", DiagnosticConfiguration: "Mailbox configuration is incomplete or invalid", DiagnosticCredential: "Mailbox credential reference is unavailable"}[code]
	return entity.EmailMailboxDiagnostic{Code: code, Message: message}
}

// DecodeSpecification не сохраняет непроверенный YAML и не включает значения в ошибки.
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
