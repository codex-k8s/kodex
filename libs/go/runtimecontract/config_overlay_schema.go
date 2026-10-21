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
	// ReasoningSupported requires an explicit effective reasoning value supplied by the configuration owner.
	// ReasoningSupported требует явного действующего значения рассуждений от владельца конфигурации.
	ReasoningSupported = "SUPPORTED"
	// ReasoningUnsupported permits only the absence of both effective and overlay reasoning values.
	// ReasoningUnsupported разрешает лишь отсутствие действующего значения и настройки рассуждений.
	ReasoningUnsupported = "UNSUPPORTED"
	// OverlaySyntaxInvalid identifies encoding, size or TOML syntax failures without exposing input values.
	// OverlaySyntaxInvalid обозначает ошибки кодировки, размера или синтаксиса TOML без раскрытия входных значений.
	OverlaySyntaxInvalid = "CONFIG_OVERLAY_SYNTAX_INVALID"
	// OverlayKeyForbidden identifies keys or content outside the editable overlay boundary.
	// OverlayKeyForbidden обозначает ключи или содержимое вне границы редактируемых настроек.
	OverlayKeyForbidden = "CONFIG_OVERLAY_KEY_FORBIDDEN"
	// OverlayValueInvalid identifies a known overlay key with an unacceptable type or value.
	// OverlayValueInvalid обозначает известный ключ с недопустимым типом или значением.
	OverlayValueInvalid = "CONFIG_OVERLAY_VALUE_INVALID"
	// OverlayEffortUnsupported distinguishes structurally valid effort values absent from the selected model's allowed set.
	// OverlayEffortUnsupported выделяет структурно допустимые степени рассуждения, отсутствующие в разрешённом наборе выбранной модели.
	OverlayEffortUnsupported = "CONFIG_OVERLAY_EFFORT_UNSUPPORTED"
)

// overlayEffortPattern bounds the structural shape of effort identifiers without deciding model compatibility.
// overlayEffortPattern ограничивает структурную форму идентификаторов рассуждений без решения о совместимости модели.
var overlayEffortPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ValidWebSearchMode checks the closed hosted-search mode set; selecting a mode neither grants shell egress nor applies sandbox domain rules to hosted search.
// ValidWebSearchMode проверяет закрытый набор режимов серверного поиска; выбор режима не даёт shell-доступа в сеть и не применяет доменные правила sandbox к серверному поиску.
func ValidWebSearchMode(mode string) bool {
	return slices.Contains([]string{"disabled", "cached", "indexed", "live"}, mode)
}

// ValidateEffectiveReasoningEffort validates the overlay and requires a structurally valid owner effort matching any explicit override, or no effort for an unsupported model.
// ValidateEffectiveReasoningEffort проверяет настройки и требует структурно допустимую степень владельца, совпадающую с явным переопределением, либо отсутствие степени у неподдерживаемой модели.
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

// ConfigOverlayDiagnostic reports a safe code, allowlisted key and source position without copying parser messages or raw input values.
// ConfigOverlayDiagnostic сообщает безопасный код, разрешённый ключ и позицию без копирования сообщений анализатора и исходных значений.
type ConfigOverlayDiagnostic struct {
	Code    string `json:"code"`
	Key     string `json:"key"`
	Line    int32  `json:"line"`
	Column  int32  `json:"column"`
	Message string `json:"message"`
}

// ConfigOverlayField describes one editable field's type, allowed values, default and editor hints for the shared schema and validator.
// ConfigOverlayField описывает тип, разрешённые значения, default и подсказки одного редактируемого поля для общей схемы и проверки.
type ConfigOverlayField struct {
	Key           string   `json:"key"`
	ValueType     string   `json:"valueType"`
	AllowedValues []string `json:"allowedValues"`
	DefaultValue  string   `json:"defaultValue"`
	Description   string   `json:"description"`
	Completion    string   `json:"completion"`
	Hover         string   `json:"hover"`
}

