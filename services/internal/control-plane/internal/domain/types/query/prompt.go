package query

// PromptPreviewContext supplies optional scope, version and input hints for a
// preview; the owner resolves references and checks expected versions.
// PromptPreviewContext передаёт необязательные подсказки области, версии и
// входа для предпросмотра; владелец разрешает ссылки и проверяет версии.
type PromptPreviewContext struct {
	ScopeOnly                                       bool
	AgentRef, WorkflowRevisionRef, WorkflowStageKey string
	ExpectedAgentVersion, ExpectedWorkflowVersion   int64
	Input                                           map[string]any
	AttachmentSetRef                                string
	Task                                            string
}
