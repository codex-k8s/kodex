package callback

const (
	workflowCatalogInputInvalidCode     = "WORKFLOW_CATALOG_INPUT_INVALID"
	workflowCatalogInputInvalidMessage  = "workflow catalog input is invalid"
	workflowCatalogInputInvalidGuidance = "Read tools/list for the current get_workflow_catalog input schema. Correct only the local input and retry at most once; never repeat unchanged arguments. Copy workflow_ref, published_ref, spec_digest and workflow_version exactly from discovery; workflow_version is the record version from discovery, not published_version. Nest these pins inside publication_read, without query, page_token or another selector. Start with offset_bytes=0; continuation uses next_offset_bytes and the unchanged configuration_sha256 from the first page, with unchanged pins. Use only schema-declared fields and bounds. Do not retry owner, stale-pin, lease, permission, integrity, audit or projection failures. Do not guess pins, change context, request broader authority or launch a Workflow."
)

// Только локальная форма до owner RPC; ответ и ошибка владельца этот тип не получают.
type workflowCatalogInputError struct{}

func (*workflowCatalogInputError) Error() string { return workflowCatalogInputInvalidMessage }

func validateWorkflowCatalogInput(arguments map[string]any) error {
	if _, _, _, valid := workflowCatalogRequestMetadata(arguments); !valid {
		return &workflowCatalogInputError{}
	}
	return nil
}

func workflowCatalogInputRecoveryGuidance(err error) string {
	// Wrapping/join не доказывает единственный локальный отказ; audit остаётся закрытым.
	if local, ok := err.(*workflowCatalogInputError); ok && local != nil {
		return workflowCatalogInputInvalidGuidance
	}
	return ""
}