// ConfigOverlaySchema publishes the bounded editor contract with a digest-derived revision so model-capability changes invalidate stale schema pins.
// ConfigOverlaySchema публикует ограниченный контракт редактора с ревизией от дайджеста, чтобы изменения возможностей модели делали старые привязки схемы неактуальными.
type ConfigOverlaySchema struct {
	Revision     string               `json:"revision"`
	Digest       string               `json:"digest"`
	Fields       []ConfigOverlayField `json:"fields"`
	MaximumBytes int32                `json:"maximumBytes"`
}

// OverlaySchema builds a digest-pinned field catalog from owner-computed model efforts and default; an empty capability set does not authorize invented effort values.
// OverlaySchema строит закреплённый дайджестом каталог из рассчитанных владельцем степеней и default модели; пустой набор возможностей не разрешает выдумывать значения.
func OverlaySchema(efforts []string, defaultEffort string) ConfigOverlaySchema {
	fields := overlayFields(efforts, defaultEffort)
	schema := ConfigOverlaySchema{Fields: fields, MaximumBytes: MaximumConfigOverlayBytes}
	raw, _ := json.Marshal(schema)
	digest := sha256.Sum256(raw)
	schema.Digest = hex.EncodeToString(digest[:])
	schema.Revision = "cos_" + schema.Digest
	return schema
}

// overlayFields returns the closed editable preference list and clones the model effort slice so editor metadata does not alias the caller's list.
// overlayFields возвращает закрытый список редактируемых настроек и копирует степени модели, чтобы метаданные редактора не разделяли список вызывающего.
func overlayFields(efforts []string, defaultEffort string) []ConfigOverlayField {
	return []ConfigOverlayField{
		{Key: "model_reasoning_effort", ValueType: "string", AllowedValues: slices.Clone(efforts), DefaultValue: defaultEffort,
			Description: "Степень рассуждения выбранной модели", Completion: "model_reasoning_effort = ", Hover: "Допустимые значения определяются exact каталогами выбранных provider accounts."},
		{Key: "web_search", ValueType: "string", AllowedValues: []string{"disabled", "cached", "indexed", "live"}, DefaultValue: "cached",
			Description: "Режим hosted native web search", Completion: "web_search = ", Hover: "Настройка Codex 0.160.0, не доказательство успешного вызова. Sandbox domain allowlist не применяется к hosted search; shell egress не меняется."},
		{Key: "personality", ValueType: "string", AllowedValues: []string{"none", "friendly", "pragmatic"},
			Description: "Стиль ответов", Completion: "personality = ", Hover: "Не изменяет полномочия или ограничения runtime."},
		{Key: "allow_login_shell", ValueType: "boolean", AllowedValues: []string{"false"}, DefaultValue: "false",
			Description: "Запрет login shell", Completion: "allow_login_shell = false", Hover: "Разрешено только false."},
		{Key: "history.persistence", ValueType: "string", AllowedValues: []string{"save-all", "none"}, DefaultValue: "save-all",
			Description: "Сохранение истории", Completion: "history.persistence = ", Hover: "save-all сохраняет историю; none отключает её сохранение."},
	}
}

// overlayPositionProbe deliberately fails primitive decoding to obtain the TOML library's source position without exposing the original value.
// overlayPositionProbe намеренно прерывает разбор примитива для получения позиции из TOML-библиотеки без раскрытия исходного значения.
type overlayPositionProbe struct{}

// UnmarshalTOML returns a fixed failure which the TOML decoder wraps with the primitive's exact position.
// UnmarshalTOML возвращает фиксированный отказ, который TOML-декодер дополняет точной позицией примитива.
func (*overlayPositionProbe) UnmarshalTOML(any) error {
	return errors.New("overlay diagnostic location")
}

// DiagnoseConfigOverlay produces at most sixteen safe field diagnostics with TOML positions, validates optional model efforts and protected content, and never returns parser Message or LastKey values.
// DiagnoseConfigOverlay формирует не более шестнадцати безопасных диагностик с TOML-позициями, проверяет необязательные степени модели и защищённое содержимое, не возвращая значения Message и LastKey анализатора.
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
					} else if (text == "" && path != "web_search") || slices.Contains(field.AllowedValues, text) {
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
