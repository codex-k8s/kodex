package query

// ExecutionWorkflowCatalog asks for a workflow catalog page under a lease;
// caller fields cannot assign a different owner or project.
// ExecutionWorkflowCatalog запрашивает страницу каталога процессов по аренде;
// поля вызывающего не могут назначить другого владельца или проект.
type ExecutionWorkflowCatalog struct {
	LeaseRef, Fence, Query, PageToken string
	Generation                        int64
	Publication                       *ExecutionWorkflowPublicationRead
	ActiveRuns                        *ExecutionWorkflowActiveRunsRead
}

// ExecutionWorkflowReadPins carry the published revision identity and spec
// digest used to keep workflow catalog projections version-bound.
// ExecutionWorkflowReadPins содержат идентификатор опубликованной ревизии и
// дайджест спецификации для привязки проекции каталога к версии.
type ExecutionWorkflowReadPins struct {
	WorkflowRef, PublishedRef, SpecDigest string
	WorkflowVersion                       int64
}

// ExecutionWorkflowPublicationRead returns the immutable publication pins for
// the selected workflow entry without exposing an editable workflow draft.
// ExecutionWorkflowPublicationRead возвращает неизменяемые привязки публикации
// выбранного процесса без выдачи редактируемого черновика.
type ExecutionWorkflowPublicationRead struct{ Pins ExecutionWorkflowReadPins }

// ExecutionWorkflowActiveRunsRead carries the same publication identity for
// the active-run projection so consumers can compare exact revisions.
// ExecutionWorkflowActiveRunsRead переносит идентификатор публикации в
// проекцию активных запусков для сравнения точных ревизий.
type ExecutionWorkflowActiveRunsRead struct{ Pins ExecutionWorkflowReadPins }
