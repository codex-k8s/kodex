package emailbridgeapi

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

//go:embed schema.gen.json
var schemaRaw []byte
var schemasOnce sync.Once
var schemas map[string]*jsonschema.Schema
var schemasError error

func validateDocument(data any, target any) error {
	name := ""
	switch target.(type) {
	case *Configuration:
		name = "Configuration"
	case *Command:
		name = "Command"
	case *MessageInput:
		name = "MessageInput"
	case *AuthorizationDecision:
		name = "AuthorizationDecision"
	case *AuthorizationRequest:
		name = "AuthorizationRequest"
	default:
		return errors.New("unregistered document model")
	}
	schemasOnce.Do(func() {
		var source any
		if schemasError = json.Unmarshal(schemaRaw, &source); schemasError != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		if schemasError = compiler.AddResource("https://kodex.invalid/email.schema.json", source); schemasError != nil {
			return
		}
		schemas = map[string]*jsonschema.Schema{}
		for _, n := range []string{"Configuration", "Command", "MessageInput", "AuthorizationDecision", "AuthorizationRequest"} {
			schemas[n], schemasError = compiler.Compile("https://kodex.invalid/email.schema.json#/$defs/" + n)
			if schemasError != nil {
				return
			}
		}
	})
	if schemasError != nil {
		return errors.New("email schema unavailable")
	}
	if schemas[name].Validate(data) != nil {
		return errors.New("email schema validation failed")
	}
	return nil
}

// Decode принимает одну строгую JSON/YAML модель без aliases и повторных ключей.
func Decode(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 24<<20 {
		return errors.New("invalid document size")
	}
	var node yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&node) != nil || checkNode(&node, 0) != nil {
		return errors.New("invalid document")
	}
	var trailing yaml.Node
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("multiple documents are forbidden")
	}
	var data any
	if node.Decode(&data) != nil {
		return errors.New("invalid document")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return errors.New("invalid document")
	}
	var normalized any
	if json.Unmarshal(encoded, &normalized) != nil || validateDocument(normalized, target) != nil {
		return errors.New("invalid document schema")
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return errors.New("invalid document fields")
	}
	return nil
}

func checkNode(n *yaml.Node, depth int) error {
	if depth > 32 || n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("invalid document tree")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
				return errors.New("invalid document key")
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := checkNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func Digest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func Address(value string) bool {
	a, e := mail.ParseAddress(value)
	return e == nil && a.Address == value && a.Name == "" && len(value) <= 320 && !strings.ContainsAny(value, "\r\n\x00")
}
func Host(value string) bool {
	if len(value) > 253 || net.ParseIP(value) != nil || value != strings.ToLower(value) || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func DescriptorValid(d Descriptor) bool { return namePattern.MatchString(d.Name) && d.Generation > 0 }

func ValidateConfiguration(c Configuration) error {
	bad := errors.New("invalid email configuration")
	if c.Version != "email-bridge/v1" || c.Revision < 1 || (c.ManagedBy != "ui" && c.ManagedBy != "git") || c.Source == "" || len(c.Source) > 512 || len(c.Mailboxes) > 100 {
		return bad
	}
	seen := map[string]bool{}
	for _, m := range c.Mailboxes {
		if !namePattern.MatchString(m.Id) || seen[m.Id] || m.TenantId == "" || m.ConnectionId == "" || m.Revision < 1 || m.Folder != "INBOX" || !Address(m.Sender) || m.EnvelopeFrom != m.Sender || !Host(m.HelloName) {
			return bad
		}
		seen[m.Id] = true
		if len(m.Recipients) == 0 || len(m.Recipients) > 1000 {
			return bad
		}
		for _, r := range m.Recipients {
			if !Address(r) {
				return bad
			}
		}
		for _, e := range []Endpoint{m.Pop, m.Smtp} {
			if !Host(e.Host) || e.ServerName != e.Host || e.Port < 1 || e.Port > 65535 || (e.TlsMode != "implicit" && e.TlsMode != "starttls") || !DescriptorValid(e.Ca) || !DescriptorValid(e.Username) || !DescriptorValid(e.Password) || e.Username.Generation != e.Password.Generation {
				return bad
			}
		}
		if m.Pop.Password.Generation != m.Smtp.Password.Generation {
			return bad
		}
		l := m.Limits
		if l.TimeoutSeconds < 1 || l.TimeoutSeconds > 60 || l.MessageBytes < 1024 || l.MessageBytes > 16<<20 || l.AttachmentBytes < 1 || l.AttachmentBytes > 8<<20 || l.AttachmentBytes > l.MessageBytes || l.MaxAttachments < 0 || l.MaxAttachments > 20 || l.MaxRecipients < 1 || l.MaxRecipients > 100 || l.PageSize < 1 || l.PageSize > 100 || l.ScanMessages < l.PageSize || l.ScanMessages > 1000 {
			return bad
		}
		ops := map[Operation]bool{}
		for _, p := range m.Policies {
			if !p.Operation.Valid() || !p.Policy.Valid() || ops[p.Operation] {
				return bad
			}
			ops[p.Operation] = true
		}
		if len(ops) != 13 {
			return bad
		}
	}
	return nil
}
