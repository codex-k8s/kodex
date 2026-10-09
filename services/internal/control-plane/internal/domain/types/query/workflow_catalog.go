package query

// ExecutionWorkflowCatalog не принимает owner или project от вызывающего.
type ExecutionWorkflowCatalog struct {
	LeaseRef, Fence, Query, PageToken string
	Generation                        int64
}
