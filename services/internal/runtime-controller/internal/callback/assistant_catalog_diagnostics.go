package callback

import "errors"

const (
	assistantCatalogSelectionFailureMessage = "configuration catalog selection is invalid"
	assistantCatalogSelectionFailureClass   = "assistant_catalog_selection_invalid"
	assistantCatalogInputInvalidCode        = "CATALOG_INPUT_INVALID"
	assistantCatalogInputInvalidGuidance    = "Retry at most once by calling get_configuration_catalog with {} or operation_types: [] to read the allowed index for the current screen. Select only operation types returned by that index. If the requested operation is unavailable here, ask the owner to open its matching native resource route and submit a new turn. Do not change context, guess operations or request broader authority."
)

var errAssistantCatalogSelection = errors.New(assistantCatalogSelectionFailureMessage)

func assistantCatalogFailureClass(err error) string {
	if errors.Is(err, errAssistantCatalogSelection) {
		return assistantCatalogSelectionFailureClass
	}
	return ""
}
