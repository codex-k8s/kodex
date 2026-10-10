package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaximumContinuationNoticeBytes = 64 << 10

var ErrRuntimeSessionContext = errors.New("runtime session context is invalid")

// ValidateRuntimeSessionContext отделяет owner-sealed последний notice от
// обычного USER ввода. Role/JSON marker сами по себе исключение не дают:
// CurrentContinuationNotice проверяет полный semantic envelope и exact текущие
// revision/session/turn/attempt/diff/capabilities. Execution consumer отдельно
// проверяет owner RuntimeRevision и execution binding. История не изменяется.
func ValidateRuntimeSessionContext(input RunnerInput) (bool, error) {
	if len(input.SessionContext) > 128 {
		return false, ErrRuntimeSessionContext
	}
	notice, err := CurrentContinuationNotice(input)
	if err != nil {
		return false, err
	}
	if notice && input.Mode != RunnerModeTurn {
		return false, ErrRuntimeSessionContext
	}
	for index, message := range input.SessionContext {
		if message.Role == "USER" {
			if notice && index == len(input.SessionContext)-1 {
				if len(message.Content) > MaximumContinuationNoticeBytes || !utf8.ValidString(message.Content) || strings.ContainsRune(message.Content, 0) {
					return false, ErrRuntimeSessionContext
				}
			} else if !ValidAssistantTurnContent(message.Content) {
				return false, ErrRuntimeSessionContext
			}
		} else if !containsString([]string{"ASSISTANT", "SYSTEM"}, message.Role) || len(message.Content) > 64<<10 || !utf8.ValidString(message.Content) {
			return false, ErrRuntimeSessionContext
		}
	}
	return notice, nil
}

type continuationDescriptor struct {
	Ref     string `json:"ref,omitempty"`
	Version int64  `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Value   string `json:"value,omitempty"`
}

type continuationChange struct {
	Component string                   `json:"component"`
	Previous  []continuationDescriptor `json:"previous"`
	Current   []continuationDescriptor `json:"current"`
	Action    string                   `json:"action"`
}

type continuationDiff struct {
	PreviousRevisionRef string               `json:"previousRevisionRef"`
	CurrentRevisionRef  string               `json:"currentRevisionRef"`
	SessionRef          string               `json:"sessionRef"`
	TurnRef             string               `json:"turnRef"`
	Attempt             int32                `json:"attempt"`
	Changes             []continuationChange `json:"changes"`
	Digest              string               `json:"digest"`
}

// CurrentContinuationNotice проверяет последнее сообщение owner-sealed input.
// Результат используется только для доставки контекста и не выдаёт полномочия;
// caller обязан проверить полный RuntimeRevision и execution binding.
func CurrentContinuationNotice(input RunnerInput) (bool, error) {
	if len(input.SessionContext) == 0 {
		return false, nil
	}
	if len(input.SessionContext) > 128 {
		return false, errPromptService
	}
	message := input.SessionContext[len(input.SessionContext)-1]
	var envelope PromptServiceEnvelope
	if message.Role != "USER" || json.Unmarshal([]byte(message.Content), &envelope) != nil {
		return false, nil
	}
	var diffContent string
	found := false
	for _, section := range envelope.Sections {
		if section.Source == "PLATFORM" && section.Slot == "RUNTIME_CHANGES" {
			if found {
				return false, errPromptService
			}
			found, diffContent = true, section.Content
		}
	}
	if !found {
		return false, nil
	}
	// Notice и базовые инструкции принадлежат одной текущей materialization.
	// Историческая v2 запись не продолжает v3 attempt и не допускает downgrade.
	if !supportedPromptServiceRevision(envelope.Revision) || envelope.Revision != input.PromptServiceTemplateRevision {
		return false, errPromptService
	}
	if len(message.Content) > MaximumContinuationNoticeBytes {
		return false, errPromptService
	}
	// Notice имеет собственный semantic kind, в отличие от базового Agent,
	// Workflow или Automation prompt. Пустой provider ID исключает рекурсию.
	consumer := input
	consumer.CodexSessionID = ""
	consumer.Instructions = message.Content
	consumer.PromptServiceTemplateRevision = envelope.Revision
	consumer.PromptTargetKind = "SESSION_CONTINUATION"
	raw, err := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{
		envelope.Revision, envelope.Locale, consumer.PromptTargetKind,
		[]string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS", "RUNTIME_CHANGES"},
	})
	if err != nil {
		return false, errPromptService
	}
	digest := sha256.Sum256(raw)
	consumer.PromptServiceTemplateDigest = hex.EncodeToString(digest[:])
	if _, err := DecodePromptService(consumer); err != nil {
		return false, err
	}
	if err := validateContinuationDiff(diffContent, input); err != nil {
		return false, err
	}
	return true, nil
}

func validateContinuationDiff(content string, input RunnerInput) error {
	if promptJSONUnique(json.NewDecoder(strings.NewReader(content)), 0) != nil {
		return errPromptService
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var diff continuationDiff
	if decoder.Decode(&diff) != nil || decoder.Decode(new(any)) != io.EOF ||
		diff.PreviousRevisionRef == "" || len(diff.PreviousRevisionRef) > 128 || diff.PreviousRevisionRef == diff.CurrentRevisionRef ||
		diff.CurrentRevisionRef == "" || diff.CurrentRevisionRef != input.RuntimeRevisionRef ||
		diff.SessionRef == "" || diff.SessionRef != input.SessionRef || diff.TurnRef == "" || diff.TurnRef != input.TurnRef ||
		diff.Attempt < 1 || diff.Attempt != input.Attempt || !sha256Pattern.MatchString(diff.Digest) {
		return errPromptService
	}
	components := []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"}
	last := -1
	for _, change := range diff.Changes {
		position := slices.Index(components, change.Component)
		if position <= last || change.Action != "USE_CURRENT_CONTEXT" || slices.Equal(change.Previous, change.Current) || len(change.Previous) > 256 || len(change.Current) > 256 {
			return errPromptService
		}
		last = position
		for _, descriptors := range [][]continuationDescriptor{change.Previous, change.Current} {
			for _, descriptor := range descriptors {
				if descriptor.Version < 0 || descriptor.Version > 9007199254740991 || len(descriptor.Ref) > 128 || len(descriptor.Value) > 256 ||
					!utf8.ValidString(descriptor.Ref+descriptor.Value) || strings.ContainsFunc(descriptor.Ref+descriptor.Value, unicode.IsControl) ||
					descriptor.Digest != "" && !sha256Pattern.MatchString(descriptor.Digest) {
					return errPromptService
				}
			}
		}
	}
	expected := diff.Digest
	diff.Digest = ""
	raw, err := json.Marshal(diff)
	if err != nil {
		return errPromptService
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expected {
		return errPromptService
	}
	return nil
}
