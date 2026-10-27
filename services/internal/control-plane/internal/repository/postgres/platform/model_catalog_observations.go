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

// queryModelCatalogLockCompletion embeds task/account locks and immutable credential metadata with
// current account/version eligibility, saved completion receipt and database time. Caller must
// compare the full claim tuple; stored references alone do not prove freshness.
// queryModelCatalogLockCompletion встраивает task/account locks и immutable credential metadata с
// текущей account/version допустимостью, сохранённой completion receipt и временем БД. Caller
// сравнивает полный claim tuple; сохранённые ссылки сами не доказывают свежесть.
//
//go:embed sql/model_catalog__lock_completion.sql
var queryModelCatalogLockCompletion string

// queryModelCatalogComplete embeds observation insertion from a CLAIMED task and atomic task
// completion clearing claimant/fence/request/expiry. Observation retention is15 minutes from
// observed time; caller verifies eligibility and receipt pins before Exec.
// queryModelCatalogComplete встраивает вставку observation из CLAIMED task и атомарное завершение
// task с очисткой claimant/fence/request/expiry. Retention observation составляет15 минут от
// времени наблюдения; caller проверяет допустимость и receipt pins до Exec.
//
//go:embed sql/model_catalog__complete.sql
var queryModelCatalogComplete string

// canonicalModelCatalogObservation matches account/credential and nonzero observation time, closes
// success/failure source combinations and bounds128 models/16 efforts. It clones and sorts
// models/efforts, rejects duplicate/invalid IDs, inconsistent defaults and oversized JSON. Content
// digest covers owner/provider/source/models, while receipt digest also records observation
// metadata; freshness and live claim authority are checked later.
// canonicalModelCatalogObservation сопоставляет account/credential и ненулевое время observation,
// ограничивает success/failure source combinations и128 models/16 efforts. Копирует и сортирует
// models/efforts, отклоняет duplicate/invalid IDs, несогласованные defaults и слишком большой
// JSON. Content digest охватывает owner/provider/source/models, receipt digest также фиксирует
// observation metadata; свежесть и live claim authority проверяются позднее.
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
	defaults := 0
	for index := range observation.Models {
		model := &observation.Models[index]
		if !validModel(model.ID) || len(model.ReasoningEfforts) > 16 || index > 0 && observation.Models[index-1].ID == model.ID {
			return observation, "", "", errs.ErrInvalid
		}
		if model.IsDefault {
			defaults++
			if defaults > 1 {
				return observation, "", "", errs.ErrInvalid
			}
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

// CompleteProviderModelCatalogTask canonicalizes observation then locks the saved owner task
// within5s, comparing account/credential tuple and exact claim/request/fence/expiry plus DB-time
// freshness. COMPLETED accepts only the same request/receipt digests without rewriting. Fresh
// completion requires one row and atomically updates verification plus platform event before
// commit; it performs no provider probe or credential issuance.
// CompleteProviderModelCatalogTask канонизирует observation и за5s блокирует сохранённую owner
// task, сравнивая account/credential tuple и точные claim/request/fence/expiry со свежестью по
// времени БД. COMPLETED принимает лишь те же request/receipt digests без перезаписи. Новое
// завершение требует одну строку и атомарно обновляет verification и platform event до Commit;
// provider probe и выдачи credential нет.
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
	var id, state, requestDigest, claimant, fence, savedRequest, savedReceipt, organizationRef string
	var generation int64
	var expires *time.Time
	var now time.Time
	var eligible bool
	var stored platformrepo.ProviderModelCatalogTask
	err = tx.QueryRow(ctx, queryModelCatalogLockCompletion, task.OrganizationID, task.Ref).Scan(&id, &state, &requestDigest, &claimant, &generation, &fence, &expires, &eligible, &savedRequest, &savedReceipt, &now,
		&stored.AccountRef, &stored.AccountVersion, &stored.ProviderDefinitionKey, &stored.CredentialRef, &stored.CredentialRevision,
		&stored.Credential.SecretName, &stored.Credential.SecretUID, &stored.Credential.SecretResourceVersion, &stored.Credential.ContentSHA256, &stored.AuthorizationMethod, &organizationRef)
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
	if _, err := tx.Exec(ctx, queryProviderVerificationComplete, pgx.StrictNamedArgs{"task_id": id}); err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, scope{
		organizationID: task.OrganizationID, organizationRef: organizationRef, correlationRef: task.Ref,
	}, "PROVIDER_ACCOUNT_CHANGED", "", task.AccountRef, "i18n:PROVIDER_ACCOUNT_UPDATED", task.AccountVersion, ""); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
