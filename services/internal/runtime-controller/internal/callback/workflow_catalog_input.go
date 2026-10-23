package callback

const (
	// workflowCatalogInputInvalidCode marks only correctable local catalog-shape
	// failures, distinct from owner permission, stale pin, audit or integrity failures.
	// workflowCatalogInputInvalidCode обозначает лишь исправимые local catalog-shape
	// отказы отдельно от owner permission,stale pin,audit и integrity failures.
	workflowCatalogInputInvalidCode = "WORKFLOW_CATALOG_INPUT_INVALID"
	// workflowCatalogInputInvalidMessage supplies the fixed public diagnostic without
	// reflecting raw selectors, publication content or owner errors.
	// workflowCatalogInputInvalidMessage задаёт fixed public diagnostic без отражения
	// raw selectors,publication content и owner errors.
	workflowCatalogInputInvalidMessage = "workflow catalog input is invalid"
	// workflowCatalogInputInvalidGuidance explains one local-input correction using
	// exact discovery pins and byte continuation commitments, prohibiting unchanged
	// retries, guessed refs, broader authority or launch side effects.
	// workflowCatalogInputInvalidGuidance объясняет одно local-input correction с exact
	// discovery pins/byte continuation commitments, запрещая unchanged retries,guessed
	// refs,broader authority и launch side effects.
	workflowCatalogInputInvalidGuidance = "Read tools/list for the current get_workflow_catalog input schema. Correct only the local input and retry at most once; never repeat unchanged arguments. Copy workflow_ref, published_ref, spec_digest and workflow_version exactly from discovery; workflow_version is the record version from discovery, not published_version. Nest these pins inside publication_read, without query, page_token or another selector. Start with offset_bytes=0; continuation uses next_offset_bytes and the unchanged configuration_sha256 from the first page, with unchanged pins. Use only schema-declared fields and bounds. Do not retry owner, stale-pin, lease, permission, integrity, audit or projection failures. Do not guess pins, change context, request broader authority or launch a Workflow."
)

// workflowCatalogInputError marks local shape failure before owner RPC; remote
// messages or typed errors returned by the owner are not sufficient provenance.
// workflowCatalogInputError отмечает local shape failure до owner RPC; remote
// messages и typed errors от owner сами не доказывают происхождение.
type workflowCatalogInputError struct{}

// Error returns the fixed catalog-input message without exposing rejected values.
// Error возвращает fixed catalog-input message без отклонённых значений.
func (*workflowCatalogInputError) Error() string { return workflowCatalogInputInvalidMessage }

// validateWorkflowCatalogInput delegates closed mode/coordinate parsing to the same
// metadata predicate used by receipts and returns only a local marker on failure.
// It performs no owner read and does not validate publication bytes.
// validateWorkflowCatalogInput использует тот же closed mode/coordinate predicate,
// что receipts, возвращая local marker при ошибке. Owner read и проверка
// publication bytes не выполняются.
func validateWorkflowCatalogInput(arguments map[string]any) error {
	if _, _, _, valid := workflowCatalogRequestMetadata(arguments); !valid {
		return &workflowCatalogInputError{}
	}
	return nil
}

// workflowCatalogInputRecoveryGuidance accepts only a direct nonnil local marker,
// not wrapped/joined errors, because a combined audit failure must stay nonrecoverable.
// workflowCatalogInputRecoveryGuidance принимает лишь прямой nonnil local marker,
// не wrapped/joined errors: совмещённый audit failure остаётся nonrecoverable.
func workflowCatalogInputRecoveryGuidance(err error) string {
	// Wrapping/join не доказывает единственный локальный отказ; audit остаётся закрытым.
	if local, ok := err.(*workflowCatalogInputError); ok && local != nil {
		return workflowCatalogInputInvalidGuidance
	}
	return ""
}
