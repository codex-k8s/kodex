package managedssh

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// InputKind separates typed managed recipes, arbitrary EACH-only execution, fixed identity inspection and owner-resolved receipt reads; it never carries a grant.
// InputKind разделяет typed managed recipes, arbitrary EACH-only execution, fixed identity inspection и owner-resolved receipt reads, не содержа grant.
type InputKind uint8

const (
	// ManagedInput contains only finite helper arguments; owner/broker add actual UUID and profile digest separately.
	// ManagedInput содержит лишь finite helper arguments; owner/broker отдельно добавляют actual UUID и profile digest.
	ManagedInput InputKind = 1
	// ExecuteInput retains an exact arbitrary command and cannot be classified as READ.
	// ExecuteInput сохраняет exact arbitrary command и не классифицируется как READ.
	ExecuteInput InputKind = 2
	// IdentityInput selects the fixed operator-qualified nonprivileged identity probe without a command field.
	// IdentityInput выбирает fixed operator-qualified nonprivileged identity probe без command field.
	IdentityInput InputKind = 3
	// ReceiptInput selects only an original invocation reference, never caller-supplied helper attempt or digest.
	// ReceiptInput выбирает лишь original invocation reference, не caller-supplied helper attempt либо digest.
	ReceiptInput InputKind = 4
)

// OperationInput retains validated owner-input bytes separately from helper pins and clears owned copies after use. Its fields are private so a decoded read cannot be changed into an arbitrary command.
// OperationInput хранит validated owner-input bytes отдельно от helper pins и очищает owned copies после использования. Private fields не позволяют заменить decoded read произвольной командой.
type OperationInput struct {
	kind       InputKind
	managed    Request
	command    string
	stdin      []byte
	maximum    uint32
	receiptRef string
	raw        []byte
}

