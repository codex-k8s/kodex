package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// runtimeMaterializationResolveProofSQL embeds the active owner execution graph lookup for an
// already registered operation/request digest, enforcing lease/deadline/generation/input/attempt,
// active actor and specialized project/assistant lineage. It returns current authority versions
// and expiry from owner rows rather than caller scope.
// runtimeMaterializationResolveProofSQL встраивает поиск активного owner execution graph для уже
// зарегистрированных operation/request digest с проверкой lease/deadline/generation/input/attempt,
// активного субъекта и специализированного project/assistant происхождения. Возвращает текущие
// версии полномочий и срок из строк владельца, а не scope клиента.
//
//go:embed sql/runtime_materialization_resolve_proof.sql
var runtimeMaterializationResolveProofSQL string

// runtimeMaterializationProofOperation recognizes only the two fixed normal/system-assistant
// materialization operation strings. It is dispatch classification, not credential or resource
// authorization.
// runtimeMaterializationProofOperation распознаёт только две фиксированные строки operation
// обычной и системной материализации. Это классификация диспетчеризации, а не проверка credential
// и доступа к ресурсу.
func runtimeMaterializationProofOperation(operation string) bool {
	return operation == runtimeMaterializationOperation || operation == assistantMaterializationOperation
}

// resolveRuntimeMaterializationProof requires runtime-controller, exact technical actor/tenant and
// operation-specific project presence plus canonical request digest. It resolves registered live
// owner execution, maps USER/SERVICE actor kind and derives a positive actor version from database
// update time. Missing/invalid data is Forbidden and backend failure Unavailable; it returns
// authority metadata without issuing credentials or materializing secrets.
// resolveRuntimeMaterializationProof требует runtime-controller, точные технические actor/tenant,
// наличие проекта по виду operation и канонический дайджест запроса. Разрешает зарегистрированное
// действующее owner execution, преобразует USER/SERVICE и выводит положительную версию субъекта из
// времени обновления БД. Отсутствующие или неверные данные дают Forbidden, ошибка хранилища —
// Unavailable; возвращает метаданные полномочий без выдачи credentials и материализации секретов.
func (repository *Repository) resolveRuntimeMaterializationProof(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	assistant := input.Operation == assistantMaterializationOperation
	decoded, digestErr := hex.DecodeString(input.RequestDigestSHA256)
	if input.CallerWorkload != "runtime-controller" || !runtimeMaterializationProofOperation(input.Operation) ||
		input.ExternalActorID != "kodex-system-subject" || input.ExternalTenantID != "kodex-installation" ||
		(assistant && input.ProjectRef != "") || (!assistant && input.ProjectRef == "") ||
		digestErr != nil || len(decoded) != 32 || strings.ToLower(input.RequestDigestSHA256) != input.RequestDigestSHA256 {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	var authority platformrepo.ProofAuthority
	var execution platformrepo.RuntimeExecutionProof
	var actorKind string
	var actorUpdatedAt time.Time
	err := repository.pool.QueryRow(ctx, runtimeMaterializationResolveProofSQL, pgx.StrictNamedArgs{
		"operation":      input.Operation,
		"request_digest": input.RequestDigestSHA256, "project_ref": input.ProjectRef,
		"system_assistant": assistant,
	}).Scan(&authority.ActorID, &actorKind, &actorUpdatedAt,
		&authority.OrganizationID, &authority.OrganizationVersion,
		&authority.ProjectID, &authority.ProjectVersion,
		&execution.RevisionID, &execution.Generation, &execution.RevisionDigest, &execution.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	switch actorKind {
	case "USER":
		execution.ActorKind = "HUMAN"
	case "SERVICE":
		execution.ActorKind = "SERVICE"
	default:
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if actorUpdatedAt.UnixMicro() <= 0 {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	authority.ActorVersion = uint64(actorUpdatedAt.UnixMicro())
	authority.RuntimeExecution = &execution
	return authority, nil
}
