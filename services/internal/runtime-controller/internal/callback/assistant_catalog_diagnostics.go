package callback

import "errors"

const (
	// assistantCatalogSelectionFailureMessage is fixed text for invalid operation-schema
	// selection without echoing the requested kind or current private context.
	// assistantCatalogSelectionFailureMessage — fixed text неверного operation-schema
	// selection без requested kind и current private context.
	assistantCatalogSelectionFailureMessage = "configuration catalog selection is invalid"
	// assistantCatalogSelectionFailureClass is the registered selection-only diagnostic
	// label used to distinguish index rediscovery from resource read-input correction.
	// assistantCatalogSelectionFailureClass — registered selection-only diagnostic label
	// для различения index rediscovery и resource read-input correction.
	assistantCatalogSelectionFailureClass = "assistant_catalog_selection_invalid"
	// assistantCatalogInputInvalidCode identifies catalog local-input recovery and does
	// not permit retrying a denied owner request or broadening native context.
	// assistantCatalogInputInvalidCode обозначает catalog local-input recovery,не разрешая
	// повтор denied owner request и расширение native context.
	assistantCatalogInputInvalidCode = "CATALOG_INPUT_INVALID"
	// assistantCatalogInputInvalidGuidance directs one current-screen index rediscovery
	// and asks the owner for a new native route when the operation is unavailable; it
	// does not authorize context switching by the model.
	// assistantCatalogInputInvalidGuidance задаёт один current-screen index rediscovery
	// и обращение к owner за новым native route при недоступной operation,
	// не разрешая модели самой менять context.
	assistantCatalogInputInvalidGuidance = "Retry at most once by calling get_configuration_catalog with {} or operation_types: [] to read the allowed index for the current screen. Select only operation types returned by that index. If the requested operation is unavailable here, ask the owner to open its matching native resource route and submit a new turn. Do not change context, guess operations or request broader authority."
	// assistantCatalogInputFailureMessage supplies fixed safe text for typed local
	// catalog shape/selector/page input failures without their rejected payload.
	// assistantCatalogInputFailureMessage задаёт fixed safe text typed local catalog
	// shape/selector/page input failures без rejected payload.
	assistantCatalogInputFailureMessage = "configuration catalog input is invalid"
	// assistantCatalogShapeInvalid classifies unknown/mixed top-level local catalog
	// shape rather than a failed owner eligibility decision.
	// assistantCatalogShapeInvalid классифицирует unknown/mixed top-level local catalog
	// shape,не failed owner eligibility decision.
	assistantCatalogShapeInvalid = "assistant_catalog_shape_invalid"
	// assistantCatalogSelectorInvalid classifies malformed local resource/query/offset
	// selection separately from owner read denial or stale current state.
	// assistantCatalogSelectorInvalid классифицирует malformed local resource/query/offset
	// selection отдельно от owner read denial и stale current state.
	assistantCatalogSelectorInvalid = "assistant_catalog_selector_invalid"
	// assistantCatalogPageInvalid classifies locally malformed configuration page
	// coordinates or digest selector, not corrupted owner source bytes.
	// assistantCatalogPageInvalid классифицирует locally malformed configuration page
	// coordinates/digest selector,не corrupted owner source bytes.
	assistantCatalogPageInvalid = "assistant_catalog_page_invalid"
	// assistantCatalogReadInputGuidance explains one schema-bound correction of exclusive
	// read selectors and digest-pinned byte continuation, retaining exact server context
	// rather than guessing fields, changing scope or retrying unchanged input.
	// assistantCatalogReadInputGuidance объясняет одно schema-bound correction exclusive read
	// selectors/digest-pinned byte continuation с exact server context вместо guessed
	// fields,scope change и unchanged-input retry.
	assistantCatalogReadInputGuidance = "Read the current get_configuration_catalog input schema and correct the rejected input at most once. Use only exact schema fields and one selector: assistant_configuration_catalog cannot be mixed with agent_query, agent_offset, definition_query, definition_offset or nonempty operation_types. For WORKFLOW_CONFIGURATION, WORKFLOW_METADATA, AGENT_CONFIGURATION or AGENT_RUNTIME_CONFIGURATION, when present in the current schema, use configuration_offset_bytes, maximum_bytes (4..16384) and configuration_sha256; continuation requires the exact digest from the first page. Keep the current server-owned context and exact resource refs. Do not guess fields, switch context, request broader authority or retry an unchanged input."
)

// errAssistantCatalogSelection provides typed sentinel provenance for local selection
// failure; matching raw error text alone does not equal this sentinel.
// errAssistantCatalogSelection даёт typed sentinel provenance local selection failure;
// совпадение raw error text не означает этот sentinel.
var errAssistantCatalogSelection = errors.New(assistantCatalogSelectionFailureMessage)

// assistantCatalogInputError carries only a local closed failure class, created
// before owner RPC and without payload or a retained remote cause.
// assistantCatalogInputError хранит лишь local closed failure class,создаваемый
// до owner RPC без payload и retained remote cause.
type assistantCatalogInputError struct{ class string }

// Error returns the fixed safe catalog-input message independently of its class.
// Error возвращает fixed safe catalog-input message независимо от class.
func (*assistantCatalogInputError) Error() string { return assistantCatalogInputFailureMessage }

// invalidAssistantCatalogInput creates a local typed marker; the consumer classifier
// still accepts only registered classes rather than trusting arbitrary class text.
// invalidAssistantCatalogInput создаёт local typed marker; consumer classifier
// принимает лишь registered classes,не доверяя arbitrary class text.
func invalidAssistantCatalogInput(class string) error {
	return &assistantCatalogInputError{class: class}
}

// assistantCatalogFailureClass prioritizes the selection sentinel through wrapping,
// otherwise accepts three registered nonnil typed input classes; unknown types/classes
// produce no catalog recovery label.
// assistantCatalogFailureClass сначала проверяет selection sentinel через wrapping,
// иначе допускает три registered nonnil typed input classes; unknown types/classes
// не получают catalog recovery label.
func assistantCatalogFailureClass(err error) string {
	if errors.Is(err, errAssistantCatalogSelection) {
		return assistantCatalogSelectionFailureClass
	}
	var failure *assistantCatalogInputError
	if errors.As(err, &failure) && failure != nil {
		switch failure.class {
		case assistantCatalogShapeInvalid, assistantCatalogSelectorInvalid, assistantCatalogPageInvalid:
			return failure.class
		}
	}
	return ""
}

// assistantCatalogRecoveryGuidance maps only a recognized local class to fixed
// index-discovery or read-input guidance; unknown/remote error text returns no guidance.
// assistantCatalogRecoveryGuidance отображает лишь recognized local class в fixed
// index-discovery/read-input guidance; unknown/remote error text не получает guidance.
func assistantCatalogRecoveryGuidance(err error) string {
	switch assistantCatalogFailureClass(err) {
	case assistantCatalogSelectionFailureClass:
		return assistantCatalogInputInvalidGuidance
	case assistantCatalogShapeInvalid, assistantCatalogSelectorInvalid, assistantCatalogPageInvalid:
		return assistantCatalogReadInputGuidance
	default:
		return ""
	}
}
