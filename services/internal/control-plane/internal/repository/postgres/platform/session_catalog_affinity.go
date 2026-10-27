package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeCatalogBindSession embeds insertion of session/account catalog pins and model
// snapshot tied to the current agent policy within the supplied tenant. Validation, authorization,
// exactly-one-row handling and commit belong to the caller.
// queryRuntimeCatalogBindSession встраивает вставку session/account catalog pins и model snapshot,
// связанных с current agent policy переданного tenant. Validation, authorization, exactly-one-row
// handling и commit принадлежат caller.
//
//go:embed sql/runtime_catalog__bind_session.sql
var queryRuntimeCatalogBindSession string

// queryRuntimeCatalogSessionAccount embeds tenant-bound session account lookup with SHARE locks on
// session/account. It returns a ref without evaluating caller permissions or catalog freshness.
// queryRuntimeCatalogSessionAccount встраивает tenant-bound session account lookup с SHARE locks
// session/account. Возвращает ref без проверки caller permissions либо catalog freshness.
//
//go:embed sql/runtime_catalog__session_account.sql
var queryRuntimeCatalogSessionAccount string

// queryRuntimeCatalogReadSessionBinding embeds exact tenant/session/account catalog binding,
// retaining policy identity/version/digest and model JSON only while the session still uses that
// account. It does not validate the decoded models or authorize execution.
// queryRuntimeCatalogReadSessionBinding встраивает exact tenant/session/account catalog binding,
// сохраняя policy identity/version/digest и model JSON только пока session использует account. Не
// проверяет decoded models и не авторизует execution.
//
//go:embed sql/runtime_catalog__read_session_binding.sql
var queryRuntimeCatalogReadSessionBinding string

// sessionCatalogBinding holds retained catalog/model and account-policy pins for session affinity
// after configuration changes. It is a database snapshot, not current account eligibility or a
// transferable execution grant.
// sessionCatalogBinding хранит retained catalog/model и account-policy pins для session affinity
// после configuration changes. Это DB snapshot, а не current account eligibility или transferable
// execution grant.
type sessionCatalogBinding struct {
	CatalogRevision, CatalogDigest, Provider string
	PolicyID, PolicyRef, PolicyDigest        string
	PolicyVersion                            int64
	Models                                   []platformrepo.ProviderModelCatalogRecord
}

// bindSessionModelCatalog locks the session account, reads current agent configuration, requires
// exactly one matching configured candidate and validates its live catalog eligibility. Every
// returned model must be available before inserting one catalog/model binding. Errors leave all
// writes to caller rollback; this helper neither authorizes the actor nor commits.
// bindSessionModelCatalog блокирует session account, читает current agent configuration, требует
// ровно одного matching configured candidate и проверяет live catalog eligibility. Все returned
// models должны быть available до вставки одного catalog/model binding. При ошибке writes остаются
// caller rollback; helper не авторизует actor и не выполняет commit.
func bindSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, agentRef string) error {
	var accountRef string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogSessionAccount, sessionID, organizationID).Scan(&accountRef); err != nil {
		return errs.ErrConflict
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, organizationID, agentRef, "")
	if err != nil {
		return err
	}
	selected := []entity.ProviderAccountCandidate{}
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		if candidate.AccountRef == accountRef {
			selected = append(selected, candidate)
		}
	}
	if len(selected) != 1 {
		return errs.ErrConflict
	}
	if _, _, err := validateRuntimeCatalogCandidates(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, configuration.Model, overlay, selected, false); err != nil {
		return err
	}
	catalog, err := readModelCatalogTx(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, accountRef)
	if err != nil {
		return err
	}
	models := []platformrepo.ProviderModelCatalogRecord{}
	for _, model := range catalog.Models {
		if !model.Available {
			return errs.ErrConflict
		}
		models = append(models, platformrepo.ProviderModelCatalogRecord{ID: model.ID, DefaultReasoningEffort: model.DefaultReasoningEffort, ReasoningEfforts: model.ReasoningEfforts, IsDefault: model.IsDefault})
	}
	result, err := tx.Exec(ctx, queryRuntimeCatalogBindSession, sessionID, organizationID, agentRef, catalog.Revision, catalog.Digest, asJSON(models))
	if err != nil {
		return errs.ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// readSessionModelCatalog reads one exact binding and decodes stored model JSON with the existing
// decodeStrict rules. Missing binding is Conflict, backend/decode errors Unavailable; partial
// binding values may accompany errors and must be discarded by callers.
// readSessionModelCatalog читает один exact binding и декодирует stored model JSON по существующим
// decodeStrict rules. Missing binding даёт Conflict, backend/decode errors — Unavailable; partial
// binding values могут сопровождать errors и должны отбрасываться caller.
func readSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, accountRef string) (sessionCatalogBinding, error) {
	var binding sessionCatalogBinding
	var raw []byte
	if err := tx.QueryRow(ctx, queryRuntimeCatalogReadSessionBinding, sessionID, organizationID, accountRef).Scan(&binding.CatalogRevision, &binding.CatalogDigest, &raw, &binding.Provider, &binding.PolicyID, &binding.PolicyRef, &binding.PolicyVersion, &binding.PolicyDigest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return binding, errs.ErrConflict
		}
		return binding, errs.ErrUnavailable
	}
	if decodeStrict(raw, &binding.Models) != nil {
		return binding, errs.ErrUnavailable
	}
	return binding, nil
}

