package controlplane

import (
	"context"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var _ domainrepo.ImageTransaction = (*transaction)(nil)

func (wrapped *transaction) NextImageBuild(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlImageBuildNext, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"now":             now,
	}))
}

func (wrapped *transaction) NextImageAdmission(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlImageAdmissionNext, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"now":             now,
	}))
}

func (wrapped *transaction) NextImagePromotion(
	ctx context.Context,
	organizationID, projectID string,
	policyRevision uint64,
	policySHA256 string,
	now time.Time,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlImagePromotionNext, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"policy_revision": policyRevision,
		"policy_sha256":   policySHA256,
		"now":             now,
	}))
}

func (wrapped *transaction) PromotedImageArtifactBySpec(
	ctx context.Context,
	organizationID, projectID, ownerActorID, specSHA256 string,
	policyRevision uint64,
	policySHA256 string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlImageArtifactPromotedBySpec, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"owner_actor_id":  ownerActorID,
		"spec_sha256":     specSHA256,
		"policy_revision": policyRevision,
		"policy_sha256":   policySHA256,
	}))
}

func (wrapped *transaction) ImageBuildsForRecipeForUpdate(
	ctx context.Context,
	organizationID, projectID, recipeID string,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(ctx, sqlImageBuildForRecipeUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"recipe_id":       recipeID,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	resources := make([]entity.Resource, 0)
	for rows.Next() {
		resource, scanErr := scanResource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		resources = append(resources, resource)
	}
	return resources, mapError(rows.Err())
}

func (wrapped *transaction) ImageArtifactsForRecipeForUpdate(
	ctx context.Context,
	organizationID, projectID, recipeID string,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(ctx, sqlImageArtifactForRecipeUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"recipe_id":       recipeID,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	resources := make([]entity.Resource, 0)
	for rows.Next() {
		resource, scanErr := scanResource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		resources = append(resources, resource)
	}
	return resources, mapError(rows.Err())
}
