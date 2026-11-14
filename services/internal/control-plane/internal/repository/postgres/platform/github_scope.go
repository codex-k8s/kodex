package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryPersonalGitHubSelectionProof checks finite owner-qualified discovery history on the same current credential and configuration pins.
// queryPersonalGitHubSelectionProof проверяет конечную owner-qualified историю discovery с актуальными credential и configuration pins.
//
//go:embed sql/github_scope__selection_proof.sql
var queryPersonalGitHubSelectionProof string

// validatePersonalGitHubScopeSelection refuses caller-created repository IDs; selection is limited to exact repositories observed through this actor's current personal credential.
// validatePersonalGitHubScopeSelection отклоняет созданные caller ID; выбор ограничен точными репозиториями, наблюдавшимися через текущий личный ключ этого actor.
// Discovery supplies coordinates only. Each actual execution separately rechecks current explicit membership and provider permissions.
// Discovery даёт лишь координаты. Каждое исполнение отдельно проверяет актуальные явный доступ и права провайдера.
func (repository *Repository) validatePersonalGitHubScopeSelection(ctx context.Context, tx pgx.Tx, current scope, connectionRef string, configuration map[string]string) error {
	selected, err := integrationpackage.ParseGitHubResourceScope(configuration)
	if err != nil {
		return errs.ErrInvalid
	}
	if selected.Kind != "SELECTED" {
		return nil
	}
	binding, err := readPersonalCredentialTx(ctx, tx, current, connectionRef)
	if err != nil {
		return err
	}
	if binding.State != "READY" || binding.Credential == nil || binding.ExpiresAt != nil && !binding.ExpiresAt.After(time.Now()) {
		return errs.ErrForbidden
	}
	var allowed bool
	err = tx.QueryRow(ctx, queryPersonalGitHubSelectionProof, pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID, "connection_ref": connectionRef, "binding_ref": binding.Ref, "binding_generation": binding.Generation, "credential_ref": binding.Credential.Ref, "connection_version": binding.ConnectionVersion, "configuration_digest": binding.VerifiedConfigurationDigest, "definition_digest": binding.VerifiedDefinitionDigest, "repositories": asJSON(selected.Repositories)}).Scan(&allowed)
	if err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrForbidden
	}
	return nil
}
