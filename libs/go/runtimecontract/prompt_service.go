package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

// PromptServiceRevision identifies the currently emitted owner-sealed prompt format.
// PromptServiceRevision обозначает текущий формат prompt, заверенного владельцем.
const PromptServiceRevision = "prompt-service-v3"

// pinnedPromptServiceRevisionV2 preserves decoding of already sealed v2 snapshots.
// pinnedPromptServiceRevisionV2 сохраняет чтение уже заверенных снимков версии v2.
const pinnedPromptServiceRevisionV2 = "prompt-service-v2"

// supportedPromptServiceRevision accepts only the current format and previously sealed v2;
// unknown revisions fail closed rather than selecting a permissive decoder.
// supportedPromptServiceRevision допускает лишь текущий формат и ранее заверенную v2;
// неизвестные версии закрыто отклоняются без выбора более мягкого декодера.
func supportedPromptServiceRevision(revision string) bool {
	return revision == PromptServiceRevision || revision == pinnedPromptServiceRevisionV2
}

// PromptRuntimeContractVersion accepts exactly numeric owner version 1 before or after JSON persistence,
// without rounding fractions or parsing string values.
// PromptRuntimeContractVersion принимает ровно числовую owner version 1 до и после сохранения JSON,
// без округления дроби или преобразования строки.
func PromptRuntimeContractVersion(value any) bool {
	raw, err := json.Marshal(value)
	return err == nil && string(raw) == "1"
}

// errPromptService is the closed-failure result for malformed prompt materialization.
// errPromptService обозначает закрытый отказ при неверной материализации prompt.
var errPromptService = errors.New("runtime prompt service materialization is invalid")

// PromptServiceSection records a platform slot or a user template with explicit provenance.
// PromptServiceSection хранит платформенный слот или пользовательский шаблон с явным происхождением.
type PromptServiceSection struct {
	UserKind       string `json:"userKind,omitempty"`
	TemplateRef    string `json:"templateRef,omitempty"`
	TemplateDigest string `json:"templateDigest,omitempty"`
	Source         string `json:"source"`
	Slot           string `json:"slot,omitempty"`
	Content        string `json:"content"`
}

// PromptServiceEnvelope groups versioned, localized sections validated against the runner snapshot.
// PromptServiceEnvelope объединяет версионированные разделы с локалью, сверяемые со снимком runner.
type PromptServiceEnvelope struct {
	Revision string                 `json:"revision"`
	Locale   string                 `json:"locale"`
	Sections []PromptServiceSection `json:"sections"`
}

