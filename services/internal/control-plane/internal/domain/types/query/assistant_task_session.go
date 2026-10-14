package query

// Locator не несёт Session/actor authority; они разрешаются владельцем по lease.
type AssistantTaskSessionRead struct{ RunRef, Cursor string }

type AssistantTaskPublishedMessage struct {
	EventRef, MessageRef, Phase, Text, Origin, SourceRunRef, SessionRef, NodeRef, TurnRef string
	SourceRunVersion, TurnNumber, EventSequence, MessageRevision                          int64
	Attempt                                                                               int32
}

// История ограничена опубликованными сообщениями выбранной Session.
type AssistantTaskSessionPage struct {
	RunRef, ProjectRef, SessionRef, Title, State, ResultSummary, SafeErrorCode, SafeErrorMessage, SessionStorageState string
	RunVersion                                                                                                        int64
	Messages                                                                                                          []AssistantTaskPublishedMessage
	SourceSHA256, NextCursor                                                                                          string
	Truncated                                                                                                         bool
}
