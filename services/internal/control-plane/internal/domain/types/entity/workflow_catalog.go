package entity

import "time"

type ExecutionWorkflowCatalogEntry struct {
	WorkflowRef, Name, Purpose, PublishedRef, SpecDigest string
	WorkflowVersion                                      int64
	Inputs                                               []WorkflowInputField
	Readiness                                            WorkflowLaunchReadiness
}

type ExecutionWorkflowCatalog struct {
	Items         []ExecutionWorkflowCatalogEntry
	NextPageToken string
	Publication   *ExecutionWorkflowPublication
	ActiveRuns    *ExecutionWorkflowActiveRuns
}

type ExecutionWorkflowPublication struct {
	ConfigurationJSON   []byte
	ConfigurationSHA256 string
}
type ExecutionWorkflowActiveRun struct {
	RunRef, WorkflowRef, PublishedRef, SpecDigest, Title, State string
	PublishedVersion                                            int32
	RunVersion                                                  int64
	CreatedAt                                                   time.Time
}
type ExecutionWorkflowActiveRuns struct {
	WorkflowRef, PublishedRef, SpecDigest string
	WorkflowVersion                       int64
	Items                                 []ExecutionWorkflowActiveRun
	NextPageToken                         string
}
