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

// MaximumContinuationNoticeBytes bounds the owner-sealed continuation message.
// MaximumContinuationNoticeBytes ограничивает размер заверенного владельцем сообщения продолжения.
const MaximumContinuationNoticeBytes = 64 << 10

// ErrRuntimeSessionContext denotes an invalid session history or continuation notice.
// ErrRuntimeSessionContext обозначает неверную историю сессии или сообщение продолжения.
var ErrRuntimeSessionContext = errors.New("runtime session context is invalid")

// ValidateRuntimeSessionContext distinguishes a sealed final continuation notice from ordinary
// USER input only after semantic validation of its revision, session, turn, attempt, diff, and capabilities;
// the execution consumer separately checks owner revision and binding without rewriting history.
// ValidateRuntimeSessionContext отличает заверенное последнее сообщение продолжения от обычного
// ввода USER только после проверки версии, сессии, хода, попытки, diff и возможностей;
// потребитель исполнения отдельно проверяет версию владельца и привязку, не меняя историю.
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

// continuationDescriptor identifies one prior or current component value in a sealed diff.
// continuationDescriptor определяет прежнее или текущее значение компонента в заверенном diff.
type continuationDescriptor RuntimeContextDescriptor

// continuationChange records one ordered component transition to current context.
// continuationChange хранит один упорядоченный переход компонента к текущему контексту.
type continuationChange RuntimeContextChange[continuationDescriptor]

// continuationDiff pins revision, session, turn, attempt, and canonical change digest.
// continuationDiff закрепляет версию, сессию, ход, попытку и канонический дайджест изменений.
type continuationDiff RuntimeContextDiff[continuationChange]

// CurrentContinuationNotice validates only the last owner-sealed context message and reports
// its presence for delivery; callers must independently verify RuntimeRevision and execution binding.
// CurrentContinuationNotice проверяет лишь последнее заверенное владельцем сообщение контекста
// для доставки; вызывающий код отдельно проверяет RuntimeRevision и привязку исполнения.
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

// validateContinuationDiff checks unique ordered changes and a digest bound to the current attempt.
// validateContinuationDiff проверяет уникальные упорядоченные изменения и дайджест текущей попытки.
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
