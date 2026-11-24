package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPersonalModelRootSource resolves the existing root's immutable launch source.
// queryPersonalModelRootSource читает исходный канал запуска существующего корневого запуска.
//
//go:embed sql/personal_model__root_source.sql
var queryPersonalModelRootSource string

// queryPersonalModelReadyAccounts returns only active subject-owned account refs for the requested provider.
// queryPersonalModelReadyAccounts возвращает ссылки только на активные личные аккаунты заданного провайдера.
//
//go:embed sql/personal_model__ready_accounts.sql
var queryPersonalModelReadyAccounts string

// interactiveModelAccountSubject restricts personal model selection to attended launch sources.
// interactiveModelAccountSubject разрешает выбор личного аккаунта для интерактивных запусков.
func interactiveModelAccountSubject(source, actorID string) string {
	switch source {
	case "", "CONTROL_CENTER", "SYSTEM_ASSISTANT":
		return actorID
	default:
		return ""
	}
}

// selectReadyProviderAccountForRoot keeps account selection anchored to the original actor and launch source, including delegation and rework.
// selectReadyProviderAccountForRoot сохраняет исходного инициатора и источник запуска при делегировании и доработке.
func (repository *Repository) selectReadyProviderAccountForRoot(ctx context.Context, tx pgx.Tx, organizationID, rootID, initiatorID, agentRef string) (string, error) {
	var source string
	if err := tx.QueryRow(ctx, queryPersonalModelRootSource,
		rootID, organizationID, initiatorID).Scan(&source); err != nil {
		return "", errs.ErrUnavailable
	}
	return repository.selectReadyProviderAccountForAgent(ctx, tx, organizationID, agentRef, interactiveModelAccountSubject(source, initiatorID))
}

// preflightModelAccountsForLaunch checks all published workflow roles before allocating the first session and returns the first role's account. Stable lock order avoids taking agent locks in workflow step order.
// preflightModelAccountsForLaunch проверяет все роли опубликованного процесса до создания первой сессии и возвращает аккаунт первой роли. Порядок блокировок стабилен и не зависит от порядка шагов.
func (repository *Repository) preflightModelAccountsForLaunch(ctx context.Context, tx pgx.Tx, organizationID string, agentRefs []string, personalSubjectID string) (string, error) {
	refs := slices.Clone(agentRefs)
	slices.Sort(refs)
	var firstAccountID string
	for _, ref := range slices.Compact(refs) {
		accountID, err := repository.selectReadyProviderAccountForAgent(ctx, tx, organizationID, ref, personalSubjectID)
		if err != nil {
			return "", err
		}
		if ref == agentRefs[0] {
			firstAccountID = accountID
		}
	}
	if firstAccountID == "" {
		return "", errs.ErrConflict
	}
	return firstAccountID, nil
}

// validatePersonalRuntimePreferences checks the deployed provider and model without assuming a user's current keys or reasoning capabilities. Live launch separately checks the owner's current catalog.
// validatePersonalRuntimePreferences проверяет модель и настройки установленного провайдера. Доступность и возможности конкретного ключа проверяются по каталогу владельца при запуске.
func validatePersonalRuntimePreferences(ctx context.Context, tx pgx.Tx, provider, model, overlay string) error {
	configuration, err := runtimeCatalogProviderConfiguration(ctx, tx, provider)
	if err != nil {
		return err
	}
	preferences, err := runtimecontract.ParseConfigOverlay(overlay)
	if err != nil {
		return errs.ErrInvalid
	}
	effort, mode := preferences.ModelReasoningEffort, runtimecontract.ReasoningUnsupported
	if effort != "" {
		mode = runtimecontract.ReasoningSupported
	}
	if err := configuration.ValidateRuntimePreferences(model, overlay, effort, mode); err != nil {
		return errs.ErrInvalid
	}
	return nil
}

// validateConfiguredOverlay checks either the shared account catalog or the deployed model contract for actor-owned credentials.
// validateConfiguredOverlay проверяет каталог общих аккаунтов либо контракт модели, если аккаунт будет выбран по инициатору.
func validateConfiguredOverlay(ctx context.Context, tx pgx.Tx, current scope, configuration entity.AgentRuntimeConfiguration, overlay string) error {
	if configuration.ProviderPolicy.Mode == "PERSONAL" {
		return validatePersonalRuntimePreferences(ctx, tx, configuration.Provider, configuration.Model, overlay)
	}
	_, _, err := validateRuntimeCatalogCandidates(ctx, tx, current, configuration.Provider, configuration.Model, overlay, configuration.ProviderPolicy.AccountCandidates, false)
	return err
}

// readyPersonalModelAccounts lists live ready catalogs for the authenticated initiator. It is a read-side projection and reserves neither a credential nor execution capacity.
// readyPersonalModelAccounts читает доступные каталоги текущего инициатора для интерфейса. Функция не резервирует ключ и ёмкость исполнения.
func readyPersonalModelAccounts(ctx context.Context, tx pgx.Tx, current scope, configuration entity.AgentRuntimeConfiguration, overlay string) ([]entity.ProviderAccountCandidate, error) {
	rows, err := tx.Query(ctx, queryPersonalModelReadyAccounts, current.organizationID, current.actorID, configuration.Provider)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	result := []entity.ProviderAccountCandidate{}
	for _, ref := range refs {
		candidates, _, err := validateRuntimeCatalogCandidatesSnapshot(ctx, tx, current, configuration.Provider, configuration.Model, overlay,
			[]entity.ProviderAccountCandidate{{AccountRef: ref, Weight: 1}}, false, false)
		switch {
		case err == nil:
			result = append(result, candidates[0])
		case errors.Is(err, errs.ErrConflict), errors.Is(err, errs.ErrVersionMismatch), errors.Is(err, errs.ErrNotFound):
			continue
		default:
			return nil, err
		}
	}
	return result, nil
}
