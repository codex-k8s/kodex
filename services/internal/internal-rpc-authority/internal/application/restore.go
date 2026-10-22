package application

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// RestoreController forwards specialized restore commands and queries to the domain; operator authority,
// durable coordination and completion evidence are not owned by this wrapper.
// RestoreController передаёт специализированные команды и запросы восстановления домену; полномочия
// оператора, устойчивая координация и свидетельство завершения не принадлежат обёртке.
type RestoreController struct {
	domain *service.RestoreController
}

// NewRestoreController wraps the supplied domain controller without validating dependencies or preparing
// restore state; composition owns initialization.
// NewRestoreController оборачивает переданный доменный контроллер без проверки зависимостей или
// подготовки состояния восстановления; инициализация принадлежит композиции.
func NewRestoreController(domain *service.RestoreController) *RestoreController {
	return &RestoreController{domain: domain}
}

// Prepare forwards the validated preparation command unchanged so the domain owns pinned restore intent,
// idempotency and transition checks.
// Prepare без изменения передаёт проверенную команду подготовки, оставляя домену закреплённое намерение,
// идемпотентность и проверки переходов.
func (application *RestoreController) Prepare(
	ctx context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Prepare(ctx, command)
}

// AuthorizeOperator forwards verified operator claims with exact RPC, idempotency and semantic digest to
// domain authorization consumption; it does not infer authority from a request identifier.
// AuthorizeOperator передаёт проверенные данные оператора с точными RPC, идемпотентностью и смысловым
// хешем доменному потреблению полномочия; идентификатор запроса не создаёт полномочия.
func (application *RestoreController) AuthorizeOperator(
	ctx context.Context,
	credential model.RestoreOperatorCredential,
	fullMethod string,
	idempotencyKey string,
	semanticDigest string,
) error {
	return application.domain.AuthorizeOperator(
		ctx,
		credential,
		fullMethod,
		idempotencyKey,
		semanticDigest,
	)
}

// GetDirective delegates the observed restore peer, role credential and revision to the domain's exact
// role-bound directive path without widening role eligibility.
// GetDirective передаёт наблюдаемую сторону восстановления, учётные данные роли и ревизию точному
// доменному пути директивы без расширения допустимых ролей.
func (application *RestoreController) GetDirective(
	ctx context.Context,
	peer service.RestorePeer,
	roleCredential string,
	observedRevision uint64,
) (service.RestoreDirectiveResult, error) {
	return application.domain.GetDirective(
		ctx,
		peer,
		roleCredential,
		observedRevision,
	)
}

// Acknowledge delegates peer/role/directive/ACK binding and idempotency to domain acknowledgement
// verification and persistence; the wrapper cannot self-confirm a role.
// Acknowledge передаёт привязку стороны, роли, директивы, ACK и идемпотентность доменной проверке и
// сохранению подтверждения; обёртка не может сама подтвердить роль.
func (application *RestoreController) Acknowledge(
	ctx context.Context,
	peer service.RestorePeer,
	roleCredential string,
	directive string,
	ack string,
	idempotencyKey string,
) (service.RestoreACKResult, error) {
	return application.domain.Acknowledge(
		ctx,
		peer,
		roleCredential,
		directive,
		ack,
		idempotencyKey,
	)
}

// Complete forwards the completion command to the domain's full restore barrier and evidence checks,
// preserving the authoritative resulting state or error.
// Complete передаёт команду завершения полному доменному барьеру и проверкам свидетельств, сохраняя
// авторитетное итоговое состояние либо ошибку.
func (application *RestoreController) Complete(
	ctx context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	return application.domain.Complete(ctx, command)
}

// Recover delegates restart recovery of restore coordination to the domain; it adds no retry loop, clock
// or direct storage access.
// Recover передаёт домену восстановление координации после перезапуска без дополнительного цикла
// повторов, часов или прямого доступа к хранилищу.
func (application *RestoreController) Recover(ctx context.Context) error {
	return application.domain.Recover(ctx)
}

// Ready returns the domain's checked coordination state through its full readiness path, not a state
// inferred from wrapper construction.
// Ready возвращает проверенное доменом состояние координации через полный путь готовности, а не выводит
// его из создания обёртки.
func (application *RestoreController) Ready(
	ctx context.Context,
) (model.RestoreState, error) {
	return application.domain.Ready(ctx)
}

// StartupReady delegates the domain startup barrier that omits publisher readback; it must not be
// represented as the controller's full readiness proof.
// StartupReady передаёт доменный барьер запуска без чтения подтверждений публикации; он не является
// доказательством полной готовности контроллера.
func (application *RestoreController) StartupReady(
	ctx context.Context,
) (model.RestoreState, error) {
	return application.domain.StartupReady(ctx)
}

// RoleTrustMetadata returns the domain controller's served role-trust metadata; it neither activates
// trust nor supplies private role credentials.
// RoleTrustMetadata возвращает обслуживаемые метаданные доверия ролей доменного контроллера; активации
// доверия и выдачи закрытых данных роли здесь нет.
func (application *RestoreController) RoleTrustMetadata() model.RestoreRoleTrustMetadata {
	return application.domain.RoleTrustMetadata()
}

// SignerGeneration reads the controller domain's signer generation without rotating, publishing or
// authorizing a new signer.
// SignerGeneration читает поколение подписанта доменного контроллера без ротации, публикации или
// разрешения нового подписанта.
func (application *RestoreController) SignerGeneration() uint64 {
	return application.domain.SignerGeneration()
}
