package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/model_catalog__lock_completion.sql
var queryModelCatalogLockCompletion string

//go:embed sql/model_catalog__complete.sql
var queryModelCatalogComplete string

func canonicalModelCatalogObservation(task platformrepo.ProviderModelCatalogTask, observation platformrepo.ProviderModelCatalogObservation) (platformrepo.ProviderModelCatalogObservation, string, string, error) {
	if observation.AccountRef != task.AccountRef || observation.CredentialRef != task.CredentialRef || observation.ObservedAt.IsZero() || len(observation.Models) > 128 {
		return observation, "", "", errs.ErrInvalid
	}
	if observation.Failure == "NONE" {
		if !(task.AuthorizationMethod == "API_KEY" && observation.Source == "REMOTE_API" || task.AuthorizationMethod == "DEVICE_CODE" && observation.Source == "REMOTE_CODEX") {
			return observation, "", "", errs.ErrInvalid
		}
	} else if !slices.Contains([]string{"UNAVAILABLE", "UNVERIFIED_SOURCE", "AUTHORIZATION_REJECTED"}, observation.Failure) || observation.Source != "" || len(observation.Models) != 0 {
		return observation, "", "", errs.ErrInvalid
	}
	observation.Models = slices.Clone(observation.Models)
	if observation.Models == nil {
		observation.Models = []platformrepo.ProviderModelCatalogRecord{}
	}
	slices.SortFunc(observation.Models, func(a, b platformrepo.ProviderModelCatalogRecord) int { return strings.Compare(a.ID, b.ID) })
	for index := range observation.Models {
		model := &observation.Models[index]
		if !validModel(model.ID) || len(model.ReasoningEfforts) > 16 || index > 0 && observation.Models[index-1].ID == model.ID {
			return observation, "", "", errs.ErrInvalid
		}
		model.ReasoningEfforts = slices.Clone(model.ReasoningEfforts)
		if model.ReasoningEfforts == nil {
			model.ReasoningEfforts = []string{}
		}
		slices.Sort(model.ReasoningEfforts)
		for position, effort := range model.ReasoningEfforts {
			if runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil || position > 0 && model.ReasoningEfforts[position-1] == effort {
				return observation, "", "", errs.ErrInvalid
			}
		}
		if len(model.ReasoningEfforts) == 0 && model.DefaultReasoningEffort != "" || len(model.ReasoningEfforts) > 0 && !slices.Contains(model.ReasoningEfforts, model.DefaultReasoningEffort) {
			return observation, "", "", errs.ErrInvalid
		}
	}
	rawModels, _ := json.Marshal(observation.Models)
	if len(rawModels) > 128<<10 {
		return observation, "", "", errs.ErrInvalid
	}
	// Время наблюдения является freshness, а не content identity.
	contentDigest := digestBytes([]byte(task.OrganizationID), []byte(task.AccountRef), []byte(task.ProviderDefinitionKey), []byte(observation.Source), rawModels)
	rawReceipt, _ := json.Marshal(observation)
	return observation, contentDigest, digestBytes(rawReceipt), nil
}

func (repository *Repository) CompleteProviderModelCatalogTask(ctx context.Context, task platformrepo.ProviderModelCatalogTask, observation platformrepo.ProviderModelCatalogObservation) error {
	observation, contentDigest, receiptDigest, err := canonicalModelCatalogObservation(task, observation)
	if err != nil || !validModelCatalogDigest(task.RequestDigest) {
		return errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, state, requestDigest, claimant, fence, savedRequest, savedReceipt string
	var generation int64
	var expires *time.Time
	var now time.Time
	var eligible bool
	var stored platformrepo.ProviderModelCatalogTask
	err = tx.QueryRow(ctx, queryModelCatalogLockCompletion, task.OrganizationID, task.Ref).Scan(&id, &state, &requestDigest, &claimant, &generation, &fence, &expires, &eligible, &savedRequest, &savedReceipt, &now,
		&stored.AccountRef, &stored.AccountVersion, &stored.ProviderDefinitionKey, &stored.CredentialRef, &stored.CredentialRevision,
		&stored.Credential.SecretName, &stored.Credential.SecretUID, &stored.Credential.SecretResourceVersion, &stored.Credential.ContentSHA256, &stored.AuthorizationMethod)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if stored.AccountRef != task.AccountRef || stored.AccountVersion != task.AccountVersion || stored.ProviderDefinitionKey != task.ProviderDefinitionKey || stored.CredentialRef != task.CredentialRef || stored.CredentialRevision != task.CredentialRevision || stored.Credential != task.Credential || stored.AuthorizationMethod != task.AuthorizationMethod {
		return errs.ErrConflict
	}
	if state == "COMPLETED" {
		if savedRequest == task.RequestDigest && savedReceipt == receiptDigest {
			return nil
		}
		return errs.ErrConflict
	}
	if state != "CLAIMED" || !eligible || requestDigest != task.RequestDigest || claimant != task.ClaimantID || generation != task.ClaimGeneration || fence != task.Fence || expires == nil || !expires.Equal(task.ExpiresAt) || !expires.After(now) || observation.ObservedAt.After(now.Add(2*time.Second)) || observation.ObservedAt.Before(expires.Add(-15*time.Second)) {
		return errs.ErrConflict
	}
	result, err := tx.Exec(ctx, queryModelCatalogComplete, id, receiptDigest, observation.Source, observation.Failure, asJSON(observation.Models), contentDigest, observation.ObservedAt)
	if err != nil {
		return errs.ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
