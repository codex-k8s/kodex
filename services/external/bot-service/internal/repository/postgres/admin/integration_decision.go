package admin

import (
	"context"
	"fmt"

	integrationsdomain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	integrationpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/integrations"
	"github.com/jackc/pgx/v5"
)

// DecideIntegrationApproval сохраняет решение внутри транзакции погашения
// callback capability, не добавляя integration SQL в общий admin repository.
func (repo *Repository) DecideIntegrationApproval(ctx context.Context, input integrationsdomain.ApprovalDecisionInput) (integrationsdomain.Invocation, error) {
	tx, ok := repo.db.(pgx.Tx)
	if !ok {
		return integrationsdomain.Invocation{}, fmt.Errorf("integration approval decision requires an active callback transaction")
	}
	return integrationpostgres.NewTransactionalRepository(tx).DecideApproval(ctx, input)
}
