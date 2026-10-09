package entity

type ExecutionWorkflowCatalogEntry struct {
	WorkflowRef, Name, Purpose, PublishedRef, SpecDigest string
	WorkflowVersion                                      int64
	Inputs                                               []WorkflowInputField
	Readiness                                            WorkflowLaunchReadiness
}

type ExecutionWorkflowCatalog struct {
	Items         []ExecutionWorkflowCatalogEntry
	NextPageToken string
}
