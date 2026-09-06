package query

// PromptPreviewContext не несёт полномочий: ссылки и версии проверяет owner.
type PromptPreviewContext struct {
	ScopeOnly                                       bool
	AgentRef, WorkflowRevisionRef, WorkflowStageKey string
	ExpectedAgentVersion, ExpectedWorkflowVersion   int64
	Input                                           map[string]any
	AttachmentSetRef                                string
	Task                                            string
}
