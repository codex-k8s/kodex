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
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// namePattern accepts only bounded lowercase descriptor and mailbox names.
// RU: namePattern допускает только ограниченные по длине строчные имена дескрипторов и ящиков.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// schemaRaw holds the generated, embedded JSON schema used for strict decoding.
// RU: schemaRaw хранит встроенную сгенерированную JSON-схему для строгого декодирования.
//
//go:embed schema.gen.json
var schemaRaw []byte

// schemasOnce compiles the embedded model schemas at most once per process.
// RU: schemasOnce компилирует встроенные схемы моделей не более одного раза за процесс.
var schemasOnce sync.Once

// schemas indexes compiled validators by their registered model names.
// RU: schemas индексирует скомпилированные валидаторы по зарегистрированным именам моделей.
var schemas map[string]*jsonschema.Schema

// schemasError preserves a schema loading failure for all subsequent callers.
// RU: schemasError сохраняет ошибку загрузки схем для всех последующих вызовов.
var schemasError error

// validateDocument selects a closed set of wire models and validates normalized
// data against the embedded schema before populating a Go target.
// RU: validateDocument выбирает модель из закрытого набора и проверяет
// нормализованные данные по встроенной схеме до заполнения Go-объекта.
func validateDocument(data any, target any) error {
	name := ""
	switch target.(type) {
	case *Configuration:
		name = "Configuration"
	case *Command:
		name = "Command"
	case *ExecutionBinding:
		name = "ExecutionBinding"
	case *MessageInput:
		name = "MessageInput"
	case *AuthorizationDecision:
		name = "AuthorizationDecision"
	case *AuthorizationRequest:
		name = "AuthorizationRequest"
	case *IntegrationInput:
		name = "IntegrationInput"
	case *Attachments:
		name = "Attachments"
	case *Recipients:
		name = "Recipients"
	case *Result:
		name = "Result"
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
		for _, n := range []string{"Configuration", "Command", "ExecutionBinding", "MessageInput", "AuthorizationDecision", "AuthorizationRequest", "IntegrationInput", "Attachments", "Recipients", "Result"} {
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

// Decode accepts exactly one size-bounded JSON or YAML document, rejecting
// aliases, duplicate keys, unknown fields and schema-invalid input.
// RU: Decode принимает ровно один ограниченный по размеру документ JSON/YAML,
// отклоняя ссылки, повторные ключи, неизвестные поля и нарушение схемы.
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

// checkNode walks the parsed YAML tree, bounding depth and rejecting anchors,
// aliases and duplicate or non-string mapping keys before model decoding.
// RU: checkNode обходит дерево YAML с ограничением глубины и отклоняет якоря,
// ссылки и повторные либо нестроковые ключи до декодирования модели.
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

// Digest returns a SHA-256 digest of the JSON form of a validated model;
// callers bind immutable source identity to these serialized bytes.
// RU: Digest возвращает SHA-256 от JSON-формы проверенной модели; вызывающие
// связывают неизменяемую идентичность источника с сериализованными данными.
func Digest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Address accepts a plain canonical mailbox address without display name,
// line breaks or NUL, bounded by the wire contract's length limit.
// RU: Address принимает обычный канонический адрес без отображаемого имени,
// переводов строки и NUL с ограничением длины по контракту.
func Address(value string) bool {
	a, e := mail.ParseAddress(value)
	return e == nil && a.Address == value && a.Name == "" && len(value) <= 320 && !strings.ContainsAny(value, "\r\n\x00")
}

// Host accepts a lowercase ASCII DNS hostname with valid LDH labels, while
// rejecting IP literals and malformed or oversized names.
// RU: Host принимает строчное ASCII-имя DNS с корректными LDH-метками,
// отклоняя IP-литералы и некорректные либо слишком длинные имена.
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

// DescriptorValid requires a bounded descriptor name and positive generation;
// secret values remain outside this wire model.
// RU: DescriptorValid требует ограниченное имя дескриптора и положительное
// поколение; сами значения секретов не входят в эту модель передачи.
func DescriptorValid(d Descriptor) bool { return namePattern.MatchString(d.Name) && d.Generation > 0 }

// Folder accepts a nonempty bounded mailbox folder without wildcard or
// control characters that could change server-side selection.
// RU: Folder принимает непустую папку ограниченной длины без шаблонных и
// управляющих символов, меняющих выборку на почтовом сервере.
func Folder(value string) bool {
	return value != "" && len(value) <= 255 && !strings.ContainsAny(value, "*%\r\n\x00")
}

// Operations returns the complete closed list of mail operations for which
// every mailbox configuration must carry an explicit policy decision.
// RU: Operations возвращает полный закрытый список почтовых операций, для
// каждой из которых конфигурация ящика обязана содержать явную политику.
func Operations() []Operation {
	return []Operation{OperationHealth, OperationMailboxes, OperationList, OperationSearch, OperationFetch, OperationDownload, OperationMark, OperationDelete, OperationSend, OperationReply, OperationReplyAll, OperationForward, OperationReceipt, OperationThread, OperationAttachments, OperationMarkRead, OperationMarkUnread, OperationMove, OperationArchive, OperationDraftCreate, OperationDraftUpdate, OperationDraftDelete}
}

// ValidateConfiguration enforces mailbox ownership metadata, descriptor
// generations, endpoints, folders, limits and one policy for every operation.
// It rejects malformed configuration before a mail transport can use it.
// RU: ValidateConfiguration проверяет метаданные владения ящиком, поколения
// дескрипторов, узлы, папки, лимиты и политику для каждой операции, отклоняя
// ошибочную конфигурацию до обращения к почтовому транспорту.
func ValidateConfiguration(c Configuration) error {
	bad := errors.New("invalid email configuration")
	if c.Version != "email-bridge/v1" || c.Revision < 1 || (c.ManagedBy != "ui" && c.ManagedBy != "git") || c.Source == "" || len(c.Source) > 512 || len(c.Mailboxes) > 100 {
		return bad
	}
	seen := map[string]bool{}
	for _, m := range c.Mailboxes {
		if !namePattern.MatchString(m.Id) || seen[m.Id] || m.TenantId == "" || m.ConnectionId == "" || m.Revision < 1 || m.CredentialGeneration < 1 || !Folder(m.Folder) || !Address(m.Sender) || !Address(m.ReplyTo) || m.EnvelopeFrom != m.Sender || !Host(m.HelloName) {
			return bad
		}
		if len(m.AllowedFolders) == 0 || len(m.AllowedFolders) > 100 || !slices.Contains(m.AllowedFolders, m.Folder) {
			return bad
		}
		folders := map[string]bool{}
		for _, folder := range m.AllowedFolders {
			if !Folder(folder) || folders[folder] {
				return bad
			}
			folders[folder] = true
		}
		for _, folder := range []string{m.ArchiveFolder, m.DraftsFolder} {
			if folder != "" && !folders[folder] {
				return bad
			}
		}
		if (m.ReceiveProtocol != "imap" && m.ReceiveProtocol != "pop3") || (m.ReceiveProtocol == "imap" && m.Imap == nil) || (m.ReceiveProtocol == "pop3" && (m.Pop == nil || m.Folder != "INBOX" || len(m.AllowedFolders) != 1)) {
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
		endpoints := []*Endpoint{&m.Smtp, m.Pop, m.Imap}
		for _, e := range endpoints {
			if e == nil {
				continue
			}
			if !Host(e.Host) || e.ServerName != e.Host || e.Port < 1 || e.Port > 65535 || (e.TlsMode != "implicit" && e.TlsMode != "starttls") || (e.AuthMethod != "password" && e.AuthMethod != "oauthbearer") || !DescriptorValid(e.Ca) || !DescriptorValid(e.Username) || !DescriptorValid(e.Secret) {
				return bad
			}
		}
		if m.Pop != nil && m.Pop.AuthMethod != "password" {
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
			for _, folder := range p.Folders {
				if !folders[folder] {
					return bad
				}
			}
		}
		if len(ops) != len(Operations()) {
			return bad
		}
	}
	return nil
}