// DecodeOperationInput admits only the canonical JSON object emitted by the owner, exact scalar types and operation-specific fields. Duplicate/case aliases, nulls, alternate integers/base64 and caller-selected authority fields fail before semantic use.
// DecodeOperationInput допускает лишь canonical JSON object владельца, exact scalar types и operation-specific fields. Duplicate/case aliases, null, alternate integers/base64 и caller-selected authority fields отклоняются до semantic use.
func DecodeOperationInput(operation string, raw []byte) (result OperationInput, resultErr error) {
	defer func() {
		if resultErr != nil {
			result.Clear()
		}
	}()
	fields, err := operationInputFields(raw)
	if err != nil {
		return result, err
	}
	keys := []string{}
	required := []string{}
	switch operation {
	case "ssh.execute":
		result.kind = ExecuteInput
		keys, required = []string{"command", "stdin_base64", "maximum_output"}, []string{"command", "maximum_output"}
	case "ssh.identity.inspect":
		result.kind = IdentityInput
	case ReceiptRead.Name():
		result.kind = ReceiptInput
		keys, required = []string{"receipt_invocation_ref"}, []string{"receipt_invocation_ref"}
	default:
		parsed, err := ParseOperation(operation)
		if err != nil {
			return result, ErrProtocol
		}
		result.kind, result.managed.Operation = ManagedInput, parsed
		switch parsed {
		case Inspect, SystemRead:
		case ProcessList:
			keys, required = []string{"limit"}, []string{"limit"}
		case FileList:
			keys, required = []string{"resource", "path", "limit"}, []string{"resource", "limit"}
		case FileStat:
			keys, required = []string{"resource", "path"}, []string{"resource", "path"}
		case FileRead:
			keys, required = []string{"resource", "path", "expected", "offset", "limit"}, []string{"resource", "path", "expected", "limit"}
		case FileCreate:
			keys, required = []string{"resource", "path", "content_base64"}, []string{"resource", "path", "content_base64"}
		case FileReplace:
			keys, required = []string{"resource", "path", "expected", "content_base64"}, []string{"resource", "path", "expected", "content_base64"}
		case FileDelete:
			keys, required = []string{"resource", "path", "expected"}, []string{"resource", "path", "expected"}
		case ServiceRead, ServiceRestart, ServiceReload:
			keys, required = []string{"resource"}, []string{"resource"}
		case JournalRead:
			keys, required = []string{"resource", "limit"}, []string{"resource", "limit"}
		default:
			return result, ErrProtocol
		}
	}
	for _, key := range required {
		if _, exists := fields[key]; !exists {
			return result, ErrProtocol
		}
	}
	for key, value := range fields {
		allowed := false
		for _, candidate := range keys {
			if key == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			return result, ErrProtocol
		}
		switch key {
		case "offset", "limit", "maximum_output":
			number, ok := value.(json.Number)
			if !ok {
				return result, ErrProtocol
			}
			parsed, err := strconv.ParseUint(string(number), 10, 64)
			if err != nil || string(number) != strconv.FormatUint(parsed, 10) {
				return result, ErrProtocol
			}
			if key == "offset" {
				result.managed.Offset = parsed
			} else {
				if parsed > uint64(^uint32(0)) {
					return result, ErrProtocol
				}
				if key == "limit" {
					result.managed.Limit = uint32(parsed)
				} else {
					result.maximum = uint32(parsed)
				}
			}
		default:
			text, ok := value.(string)
			if !ok {
				return result, ErrProtocol
			}
			switch key {
			case "resource":
				result.managed.Resource = text
			case "path":
				result.managed.Path = text
			case "expected":
				result.managed.Expected = text
			case "command":
				result.command = text
			case "receipt_invocation_ref":
				result.receiptRef = text
			case "content_base64", "stdin_base64":
				decoded, err := decodeOperationContent(text)
				if err != nil {
					return result, err
				}
				if key == "stdin_base64" {
					result.stdin = decoded
				} else {
					result.managed.Content = decoded
				}
			}
		}
	}
	switch result.kind {
	case ManagedInput:
		if result.managed.validateArguments() != nil {
			return result, ErrProtocol
		}
	case ExecuteInput:
		if len(result.command) == 0 || len(result.command) > MaximumCommand || !utf8.ValidString(result.command) || strings.ContainsRune(result.command, 0) || result.maximum == 0 || result.maximum > MaximumReply {
			return result, ErrProtocol
		}
	case ReceiptInput:
		if !OwnerReference(result.receiptRef) || !strings.HasPrefix(result.receiptRef, "inv_") {
			return result, ErrProtocol
		}
	}
	result.raw = bytes.Clone(raw)
	return result, nil
}

// operationInputFields validates one bounded canonical flat scalar object before any operation-specific interpretation, preserving duplicate detection and rejecting malformed UTF encodings.
// operationInputFields проверяет bounded canonical flat scalar object до operation-specific interpretation, сохраняя duplicate detection и отказ malformed UTF encoding.
func operationInputFields(raw []byte) (map[string]any, error) {
	if len(raw) < 2 || len(raw) > MaximumFrame || !utf8.Valid(raw) {
		return nil, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrProtocol
	}
	fields := make(map[string]any)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || len(fields) >= 5 {
			return nil, ErrProtocol
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ErrProtocol
		}
		value, err := decoder.Token()
		if err != nil {
			return nil, ErrProtocol
		}
		switch value.(type) {
		case string, json.Number:
		default:
			return nil, ErrProtocol
		}
		fields[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrProtocol
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrProtocol
	}
	canonical, err := json.Marshal(fields)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, ErrProtocol
	}
	return fields, nil
}

// decodeOperationContent accepts the sole standard padded base64 form and checks actual decoded bytes; the encoded length alone could otherwise admit two extra bytes.
// decodeOperationContent принимает единственную standard padded base64 form и проверяет actual decoded bytes; одного encoded length недостаточно для исключения двух лишних bytes.
func decodeOperationContent(text string) ([]byte, error) {
	if len(text) > base64.StdEncoding.EncodedLen(MaximumContent) {
		return nil, ErrProtocol
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) > MaximumContent || base64.StdEncoding.EncodeToString(decoded) != text {
		clear(decoded)
		return nil, ErrProtocol
	}
	return decoded, nil
}

