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

const PromptServiceRevision = "prompt-service-v2"

// PromptRuntimeContractVersion допускает ровно числовую owner version 1
// до и после JSON persistence, без округления дроби или разбора строки.
func PromptRuntimeContractVersion(value any) bool {
	raw, err := json.Marshal(value)
	return err == nil && string(raw) == "1"
}

var errPromptService = errors.New("runtime prompt service materialization is invalid")

type PromptServiceSection struct {
	UserKind       string `json:"userKind,omitempty"`
	TemplateRef    string `json:"templateRef,omitempty"`
	TemplateDigest string `json:"templateDigest,omitempty"`
	Source         string `json:"source"`
	Slot           string `json:"slot,omitempty"`
	Content        string `json:"content"`
}

type PromptServiceEnvelope struct {
	Revision string                 `json:"revision"`
	Locale   string                 `json:"locale"`
	Sections []PromptServiceSection `json:"sections"`
}

// DecodePromptService принимает только provenance из связанного owner snapshot,
// а не marker внутри пользовательского текста. Пустая revision означает legacy.
func DecodePromptService(input RunnerInput) (PromptServiceEnvelope, error) {
	var value PromptServiceEnvelope
	if input.PromptServiceTemplateRevision != PromptServiceRevision ||
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
	if input.CodexSessionID != "" && input.PromptTargetKind != "SESSION_CONTINUATION" {
		return value, errPromptService
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

// encoding/json принимает последнее значение duplicate key; на authority boundary
// до typed decode запрещаем неоднозначную JSON форму целиком.
func promptJSONUnique(decoder *json.Decoder, depth int) error {
	if depth > 8 {
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
			if promptJSONUnique(decoder, depth+1) != nil {
				return errPromptService
			}
		}
	case '[':
		for decoder.More() {
			if promptJSONUnique(decoder, depth+1) != nil {
				return errPromptService
			}
		}
	default:
		return errPromptService
	}
	_, err = decoder.Token()
	return err
}
