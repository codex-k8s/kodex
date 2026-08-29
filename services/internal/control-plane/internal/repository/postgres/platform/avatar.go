package platform

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

const avatarContentPathPrefix = "/api/v1/artifacts/"

func parseAvatarArtifactURL(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", errs.ErrInvalid
	}
	if !strings.HasPrefix(parsed.Path, avatarContentPathPrefix) || !strings.HasSuffix(parsed.Path, "/content") {
		return "", "", errs.ErrInvalid
	}
	artifactRef := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, avatarContentPathPrefix), "/content")
	if artifactRef == "" || strings.Contains(artifactRef, "/") {
		return "", "", errs.ErrInvalid
	}
	query := parsed.Query()
	if len(query) != 1 || len(query["purpose"]) != 1 || query.Get("purpose") != "PREVIEW" {
		return "", "", errs.ErrInvalid
	}
	canonical := avatarArtifactContentURL(artifactRef)
	if value != canonical {
		return "", "", errs.ErrInvalid
	}
	return canonical, artifactRef, nil
}

func avatarArtifactContentURL(artifactRef string) string {
	return avatarContentPathPrefix + artifactRef + "/content?purpose=PREVIEW"
}

func (repository *Repository) validateAvatarArtifact(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectID string,
	value string,
) (string, error) {
	canonical, artifactRef, err := parseAvatarArtifactURL(value)
	if err != nil || canonical == "" {
		return canonical, err
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "artifact.manage", "ARTIFACT", artifactRef, "")
	if err != nil {
		return "", errs.ErrInvalid
	}
	if err := repository.requireAccess(ctx, tx, current, "artifact.manage", target); err != nil {
		return "", errs.ErrNotFound
	}
	var storedRef string
	err = tx.QueryRow(ctx, queryCommandsValidateAvatarArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_id":      projectID,
		"artifact_ref":    artifactRef,
	}).Scan(&storedRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrInvalid
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	if storedRef != artifactRef {
		return "", errs.ErrConflict
	}
	return canonical, nil
}

func (repository *Repository) validateAvatarUpdate(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectID string,
	currentValue string,
	nextValue string,
) (string, error) {
	if nextValue == currentValue {
		canonical, _, err := parseAvatarArtifactURL(nextValue)
		return canonical, err
	}
	return repository.validateAvatarArtifact(ctx, tx, current, projectID, nextValue)
}
