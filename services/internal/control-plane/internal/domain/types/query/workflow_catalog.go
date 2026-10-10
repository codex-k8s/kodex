package query

// ExecutionWorkflowCatalog не принимает owner или project от вызывающего.
type ExecutionWorkflowCatalog struct {
	LeaseRef, Fence, Query, PageToken string
	Generation                        int64
	Publication                       *ExecutionWorkflowPublicationRead
	ActiveRuns                        *ExecutionWorkflowActiveRunsRead
}

type ExecutionWorkflowReadPins struct {
	WorkflowRef, PublishedRef, SpecDigest string
	WorkflowVersion                       int64
}
type ExecutionWorkflowPublicationRead struct{ Pins ExecutionWorkflowReadPins }
type ExecutionWorkflowActiveRunsRead struct{ Pins ExecutionWorkflowReadPins }
