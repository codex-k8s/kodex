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

//go:embed sql/integration_invocation_authorize_completion.sql
var queryIntegrationInvocationAuthorizeCompletion string

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
