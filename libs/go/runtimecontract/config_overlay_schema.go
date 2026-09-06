package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const (
	ReasoningSupported       = "SUPPORTED"
	ReasoningUnsupported     = "UNSUPPORTED"
	OverlaySyntaxInvalid     = "CONFIG_OVERLAY_SYNTAX_INVALID"
	OverlayKeyForbidden      = "CONFIG_OVERLAY_KEY_FORBIDDEN"
	OverlayValueInvalid      = "CONFIG_OVERLAY_VALUE_INVALID"
	OverlayEffortUnsupported = "CONFIG_OVERLAY_EFFORT_UNSUPPORTED"
)

var overlayEffortPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func ValidateEffectiveReasoningEffort(overlay, effective, mode string) error {
	parsed, err := ParseConfigOverlay(overlay)
	if err != nil {
		return errors.New("effective reasoning effort is invalid")
	}
	if mode == ReasoningUnsupported && effective == "" && parsed.ModelReasoningEffort == "" {
		return nil
	}
	if mode != ReasoningSupported || !overlayEffortPattern.MatchString(effective) || parsed.ModelReasoningEffort != "" && parsed.ModelReasoningEffort != effective {
		return errors.New("effective reasoning effort is invalid")
	}
	return nil
}

type ConfigOverlayDiagnostic struct {
	Code    string `json:"code"`
	Key     string `json:"key"`
	Line    int32  `json:"line"`
	Column  int32  `json:"column"`
	Message string `json:"message"`
}

type ConfigOverlayField struct {
	Key           string   `json:"key"`
	ValueType     string   `json:"valueType"`
	AllowedValues []string `json:"allowedValues"`
	DefaultValue  string   `json:"defaultValue"`
	Description   string   `json:"description"`
	Completion    string   `json:"completion"`
	Hover         string   `json:"hover"`
}

type ConfigOverlaySchema struct {
	Revision     string               `json:"revision"`
	Digest       string               `json:"digest"`
	Fields       []ConfigOverlayField `json:"fields"`
	MaximumBytes int32                `json:"maximumBytes"`
}

// OverlaySchema использует model-specific пересечение, рассчитанное owner.
// Пустой список не разрешает UI придумывать effort; structural parser отдельно
// проверяет форму строки перед обязательной owner-проверкой совместимости.
func OverlaySchema(efforts []string, defaultEffort string) ConfigOverlaySchema {
	fields := overlayFields(efforts, defaultEffort)
	schema := ConfigOverlaySchema{Fields: fields, MaximumBytes: MaximumConfigOverlayBytes}
	raw, _ := json.Marshal(schema)
	digest := sha256.Sum256(raw)
	schema.Digest = hex.EncodeToString(digest[:])
	schema.Revision = "cos_" + schema.Digest
	return schema
}

func overlayFields(efforts []string, defaultEffort string) []ConfigOverlayField {
	return []ConfigOverlayField{
		{Key: "model_reasoning_effort", ValueType: "string", AllowedValues: slices.Clone(efforts), DefaultValue: defaultEffort,
			Description: "Степень рассуждения выбранной модели", Completion: "model_reasoning_effort = ", Hover: "Допустимые значения определяются exact каталогами выбранных provider accounts."},
		{Key: "personality", ValueType: "string", AllowedValues: []string{"none", "friendly", "pragmatic"},
			Description: "Стиль ответов", Completion: "personality = ", Hover: "Не изменяет полномочия или ограничения runtime."},
		{Key: "allow_login_shell", ValueType: "boolean", AllowedValues: []string{"false"}, DefaultValue: "false",
			Description: "Запрет login shell", Completion: "allow_login_shell = false", Hover: "Разрешено только false."},
		{Key: "history.persistence", ValueType: "string", AllowedValues: []string{"save-all", "none"}, DefaultValue: "save-all",
			Description: "Сохранение истории", Completion: "history.persistence = ", Hover: "save-all сохраняет историю; none отключает её сохранение."},
	}
}

