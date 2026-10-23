package platform

import _ "embed"

var (
	// queryEnvironmentDraftGet reads one runtime-environment draft by organization/ref with saved specification, base version, scope and diagnostics.
	// queryEnvironmentDraftGet читает один черновик runtime-окружения по организации и ссылке с сохранённой спецификацией, базовой версией, областью и диагностикой.
	//go:embed sql/environment_draft_get.sql
	queryEnvironmentDraftGet string
	// queryEnvironmentDraftLock locks one runtime-environment draft by organization/ref and returns its base version, scope, state and diagnostics for owner mutation.
	// queryEnvironmentDraftLock блокирует один черновик runtime-окружения по организации и ссылке и возвращает базовую версию, область, состояние и диагностику для изменения владельцем.
	//go:embed sql/environment_draft_lock.sql
	queryEnvironmentDraftLock string
	// queryEnvironmentDraftInsert creates a DRAFT environment specification with actor and optional exact base revision in the matching organization, project and scope.
	// queryEnvironmentDraftInsert создаёт DRAFT спецификацию окружения с автором и необязательной точной базовой ревизией в совпадающих организации, проекте и области.
	//go:embed sql/environment_draft_insert.sql
	queryEnvironmentDraftInsert string
	// queryEnvironmentDraftUpdate updates a nonterminal draft at expected version with specification, validation and publication fields, advancing version and optionally saved time.
	// queryEnvironmentDraftUpdate обновляет незавершённый черновик при ожидаемой версии со спецификацией, полями проверки и публикации, увеличивая версию и при необходимости время сохранения.
	//go:embed sql/environment_draft_update.sql
	queryEnvironmentDraftUpdate string
)
