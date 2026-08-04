package generated

type AnonymousSchema_121 struct {
	ParentProcessRunId   string `json:"parentProcessRunId,omitempty"`
	PlaybookRef          string `json:"playbookRef" binding:"required"`
	PolicyRevision       int    `json:"policyRevision" binding:"required"`
	RootTriggerRef       string `json:"rootTriggerRef" binding:"required"`
	RootSessionId        string `json:"rootSessionId" binding:"required"`
	RootTurnId           string `json:"rootTurnId" binding:"required"`
	RootAttempt          int    `json:"rootAttempt" binding:"required"`
	ImmutableInputSha256 string `json:"immutableInputSha256" binding:"required"`
	RuntimeRevisionId    string `json:"runtimeRevisionId" binding:"required"`
	CurrentSessionId     string `json:"currentSessionId,omitempty"`
	CurrentTurnId        string `json:"currentTurnId,omitempty"`
	CurrentAttempt       int    `json:"currentAttempt,omitempty"`
}