// checkedSessionModelCatalog delegates session-affinity resolution with candidate validation
// locking enabled. It retains current configuration and tenant/session/account arguments exactly
// and does not open or commit a transaction.
// checkedSessionModelCatalog делегирует session-affinity resolution с enabled candidate validation
// locking. Сохраняет current configuration и tenant/session/account arguments точно, не открывает
// и не фиксирует transaction.
func checkedSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, accountRef string, configuration entity.AgentRuntimeConfiguration, overlay string) (entity.ProviderAccountCandidate, *sessionCatalogBinding, error) {
	return checkedSessionModelCatalogSnapshot(ctx, tx, organizationID, sessionID, accountRef, configuration, overlay, true)
}

// checkedSessionModelCatalogSnapshot loads the session binding, selects a current or retained
// candidate and revalidates that single account through the requested snapshot-lock mode. It
// returns the verified candidate plus retained binding only when needed; the lock flag does not
// bypass eligibility or confer actor authority.
// checkedSessionModelCatalogSnapshot читает session binding, выбирает current либо retained
// candidate и заново проверяет единственный account через requested snapshot-lock mode. Возвращает
// verified candidate и retained binding только при необходимости; lock flag не обходит eligibility
// и не выдаёт actor authority.
func checkedSessionModelCatalogSnapshot(ctx context.Context, tx pgx.Tx, organizationID, sessionID, accountRef string, configuration entity.AgentRuntimeConfiguration, overlay string, lock bool) (entity.ProviderAccountCandidate, *sessionCatalogBinding, error) {
	binding, err := readSessionModelCatalog(ctx, tx, organizationID, sessionID, accountRef)
	if err != nil {
		return entity.ProviderAccountCandidate{}, nil, err
	}
	selected, retained, err := resolveSessionModelCatalogCandidate(accountRef, configuration, binding)
	if err != nil {
		return entity.ProviderAccountCandidate{}, nil, err
	}
	verified, _, err := validateRuntimeCatalogCandidatesSnapshot(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, configuration.Model, overlay, []entity.ProviderAccountCandidate{selected}, false, lock)
	if err != nil {
		return entity.ProviderAccountCandidate{}, nil, err
	}
	return verified[0], retained, nil
}

// resolveSessionModelCatalogCandidate prefers exactly one configured account match; only when
// absent and provider/model match may it synthesize a weight-one candidate from retained session
// catalog pins. Duplicate/missing selection is Conflict. It does no I/O or live eligibility check,
// and the returned retained pointer is a shallow copy of the binding.
// resolveSessionModelCatalogCandidate предпочитает ровно один configured account match; только при
// его отсутствии и совпадении provider/model может создать weight-one candidate из retained
// session catalog pins. Duplicate/missing selection даёт Conflict. Не делает I/O или live
// eligibility check; returned retained pointer указывает на shallow copy binding.
func resolveSessionModelCatalogCandidate(accountRef string, configuration entity.AgentRuntimeConfiguration, binding sessionCatalogBinding) (entity.ProviderAccountCandidate, *sessionCatalogBinding, error) {
	selected := []entity.ProviderAccountCandidate{}
	var retained *sessionCatalogBinding
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		if candidate.AccountRef == accountRef {
			selected = append(selected, candidate)
		}
	}
	if len(selected) == 0 && binding.Provider == configuration.Provider {
		for _, model := range binding.Models {
			if model.ID == configuration.Model {
				selected = append(selected, entity.ProviderAccountCandidate{AccountRef: accountRef, Weight: 1, ProviderDefinitionKey: binding.Provider, CatalogRevision: binding.CatalogRevision, CatalogDigest: binding.CatalogDigest, DefaultReasoningEffort: model.DefaultReasoningEffort})
				retained = &binding
				break
			}
		}
	}
	if len(selected) != 1 {
		return entity.ProviderAccountCandidate{}, nil, errs.ErrConflict
	}
	return selected[0], retained, nil
}

// validateSessionRuntimeCatalog reads the locked session account and current agent configuration,
// then applies the locking session-affinity validator. Account lookup failures map to Conflict;
// later errors propagate. It validates in the caller transaction without changing the binding or
// committing.
// validateSessionRuntimeCatalog читает locked session account и current agent configuration, затем
// применяет locking session-affinity validator. Account lookup failures дают Conflict, последующие
// errors передаются далее. Проверяет в caller transaction без изменения binding либо commit.
func validateSessionRuntimeCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, agentRef string) error {
	var accountRef string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogSessionAccount, sessionID, organizationID).Scan(&accountRef); err != nil {
		return errs.ErrConflict
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, organizationID, agentRef, "")
	if err != nil {
		return err
	}
	_, _, err = checkedSessionModelCatalog(ctx, tx, organizationID, sessionID, accountRef, configuration, overlay)
	return err
}
