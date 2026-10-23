package query

// AssistantTaskSessionRead locates one run and optional page cursor; session
// and actor authority must be resolved by the owner from the lease.
// AssistantTaskSessionRead указывает запуск и необязательный курсор страницы;
// сессию и полномочия actor владелец определяет по аренде.
type AssistantTaskSessionRead struct{ RunRef, Cursor string }

// AssistantTaskPublishedMessage represents one published task-session message
// with its source run, attempt and event ordering for owner-scoped history.
// AssistantTaskPublishedMessage описывает опубликованное сообщение с исходным
// запуском, попыткой и порядком события в истории, ограниченной владельцем.
type AssistantTaskPublishedMessage struct {
	EventRef, MessageRef, Phase, Text, Origin, SourceRunRef, SessionRef, NodeRef, TurnRef string
	SourceRunVersion, TurnNumber, EventSequence, MessageRevision                          int64
	Attempt                                                                               int32
}

// AssistantTaskSessionPage carries published messages of the selected session
// and a bounded continuation cursor, not an unfiltered execution log.
// AssistantTaskSessionPage содержит опубликованные сообщения выбранной сессии
// и курсор продолжения, а не полный журнал выполнения без отбора.
type AssistantTaskSessionPage struct {
	RunRef, ProjectRef, SessionRef, Title, State, ResultSummary, SafeErrorCode, SafeErrorMessage, SessionStorageState string
	RunVersion                                                                                                        int64
	Messages                                                                                                          []AssistantTaskPublishedMessage
	SourceSHA256, NextCursor                                                                                          string
	Truncated                                                                                                         bool
}
