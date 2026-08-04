package generated

type AnonymousSchema_106 struct {
	AgentId                  string `json:"agentId" binding:"required"`
	ProviderAccountBindingId string `json:"providerAccountBindingId" binding:"required"`
	ConversationId           string `json:"conversationId,omitempty"`
	ArchiveRef               string `json:"archiveRef,omitempty"`
	LastTurnSequence         int    `json:"lastTurnSequence" binding:"required"`
}
