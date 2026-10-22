package entity

import "time"

// ExecutionWorkflowCatalogEntry describes a workflow's published reference, version, specification
// digest, input schema, and launch readiness. Discovery supplies selection evidence; launching still
// validates the exact current publication and caller authority.
// ExecutionWorkflowCatalogEntry описывает опубликованную ссылку, версию, дайджест спецификации, схему
// входа и готовность процесса к запуску. Обнаружение предоставляет свидетельства выбора; запуск
// проверяет точную текущую публикацию и полномочия вызывающего субъекта.
type ExecutionWorkflowCatalogEntry struct {
	WorkflowRef, Name, Purpose, PublishedRef, SpecDigest string
	WorkflowVersion                                      int64
	Inputs                                               []WorkflowInputField
	Readiness                                            WorkflowLaunchReadiness
}

// ExecutionWorkflowCatalog returns a page of eligible workflow entries with an opaque next-page token
// and optional publication or active-run details. The producer selects the requested projection; the
// struct does not enforce exclusive optional fields.
// ExecutionWorkflowCatalog возвращает страницу допустимых процессов с непрозрачным токеном следующей
// страницы и необязательными сведениями о публикации или активных запусках. Производитель выбирает
// запрошенное представление; структура не обеспечивает исключение необязательных полей.
type ExecutionWorkflowCatalog struct {
	Items         []ExecutionWorkflowCatalogEntry
	NextPageToken string
	Publication   *ExecutionWorkflowPublication
	ActiveRuns    *ExecutionWorkflowActiveRuns
}

// ExecutionWorkflowPublication carries configuration bytes and their content digest for a selected
// publication. Reading the bytes does not authorize modifying the publication or launching an unpinned
// replacement.
// ExecutionWorkflowPublication передаёт байты конфигурации и дайджест содержимого выбранной
// публикации. Чтение байтов не разрешает изменение публикации или запуск незакреплённой подмены.
type ExecutionWorkflowPublication struct {
	ConfigurationJSON   []byte
	ConfigurationSHA256 string
}

// ExecutionWorkflowActiveRun describes a run against an exact workflow publication with title, state,
// aggregate version, and creation time. It is a read projection and does not grant permission to
// cancel or continue that run.
// ExecutionWorkflowActiveRun описывает запуск относительно точной публикации процесса с названием,
// состоянием, версией агрегата и временем создания. Это проекция чтения, не разрешающая отмену или
// продолжение запуска.
type ExecutionWorkflowActiveRun struct {
	RunRef, WorkflowRef, PublishedRef, SpecDigest, Title, State string
	PublishedVersion                                            int32
	RunVersion                                                  int64
	CreatedAt                                                   time.Time
}

// ExecutionWorkflowActiveRuns groups a page of active-run projections under workflow version and
// publication pins. The next-page token and pins preserve query context rather than authorizing access
// to a different workflow.
// ExecutionWorkflowActiveRuns объединяет страницу представлений активных запусков под привязками
// версии и публикации процесса. Токен следующей страницы и привязки сохраняют контекст запроса, а не
// разрешают доступ к другому процессу.
type ExecutionWorkflowActiveRuns struct {
	WorkflowRef, PublishedRef, SpecDigest string
	WorkflowVersion                       int64
	Items                                 []ExecutionWorkflowActiveRun
	NextPageToken                         string
}
