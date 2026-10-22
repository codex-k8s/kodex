package command

// LaunchWorkflowInput requests child workflow execution under exact lease, fence, generation, and
// publication pins. The lease owner derives actor, project, and root lineage; the payload
// intentionally does not supply those authorities.
// LaunchWorkflowInput запрашивает дочернее исполнение процесса с точными привязками аренды, барьера,
// поколения и публикации. Владелец аренды определяет субъекта, проект и корневое происхождение; эти
// полномочия намеренно не передаются во входных данных.
type LaunchWorkflowInput struct {
	LeaseRef, Fence, WorkflowRef, Task, Title string
	Generation                                int64
	Input                                     map[string]any
	ExpectedPublishedRef, ExpectedSpecDigest  string
	ExpectedWorkflowVersion                   int64
}
