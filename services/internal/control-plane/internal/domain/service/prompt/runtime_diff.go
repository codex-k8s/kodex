package prompt

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// RuntimeDescriptor names the prompt owner's component descriptor while sharing its JSON shape
// with the continuation consumer. It has no credential or locator fields; callers still validate
// descriptive values and may not use this shape as a redaction mechanism.
// RuntimeDescriptor именует дескриптор компонента владельца промпта с общей JSON-формой потребителя
// продолжения. Полей реквизитов и адресов нет; вызывающий код проверяет описательные значения и не
// использует форму как механизм сокрытия данных.
type RuntimeDescriptor runtimecontract.RuntimeContextDescriptor

// RuntimeChange records one component's ordered previous and current descriptors under the owner's
// distinct nominal type. Component and action eligibility are checked by the diff functions.
// RuntimeChange хранит упорядоченные прежние и текущие дескрипторы компонента в отдельном именованном
// типе владельца. Допустимость компонента и действия проверяют функции различий.
type RuntimeChange runtimecontract.RuntimeContextChange[RuntimeDescriptor]

// RuntimeDiff binds changes to revision and session identity without becoming a continuation grant.
// The prompt owner retains this named type and its existing prospective and execution validation.
// RuntimeDiff связывает изменения с ревизиями и сессией, не становясь разрешением продолжения.
// Владелец промпта сохраняет этот именованный тип и существующие проверки предварительной и исполняемой форм.
type RuntimeDiff runtimecontract.RuntimeContextDiff[RuntimeChange]

// runtimeComponentOrder fixes the closed component order used for comparison and digest production.
// runtimeComponentOrder закрепляет закрытый порядок компонентов для сравнения и формирования дайджеста.
var runtimeComponentOrder = []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"}

// ValidateRuntimeDiff reconstructs the closed component maps, rejects unordered or unchanged entries,
// selects prospective or execution identity validation, and recomputes the stored diff digest. It does
// not authenticate the notice or replace the consumer's exact runtime-revision checks.
// ValidateRuntimeDiff восстанавливает закрытые карты компонентов, отклоняет неупорядоченные и
// неизменившиеся записи, выбирает проверку предварительной или исполняемой личности и пересчитывает
// сохранённый дайджест различий. Функция не удостоверяет уведомление и не заменяет точные проверки ревизии потребителем.
func ValidateRuntimeDiff(diff RuntimeDiff) error {
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component], current[component] = nil, nil
	}
	last := -1
	for _, change := range diff.Changes {
		position := slices.Index(runtimeComponentOrder, change.Component)
		if position <= last || change.Action != "USE_CURRENT_CONTEXT" || slices.Equal(change.Previous, change.Current) {
			return ErrInvalid
		}
		last = position
		previous[change.Component], current[change.Component] = change.Previous, change.Current
	}
	var checked RuntimeDiff
	var err error
	if diff.CurrentRevisionRef == "" {
		checked, err = CompareProspectiveRuntimeContexts(previous, current, diff)
	} else {
		checked, err = CompareRuntimeContexts(previous, current, diff)
	}
	if err != nil || diff.Digest != checked.Digest {
		return ErrInvalid
	}
	return nil
}

// CompareRuntimeContexts requires distinct nonempty revisions and a session, turn, and positive
// attempt before comparing descriptors. The resulting digest describes that exact execution context;
// ownership and authorization of the supplied references are checked outside this pure function.
// CompareRuntimeContexts требует разные непустые ревизии, сессию, ход и положительную попытку перед
// сравнением дескрипторов. Итоговый дайджест описывает точный контекст исполнения; владение и полномочия
// указанных ссылок проверяются вне этой чистой функции.
func CompareRuntimeContexts(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	if identity.PreviousRevisionRef == "" || identity.CurrentRevisionRef == "" || identity.PreviousRevisionRef == identity.CurrentRevisionRef || identity.SessionRef == "" || identity.TurnRef == "" || identity.Attempt < 1 {
		return RuntimeDiff{}, fmt.Errorf("runtime diff identity is invalid: %w", ErrInvalid)
	}
	return compareRuntimeDescriptors(previous, current, identity)
}

// CompareProspectiveRuntimeContexts compares a prospective context with a known prior revision and
// session while requiring the future revision, turn, and attempt to remain unset. It cannot mint an
// execution identity in advance of the owner's continuation transition.
// CompareProspectiveRuntimeContexts сравнивает предварительный контекст с известными прежней ревизией
// и сессией, требуя незаданных будущих ревизии, хода и попытки. Функция не создаёт личность исполнения
// до перехода продолжения, выполняемого владельцем.
func CompareProspectiveRuntimeContexts(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	if identity.PreviousRevisionRef == "" || identity.SessionRef == "" || identity.CurrentRevisionRef != "" || identity.TurnRef != "" || identity.Attempt != 0 {
		return RuntimeDiff{}, fmt.Errorf("prospective runtime diff identity is invalid: %w", ErrInvalid)
	}
	return compareRuntimeDescriptors(previous, current, identity)
}

// compareRuntimeDescriptors validates the complete closed maps and bounded public descriptors, copies
// changed descriptor slices into component order, and hashes the identity with an empty Digest field.
// Caller maps and slices are not changed; unchanged components are absent from the resulting diff.
// compareRuntimeDescriptors проверяет полные закрытые карты и ограниченные публичные дескрипторы,
// копирует изменённые срезы по порядку компонентов и хеширует личность с пустым полем Digest. Карты
// и срезы вызывающего кода не меняются; неизменившиеся компоненты отсутствуют в итоговых различиях.
func compareRuntimeDescriptors(previous, current map[string][]RuntimeDescriptor, identity RuntimeDiff) (RuntimeDiff, error) {
	for _, context := range []map[string][]RuntimeDescriptor{previous, current} {
		if len(context) != len(runtimeComponentOrder) {
			return RuntimeDiff{}, ErrInvalid
		}
		for component, descriptors := range context {
			if !slices.Contains(runtimeComponentOrder, component) || len(descriptors) > 256 {
				return RuntimeDiff{}, ErrInvalid
			}
			for _, descriptor := range descriptors {
				if descriptor.Version < 0 || descriptor.Version > 9007199254740991 || len(descriptor.Ref) > 128 || len(descriptor.Value) > 256 ||
					!utf8.ValidString(descriptor.Ref) || !utf8.ValidString(descriptor.Value) ||
					strings.ContainsFunc(descriptor.Ref+descriptor.Value, unicode.IsControl) ||
					descriptor.Digest != "" && (!validDigest(descriptor.Digest) || strings.ToLower(descriptor.Digest) != descriptor.Digest) {
					return RuntimeDiff{}, fmt.Errorf("runtime diff descriptor for %s is invalid: %w", component, ErrInvalid)
				}
			}
		}
	}
	identity.Changes = []RuntimeChange{}
	identity.Digest = ""
	for _, component := range runtimeComponentOrder {
		if slices.Equal(previous[component], current[component]) {
			continue
		}
		identity.Changes = append(identity.Changes, RuntimeChange{Component: component, Previous: append([]RuntimeDescriptor{}, previous[component]...), Current: append([]RuntimeDescriptor{}, current[component]...), Action: "USE_CURRENT_CONTEXT"})
	}
	identity.Digest = semanticDigest(identity)
	if identity.Digest == "" {
		return RuntimeDiff{}, ErrInvalid
	}
	return identity, nil
}