// DecodePromptService validates the sealed template digest, exact slots, JSON shape, and provenance;
// a marker in user text cannot grant platform authority, and unknown revisions fail closed.
// DecodePromptService проверяет заверенный дайджест шаблона, точные слоты, форму JSON и происхождение;
// маркер в тексте пользователя не даёт платформенных полномочий, неизвестные версии отклоняются.
func DecodePromptService(input RunnerInput) (PromptServiceEnvelope, error) {
	var value PromptServiceEnvelope
	if !supportedPromptServiceRevision(input.PromptServiceTemplateRevision) ||
		!sha256Pattern.MatchString(input.PromptServiceTemplateDigest) ||
		len(input.Instructions) > 256<<10 || !utf8.ValidString(input.Instructions) {
		return value, errPromptService
	}
	required := []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	switch input.PromptTargetKind {
	case "AGENT", "AUTOMATION":
	case "WORKFLOW_STAGE":
		required = []string{"WORKFLOW", "STAGE", "PURPOSE", "EXPECTED_RESULT", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	case "SESSION_CONTINUATION":
		required = append(required, "RUNTIME_CHANGES")
	default:
		return value, errPromptService
	}
	if input.CodexSessionID != "" {
		// Вид базовых инструкций не меняется при resume. Сообщение продолжения
		// находится отдельно в owner-sealed SessionContext и обязательно для него.
		if notice, err := CurrentContinuationNotice(input); err != nil || !notice {
			return value, errPromptService
		}
	}
	if promptJSONUnique(json.NewDecoder(strings.NewReader(input.Instructions)), 0) != nil {
		return value, errPromptService
	}
	decoder := json.NewDecoder(strings.NewReader(input.Instructions))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF ||
		value.Revision != input.PromptServiceTemplateRevision || (value.Locale != "en" && value.Locale != "ru") {
		return value, errPromptService
	}
	raw, _ := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{value.Revision, value.Locale, input.PromptTargetKind, required})
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != input.PromptServiceTemplateDigest {
		return value, errPromptService
	}
	seen := map[string]bool{}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(input.Instructions), &object) != nil || len(object) != 3 || object["revision"] == nil || object["locale"] == nil || object["sections"] == nil {
		return value, errPromptService
	}
	var shape struct {
		Sections []map[string]json.RawMessage `json:"sections"`
	}
	if json.Unmarshal([]byte(input.Instructions), &shape) != nil {
		return value, errPromptService
	}
	for _, section := range shape.Sections {
		if _, ok := section["content"]; !ok {
			return value, errPromptService
		}
		for key, field := range section {
			if !slices.Contains([]string{"userKind", "templateRef", "templateDigest", "source", "slot", "content"}, key) {
				return value, errPromptService
			}
			if len(field) == 0 || field[0] != '"' {
				return value, errPromptService
			}
		}
	}
	expectedCapabilities := append([]string(nil), input.Capabilities...)
	slices.Sort(expectedCapabilities)
	for _, section := range value.Sections {
		switch section.Source {
		case "PLATFORM":
			if !slices.Contains(required, section.Slot) || seen[section.Slot] || section.UserKind != "" || section.TemplateRef != "" || section.TemplateDigest != "" {
				return value, errPromptService
			}
			seen[section.Slot] = true
			if section.Slot == "EFFECTIVE_CAPABILITIES" && section.Content != strings.Join(expectedCapabilities, "\n") {
				return value, errPromptService
			}
		case "USER_TEMPLATE":
			if section.Slot != "" {
				return value, errPromptService
			}
			if section.UserKind == "" {
				if section.TemplateRef != "" || section.TemplateDigest != "" {
					return value, errPromptService
				}
			} else if (section.UserKind != "WORKFLOW_CONTEXT" && section.UserKind != "AUTOMATION_TASK") || section.TemplateRef == "" || !sha256Pattern.MatchString(section.TemplateDigest) || (section.UserKind == "WORKFLOW_CONTEXT" && input.PromptTargetKind != "WORKFLOW_STAGE") {
				return value, errPromptService
			}
		default:
			return value, errPromptService
		}
	}
	if len(seen) != len(required) {
		return value, errPromptService
	}
	return value, nil
}

// promptJSONUnique rejects duplicate JSON keys before typed decoding, where encoding/json would
// otherwise keep the last value and make the authority-bearing prompt ambiguous.
// promptJSONUnique отклоняет повторяющиеся ключи JSON до типизированного разбора, при котором
// encoding/json оставил бы последнее значение и сделал полномочный prompt неоднозначным.
func promptJSONUnique(decoder *json.Decoder, depth int) error {
	return boundedJSONUnique(decoder, depth, 8)
}

// boundedJSONUnique walks JSON containers with a caller-specific depth limit and rejects
// repeated object keys; scanner metadata does not inherit the prompt-section depth budget.
// boundedJSONUnique обходит контейнеры JSON с отдельным лимитом глубины и отклоняет
// повторные ключи объекта; метаданные сканера не наследуют лимит разделов prompt.
func boundedJSONUnique(decoder *json.Decoder, depth, maximumDepth int) error {
	if depth > maximumDepth {
		return errPromptService
	}
	token, err := decoder.Token()
	if err != nil {
		return errPromptService
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return errPromptService
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errPromptService
			}
			seen[name] = true
			if boundedJSONUnique(decoder, depth+1, maximumDepth) != nil {
				return errPromptService
			}
		}
	case '[':
		for decoder.More() {
			if boundedJSONUnique(decoder, depth+1, maximumDepth) != nil {
				return errPromptService
			}
		}
	default:
		return errPromptService
	}
	_, err = decoder.Token()
	return err
}
