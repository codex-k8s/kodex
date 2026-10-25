package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationInvocationAuthorizeCompletion embeds exact
// tenant/invocation/workload/lease/fence-digest/generation matching with nonempty stored claim
// fields. It checks stored claim identity, not lease freshness, terminal eligibility or evidence
// of an external effect.
// queryIntegrationInvocationAuthorizeCompletion встраивает точное совпадение организации,
// invocation, workload, lease, дайджеста fence и generation при непустых сохранённых полях
// захвата. Проверяет идентичность захвата, но не свежесть аренды, допустимость завершения и
// доказательства внешнего эффекта.
//
//go:embed sql/integration_invocation_authorize_completion.sql
var queryIntegrationInvocationAuthorizeCompletion string

// queryIntegrationTestAuthorizeCompletion embeds exact
// tenant/test/workload/lease/fence-digest/generation matching with nonempty stored claim fields.
// Test state and lease expiry must be checked by the specialized transition path.
// queryIntegrationTestAuthorizeCompletion встраивает точное совпадение организации, теста,
// workload, lease, дайджеста fence и generation при непустых сохранённых полях захвата. Состояние
// теста и срок аренды проверяет специализированный путь перехода.
//
//go:embed sql/integration_test_authorize_completion.sql
var queryIntegrationTestAuthorizeCompletion string

// authorizeIntegrationTestCompletion requires a recognized execution workload and typed test
// payload, hashes the supplied fence and checks the exact stored test claim tuple. Query errors
// are Unavailable and a false match Forbidden; it neither validates terminal results nor replaces
// lifecycle/lease checks.
// authorizeIntegrationTestCompletion требует известный workload исполнения и типизированный
// payload теста, хеширует переданный fence и сверяет точный сохранённый захват теста. Ошибка
// запроса даёт Unavailable, несовпадение — Forbidden; не проверяет конечный результат и не
// заменяет проверки жизненного цикла и аренды.
func (repository *Repository) authorizeIntegrationTestCompletion(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	if _, err := integrationExecutionRoute(input.Principal.CallerWorkload); err != nil {
		return err
	}
	payload, ok := input.Payload.(command.IntegrationConnectionTestInput)
	if !ok {
		return errs.ErrInvalid
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	var allowed bool
	if err := tx.QueryRow(ctx, queryIntegrationTestAuthorizeCompletion, current.organizationID, payload.TestRef, input.Principal.CallerWorkload,
		payload.LeaseRef, hex.EncodeToString(digest[:]), payload.Generation).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrForbidden
	}
	return nil
}

// authorizeIntegrationCompletion requires a recognized execution workload and typed invocation
// payload, hashes the supplied fence and checks the exact stored invocation claim tuple. Query
// errors are Unavailable and a false match Forbidden; result validation and transition
// admissibility remain in the owner command path.
// authorizeIntegrationCompletion требует известный workload исполнения и типизированный payload
// invocation, хеширует переданный fence и сверяет точный сохранённый захват invocation. Ошибка
// запроса даёт Unavailable, несовпадение — Forbidden; проверка результата и допустимости перехода
// остаётся в пути команды владельца.
func (repository *Repository) authorizeIntegrationCompletion(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	if _, err := integrationExecutionRoute(input.Principal.CallerWorkload); err != nil {
		return err
	}
	payload, ok := input.Payload.(command.IntegrationInvocationInput)
	if !ok {
		return errs.ErrInvalid
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	var allowed bool
	if err := tx.QueryRow(ctx, queryIntegrationInvocationAuthorizeCompletion, current.organizationID, payload.InvocationRef,
		input.Principal.CallerWorkload, payload.LeaseRef, hex.EncodeToString(digest[:]), payload.Generation).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrForbidden
	}
	return nil
}
