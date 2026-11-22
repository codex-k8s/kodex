package managedssh

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version pins the canonical request/reply model; unsupported versions never fall back to shell or a previous decoder.
// Version закрепляет canonical request/reply model; неизвестная версия не переключается на shell либо прежний decoder.
const Version = 1

// MaximumFrame bounds canonical JSON before decoding; the caller separately bounds the SSH channel and process lifetime.
// MaximumFrame ограничивает canonical JSON до декодирования; caller отдельно ограничивает SSH channel и process lifetime.
const MaximumFrame = 65536

// MaximumReply matches the broker's combined output budget; larger read collections require explicit record-boundary truncation before encoding.
// MaximumReply соответствует combined output budget broker; большие read collections требуют явного усечения по границам records до сериализации.
const MaximumReply = 32768

// MaximumContent bounds one managed file change or returned file fragment, excluding its base64 framing overhead.
// MaximumContent ограничивает одно изменение managed file либо возвращаемый file fragment без base64 framing overhead.
const MaximumContent = 16384

// MaximumCommand bounds the exact arbitrary exec string before transport; a command string never becomes a READ recipe through parsing or a prefix.
// MaximumCommand ограничивает exact arbitrary exec string до transport; parsing либо prefix не превращают command string в READ recipe.
const MaximumCommand = 8192

// Request carries a finite recipe and immutable pins, not credentials, executable names, caller authority or an extensible argument object.
// Request содержит конечный recipe и immutable pins, а не credentials, executable names, caller authority либо расширяемый argument object.
type Request struct {
	Version       uint8     `json:"version"`
	ProfileDigest string    `json:"profile_digest"`
	AttemptID     string    `json:"attempt_id"`
	Operation     Operation `json:"operation"`
	Resource      string    `json:"resource,omitempty"`
	Path          string    `json:"path,omitempty"`
	Offset        uint64    `json:"offset,omitempty"`
	Limit         uint32    `json:"limit,omitempty"`
	Expected      string    `json:"expected,omitempty"`
	Content       []byte    `json:"content,omitempty"`
}

// Validate enforces operation-specific arguments and denies unused fields; resource aliases are still resolved against the operator-owned profile by the helper.
// Validate проверяет operation-specific arguments и запрещает лишние поля; resource aliases helper отдельно разрешает по operator-owned profile.
func (request Request) Validate() error {
	if request.Version != Version || !Digest(request.ProfileDigest) || !attemptID(request.AttemptID) {
		return ErrProtocol
	}
	return request.validateArguments()
}

// validateArguments shares finite argument validation with the owner-input decoder before a broker can add trusted attempt/profile pins; it grants no dispatch authority.
// validateArguments разделяет finite argument validation с owner-input decoder до добавления broker trusted attempt/profile pins и не выдаёт dispatch authority.
func (request Request) validateArguments() error {
	if request.Operation.Name() == "" || len(request.Content) > MaximumContent {
		return ErrProtocol
	}
	if request.Resource != "" && !Alias(request.Resource) || request.Path != "" && !RelativePath(request.Path) || request.Expected != "" && !Digest(request.Expected) {
		return ErrProtocol
	}
	noContent := len(request.Content) == 0
	noFile := request.Path == "" && request.Expected == "" && request.Offset == 0 && noContent
	valid := false
	switch request.Operation {
	case Inspect, SystemRead, ReceiptRead:
		valid = request.Resource == "" && noFile && request.Limit == 0
	case ProcessList:
		valid = request.Resource == "" && noFile && request.Limit >= 1 && request.Limit <= 128
	case FileList:
		valid = request.Resource != "" && request.Expected == "" && request.Offset == 0 && noContent && request.Limit >= 1 && request.Limit <= 64
	case FileStat:
		valid = request.Resource != "" && request.Path != "" && request.Expected == "" && request.Offset == 0 && request.Limit == 0 && noContent
	case FileRead:
		valid = request.Resource != "" && request.Path != "" && request.Expected != "" && request.Offset <= 1<<20 && request.Limit >= 1 && request.Limit <= MaximumContent && noContent
	case FileCreate:
		valid = request.Resource != "" && request.Path != "" && request.Expected == "" && request.Offset == 0 && request.Limit == 0
	case FileReplace:
		valid = request.Resource != "" && request.Path != "" && request.Expected != "" && request.Offset == 0 && request.Limit == 0
	case FileDelete:
		valid = request.Resource != "" && request.Path != "" && request.Expected != "" && request.Offset == 0 && request.Limit == 0 && noContent
	case ServiceRead, ServiceRestart, ServiceReload:
		valid = request.Resource != "" && noFile && request.Limit == 0
	case JournalRead:
		valid = request.Resource != "" && noFile && request.Limit >= 1 && request.Limit <= 64
	}
	if !valid {
		return ErrProtocol
	}
	return nil
}

