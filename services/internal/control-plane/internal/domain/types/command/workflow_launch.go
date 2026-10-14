package command

// LaunchWorkflowInput не содержит caller actor/project/root; их разрешает lease owner.
type LaunchWorkflowInput struct {
	LeaseRef, Fence, WorkflowRef, Task, Title string
	Generation                                int64
	Input                                     map[string]any
	ExpectedPublishedRef, ExpectedSpecDigest  string
	ExpectedWorkflowVersion                   int64
}
