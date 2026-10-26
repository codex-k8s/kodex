package platform

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// contextCatalogPageArgs binds the existing memory/skill list SQL inputs to the resolved
// organization, actor and authority project, caller filters, validated cursor and lookahead
// limit. It allocates a fresh strict argument map and samples UTC time once at the original
// QueryRow argument-building step. It does not choose SQL, normalize filters or decide access;
// the distinct queries retain their own authoritative eligibility predicates.
// contextCatalogPageArgs связывает прежние SQL inputs списков memory/skill с разрешёнными
// organization, actor и authority project, фильтрами caller, проверенным cursor и lookahead
// limit. Создаёт новый strict map и один раз читает UTC time на прежнем шаге подготовки
// аргументов QueryRow. Не выбирает SQL, не нормализует фильтры и не решает вопрос доступа;
// разные запросы сохраняют собственные авторитетные eligibility predicates.
func contextCatalogPageArgs(current scope, filter query.Filter, cursor string, limit int32) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project": current.authorityProjectID, "project_ref": filter.ProjectRef,
		"agent_ref": filter.ResourceRef, "state": filter.State, "query": filter.Query,
		"cursor_ref": cursor, "page_size": limit + 1, "evaluated_at": time.Now().UTC(),
	}
}
