package callback

import "errors"

const (
	assistantCatalogSelectionFailureMessage = "configuration catalog selection is invalid"
	assistantCatalogSelectionFailureClass   = "assistant_catalog_selection_invalid"
	assistantCatalogInputInvalidCode        = "CATALOG_INPUT_INVALID"
	assistantCatalogInputInvalidGuidance    = "Retry at most once by calling get_configuration_catalog with {} or operation_types: [] to read the allowed index for the current screen. Select only operation types returned by that index. If the requested operation is unavailable here, ask the owner to open its matching native resource route and submit a new turn. Do not change context, guess operations or request broader authority."
	assistantCatalogInputFailureMessage     = "configuration catalog input is invalid"
	assistantCatalogShapeInvalid            = "assistant_catalog_shape_invalid"
	assistantCatalogSelectorInvalid         = "assistant_catalog_selector_invalid"
	assistantCatalogPageInvalid             = "assistant_catalog_page_invalid"
	assistantCatalogReadInputGuidance       = "Read the current get_configuration_catalog input schema and correct the rejected input at most once. Use only exact schema fields and one selector: assistant_configuration_catalog cannot be mixed with agent_query, agent_offset, definition_query, definition_offset or nonempty operation_types. For WORKFLOW_CONFIGURATION or AGENT_CONFIGURATION, use configuration_offset_bytes, maximum_bytes (4..16384) and configuration_sha256; continuation requires the exact digest from the first page. Keep the current server-owned context and exact resource refs. Do not guess fields, switch context, request broader authority or retry an unchanged input."
)

var errAssistantCatalogSelection = errors.New(assistantCatalogSelectionFailureMessage)

// Маркер создаётся только локальным parser до owner RPC, без payload и remote cause.
type assistantCatalogInputError struct{ class string }

func (*assistantCatalogInputError) Error() string { return assistantCatalogInputFailureMessage }

func invalidAssistantCatalogInput(class string) error {
	return &assistantCatalogInputError{class: class}
}

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