// Kind reports the closed decoded input family, never a caller-controlled authority classification.
// Kind сообщает closed decoded input family, а не caller-controlled authority classification.
func (input OperationInput) Kind() InputKind { return input.kind }

// Effect fixes arbitrary execute to EACH and preserves the built-in recipe floor; strings are never inspected to guess safe shell execution.
// Effect фиксирует arbitrary execute как EACH и сохраняет built-in recipe floor без попыток угадать safe shell execution по строке.
func (input OperationInput) Effect() (EffectClass, error) {
	switch input.kind {
	case ManagedInput:
		return input.managed.Operation.Effect()
	case ExecuteInput:
		return EachChange, nil
	case IdentityInput, ReceiptInput:
		return ReadOnly, nil
	}
	return 0, ErrProtocol
}

// Request adds actual owner/broker-resolved helper pins only for managed input; receipt recovery must separately resolve the original invocation and cannot use the new caller's attempt.
// Request добавляет actual owner/broker-resolved helper pins лишь managed input; receipt recovery отдельно разрешает original invocation и не использует attempt нового caller.
func (input OperationInput) Request(attempt, profileDigest string) (Request, error) {
	if input.kind != ManagedInput {
		return Request{}, ErrProtocol
	}
	request := input.managed
	request.Version, request.AttemptID, request.ProfileDigest = Version, attempt, profileDigest
	if request.Validate() != nil {
		return Request{}, ErrProtocol
	}
	request.Content = bytes.Clone(request.Content)
	return request, nil
}

// Execution returns an owned stdin copy only for arbitrary execute input; the caller must retain EACH admission and clear bytes after joined cleanup.
// Execution возвращает owned stdin copy лишь arbitrary execute input; caller сохраняет EACH admission и очищает bytes после joined cleanup.
func (input OperationInput) Execution() (string, []byte, uint32, error) {
	if input.kind != ExecuteInput {
		return "", nil, 0, ErrProtocol
	}
	return input.command, bytes.Clone(input.stdin), input.maximum, nil
}

// ReceiptReference exposes only the addressed original invocation locator; ownership, original operation/digest and same target resolution remain authoritative owner work.
// ReceiptReference предоставляет лишь locator original invocation; ownership, original operation/digest и same target разрешает authoritative owner.
func (input OperationInput) ReceiptReference() (string, error) {
	if input.kind != ReceiptInput {
		return "", ErrProtocol
	}
	return input.receiptRef, nil
}

// Digest hashes the exact validated immutable owner input, not helper output, profile claims or a reusable grant.
// Digest хеширует exact validated immutable owner input, не helper output, profile claims либо reusable grant.
func (input OperationInput) Digest() (string, error) {
	if input.kind == 0 || len(input.raw) == 0 {
		return "", ErrProtocol
	}
	sum := sha256.Sum256(input.raw)
	return hex.EncodeToString(sum[:]), nil
}

// Clear releases owned content and canonical input copies; any separately returned request/stdin copy remains owned by its caller.
// Clear освобождает owned content и canonical input copies; отдельно возвращёнными request/stdin copies владеет caller.
func (input *OperationInput) Clear() {
	if input == nil {
		return
	}
	clear(input.raw)
	clear(input.stdin)
	clear(input.managed.Content)
	*input = OperationInput{}
}

// String hides commands, stdin, paths and original invocation coordinates from ordinary formatting.
// String скрывает commands, stdin, paths и original invocation coordinates при ordinary formatting.
func (OperationInput) String() string { return "[SSH private operation input]" }

// GoString preserves the same redaction for detailed formatting.
// GoString сохраняет ту же redaction при detailed formatting.
func (input OperationInput) GoString() string { return input.String() }