type overlayPositionProbe struct{}

func (*overlayPositionProbe) UnmarshalTOML(any) error {
	return errors.New("overlay diagnostic location")
}

// DiagnoseConfigOverlay не возвращает Message/LastKey из parser: они могут
// содержать исходные значения. PrimitiveDecode сохраняет точную позицию TOML,
// включая dotted/quoted keys и inline tables, без второго самописного parser.
func DiagnoseConfigOverlay(raw string, efforts []string) []ConfigOverlayDiagnostic {
	if len(raw) > MaximumConfigOverlayBytes || !utf8.ValidString(raw) || strings.ContainsRune(raw, '\x00') {
		return []ConfigOverlayDiagnostic{{Code: OverlaySyntaxInvalid, Message: "Overlay size or encoding is invalid"}}
	}
	var root map[string]toml.Primitive
	metadata, err := toml.Decode(raw, &root)
	if err != nil {
		diagnostic := ConfigOverlayDiagnostic{Code: OverlaySyntaxInvalid, Message: "Overlay TOML syntax is invalid"}
		var parseError toml.ParseError
		if errors.As(err, &parseError) {
			diagnostic.Line, diagnostic.Column = int32(parseError.Position.Line), int32(parseError.Position.Col)
		}
		return []ConfigOverlayDiagnostic{diagnostic}
	}
	fields := overlayFields(efforts, "")
	var diagnostics []ConfigOverlayDiagnostic
	var visit func(map[string]toml.Primitive, string)
	visit = func(values map[string]toml.Primitive, prefix string) {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if len(diagnostics) >= 16 {
				return
			}
			primitive := values[key]
			path := prefix + key
			if path == "history" {
				var nested map[string]toml.Primitive
				if metadata.PrimitiveDecode(primitive, &nested) == nil {
					visit(nested, "history.")
					continue
				}
			}
			code, message := OverlayKeyForbidden, "Overlay key is not allowed"
			safeKey := ""
			for _, field := range fields {
				if path != field.Key {
					continue
				}
				safeKey = path
				code, message = OverlayValueInvalid, "Overlay value is not allowed"
				var value any
				if metadata.PrimitiveDecode(primitive, &value) != nil {
					break
				}
				if field.ValueType == "boolean" {
					if boolean, ok := value.(bool); ok && !boolean {
						code = ""
					}
				} else if text, ok := value.(string); ok {
					if path == "model_reasoning_effort" {
						if text == "" || overlayEffortPattern.MatchString(text) {
							code = ""
							if text != "" && efforts != nil && !slices.Contains(efforts, text) {
								code, message = OverlayEffortUnsupported, "Reasoning effort is not supported by the selected model"
							}
						}
					} else if text == "" || slices.Contains(field.AllowedValues, text) {
						code = ""
					}
				}
				break
			}
			if code == "" {
				continue
			}
			if slices.Contains([]string{"model", "model_provider", "model_providers", "api_key", "approval_policy", "sandbox_mode", "permissions", "mcp_servers", "shell_environment_policy", "cli_auth_credentials_store"}, path) {
				safeKey = path
			}
			diagnostic := ConfigOverlayDiagnostic{Code: code, Key: safeKey, Message: message}
			var probe overlayPositionProbe
			var parseError toml.ParseError
			if errors.As(metadata.PrimitiveDecode(primitive, &probe), &parseError) {
				diagnostic.Line, diagnostic.Column = int32(parseError.Position.Line), int32(parseError.Position.Col)
			}
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	visit(root, "")
	if len(diagnostics) == 0 && ValidateConfigOverlayDraftPayload(raw) != nil {
		return []ConfigOverlayDiagnostic{{Code: OverlayKeyForbidden, Message: "Overlay contains protected content"}}
	}
	return diagnostics
}