// EncodeRequest validates then produces the only accepted JSON representation; the digest binds these exact bytes to the owner intent and helper receipt.
// EncodeRequest проверяет и создаёт единственное допустимое JSON представление; digest связывает exact bytes с owner intent и helper receipt.
func EncodeRequest(request Request) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, ErrProtocol
	}
	return canonical(request)
}

// DecodeRequest rejects noncanonical JSON, aliases, duplicate/unknown keys, null scalars, alternate encodings and trailing bytes before semantic use.
// DecodeRequest до семантического использования отклоняет noncanonical JSON, aliases, duplicate/unknown keys, null scalars, alternate encodings и trailing bytes.
func DecodeRequest(raw []byte) (Request, error) {
	var request Request
	if decodeCanonical(raw, &request) != nil || request.Validate() != nil {
		return Request{}, ErrProtocol
	}
	return request, nil
}

// RequestDigest hashes only a valid canonical request, including its operation, profile, attempt, exact path, version and content.
// RequestDigest хеширует только valid canonical request, включая operation, profile, attempt, exact path, version и content.
func RequestDigest(request Request) (string, error) {
	raw, err := EncodeRequest(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// Digest recognizes one nonzero lowercase SHA-256 hexadecimal value; no absent or wildcard digest can authorize a version-bound read or mutation.
// Digest распознаёт один nonzero lowercase SHA-256 hexadecimal value; отсутствие либо wildcard digest не допускают version-bound read либо mutation.
func Digest(value string) bool {
	if len(value) != 64 || value == strings.Repeat("0", 64) {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

// Alias recognizes a bounded ASCII operator-selected alias rather than a path, program, unit name or tenant identifier supplied as authority.
// Alias распознаёт bounded ASCII alias, выбранный оператором, а не path, program, unit name либо tenant identifier, переданный как authority.
func Alias(value string) bool {
	if len(value) < 1 || len(value) > 48 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '-' && character != '_' {
					return false
				}
			}
		}
	}
	return true
}

// RelativePath validates a bounded root-relative spelling; the Linux helper must additionally enforce descriptor-relative no-symlink/no-mount traversal.
// RelativePath проверяет bounded root-relative spelling; Linux helper дополнительно обязан обеспечить descriptor-relative no-symlink/no-mount traversal.
func RelativePath(value string) bool {
	if len(value) < 1 || len(value) > 1024 || !utf8.ValidString(value) || strings.Contains(value, "\\") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return false
		}
	}
	return true
}

// attemptID recognizes a canonical nonzero UUID spelling; generation and ownership are assigned by CP, never inferred from this syntax.
// attemptID распознаёт canonical nonzero UUID spelling; generation и ownership назначает CP, а не эта синтаксическая проверка.
func attemptID(value string) bool {
	if len(value) != 36 || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
		} else if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

// canonical marshals bounded immutable protocol DTOs without custom parsers, executable renderers or extension objects.
// canonical сериализует bounded immutable protocol DTOs без custom parsers, executable renderers либо extension objects.
func canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumFrame {
		return nil, ErrProtocol
	}
	return raw, nil
}

// decodeCanonical compares the typed standard-library round trip with the complete original frame; any discarded or normalized JSON syntax fails closed.
// decodeCanonical сравнивает typed standard-library round trip с полным исходным frame; любой отброшенный либо нормализованный JSON syntax закрыто отклоняется.
func decodeCanonical(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > MaximumFrame || json.Unmarshal(raw, value) != nil {
		return ErrProtocol
	}
	encoded, err := canonical(value)
	if err != nil || !bytes.Equal(raw, encoded) {
		return ErrProtocol
	}
	return nil
}
