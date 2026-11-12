package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryModelProviderRegisterDefinition preserves Git ownership and immutable gateway authority.
	// queryModelProviderRegisterDefinition сохраняет Git ownership и immutable gateway authority.
	//go:embed sql/model_provider_register_definition.sql
	queryModelProviderRegisterDefinition string
	// queryModelProviderRegisterProfile creates a source-pinned runtime profile without rewriting agent choices.
	// queryModelProviderRegisterProfile создаёт source-pinned profile без переписывания выбора агентов.
	//go:embed sql/model_provider_register_profile.sql
	queryModelProviderRegisterProfile string
	// queryModelProviderDisableOtherDefinitions closes Git-owned gateway definitions omitted by this deployment.
	// queryModelProviderDisableOtherDefinitions закрывает Git-owned definitions, отсутствующие в deployment.
	//go:embed sql/model_provider_disable_other_definitions.sql
	queryModelProviderDisableOtherDefinitions string
)

// ConfigureModelProvider constructs an explicit single-route catalog for programmatic callers; installation files use only the catalog decoder.
// ConfigureModelProvider создаёт явный каталог одного маршрута для программных callers; файлы установки разбираются только catalog decoder.
func (repository *Repository) ConfigureModelProvider(config runtimecontract.ModelProviderConfiguration, defaultModel string) error {
	return repository.ConfigureModelProviderCatalog(runtimecontract.ModelProviderCatalog{Schema: runtimecontract.ModelProviderCatalogSchema, ManagedBy: "git", SourceRevision: config.SourceRevision, DefaultProvider: config.DefinitionKey(), DefaultModel: defaultModel, Providers: []runtimecontract.ModelProviderConfiguration{config}})
}

// ConfigureModelProviderCatalog installs a detached validated immutable catalog before bootstrap and workers, without changing existing agent selections.
// ConfigureModelProviderCatalog назначает проверенный immutable каталог до bootstrap и workers без изменения выбора существующих агентов.
func (repository *Repository) ConfigureModelProviderCatalog(catalog runtimecontract.ModelProviderCatalog) error {
	cloned, err := catalog.Clone()
	if err != nil {
		return err
	}
	repository.modelProviderCatalog = &cloned
	return nil
}

// configuredModelProvider resolves one exact active definition with no default fallback.
// configuredModelProvider разрешает точный активный definition без default fallback.
func (repository *Repository) configuredModelProvider(key string) *runtimecontract.ModelProviderConfiguration {
	if repository.modelProviderCatalog == nil {
		return nil
	}
	provider, _ := repository.modelProviderCatalog.Provider(key)
	return provider
}

// defaultModelProvider resolves only the explicit installation default; it does not choose a route from model spelling.
// defaultModelProvider разрешает только явный default установки, не выбирая маршрут по имени модели.
func (repository *Repository) defaultModelProvider() *runtimecontract.ModelProviderConfiguration {
	if repository.modelProviderCatalog == nil {
		return nil
	}
	return repository.configuredModelProvider(repository.modelProviderCatalog.DefaultProvider)
}

// modelProviderDefinitionKeys returns a detached closed installation set for task eligibility and expiry SQL.
// modelProviderDefinitionKeys возвращает отдельный закрытый набор установки для eligibility и expiry SQL.
func (repository *Repository) modelProviderDefinitionKeys() []string {
	keys := []string{}
	if repository.modelProviderCatalog != nil {
		for _, provider := range repository.modelProviderCatalog.Providers {
			keys = append(keys, provider.DefinitionKey())
		}
	}
	return keys
}

// defaultRuntimeProfileKey selects a configured default only when the operator explicitly supplied it.
// defaultRuntimeProfileKey выбирает configured default только по явному указанию владельца установки.
func (repository *Repository) defaultRuntimeProfileKey() string {
	if provider := repository.defaultModelProvider(); provider != nil {
		return modelProviderProfileKey(*provider, repository.modelProviderCatalog.DefaultModel)
	}
	return defaultRuntimeKey
}

// defaultProviderDefinitionKey keeps bootstrap credential affinity aligned with the explicit installation default.
// defaultProviderDefinitionKey сохраняет affinity bootstrap credential к явному default установки.
func (repository *Repository) defaultProviderDefinitionKey() string {
	if provider := repository.defaultModelProvider(); provider != nil {
		return provider.DefinitionKey()
	}
	return repository.defaultRuntimeProvider
}

// modelProviderProfileKey derives an immutable bounded locator from complete configuration and model pins.
// modelProviderProfileKey выводит immutable bounded locator из полной конфигурации и model pins.
func modelProviderProfileKey(config runtimecontract.ModelProviderConfiguration, model string) string {
	digest, err := config.Digest()
	if err != nil {
		return ""
	}
	return "opencode-" + digestBytes([]byte(digest), []byte(model))[:32]
}

// runtimeProviderSelection resolves only a registered engine and exact allowlisted model under the active deployment configuration.
// It returns a descriptor copy for the immutable snapshot; callers do not obtain credential values.
// runtimeProviderSelection разрешает только registered engine и exact allowlisted model действующей конфигурации.
// Возвращает копию descriptor для immutable snapshot без credential values.
func (repository *Repository) runtimeProviderSelection(definitionKey, model string) (string, *runtimecontract.ModelProviderConfiguration, error) {
	selected := repository.configuredModelProvider(definitionKey)
	if selected == nil {
		return "", nil, errs.ErrConflict
	}
	config := *selected
	if config.Validate() != nil {
		return "", nil, errs.ErrConflict
	}
	if _, found := config.Model(model); !found {
		return "", nil, errs.ErrConflict
	}
	if _, err := config.ResolveModelCache(model); err != nil {
		return "", nil, errs.ErrConflict
	}
	cloned, err := config.Clone()
	if err != nil {
		return "", nil, errs.ErrConflict
	}
	return "opencode", &cloned, nil
}

// reconcileModelProvider runs inside the installation lock; it never adopts foreign definitions or changes an endpoint under existing credentials.
// Disabling the file closes the definition, while immutable history and existing UI-owned role configurations remain stored.
// reconcileModelProvider работает под installation lock, не присваивает чужие definitions и не меняет endpoint существующих credentials.
// Отключение файла закрывает definition; immutable history и UI-owned role configurations остаются сохранёнными.
func (repository *Repository) reconcileModelProvider(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, queryModelProviderDisableOtherDefinitions, pgx.StrictNamedArgs{"definition_keys": repository.modelProviderDefinitionKeys()}); err != nil {
		return bootstrapFailure("reconcile model provider eligibility", err)
	}
	if repository.modelProviderCatalog == nil {
		return nil
	}
	for _, config := range repository.modelProviderCatalog.Providers {
		if err := repository.registerModelProvider(ctx, tx, config); err != nil {
			return err
		}
	}
	return nil
}

// registerModelProvider materializes one exact Git-owned definition and immutable model profiles inside the installation transaction.
// registerModelProvider материализует один Git-owned definition и immutable model profiles в транзакции установки.
func (repository *Repository) registerModelProvider(ctx context.Context, tx pgx.Tx, config runtimecontract.ModelProviderConfiguration) error {
	selectedKey := config.DefinitionKey()
	digest, err := config.Digest()
	if err != nil {
		return err
	}
	capabilities, err := json.Marshal(map[string]any{"sessionAffinity": true, "deviceAuthorization": false, "models": true,
		"managedBy": "git", "runtimeEngine": config.Engine, "toolMode": config.ToolMode, "sourceRevision": config.SourceRevision,
		"configurationDigest": digest, "modelProvider": config})
	if err != nil {
		return errs.ErrInvalid
	}
	var recorded string
	err = tx.QueryRow(ctx, queryModelProviderRegisterDefinition, pgx.StrictNamedArgs{
		"definition_key": selectedKey, "capabilities": capabilities, "base_url": config.BaseURL,
	}).Scan(&recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil || recorded != selectedKey {
		return bootstrapFailure("register model provider definition", err)
	}
	limits, _ := json.Marshal(map[string]any{"cpu": "1000m", "memory": "2Gi", "maxConcurrentTurns": 1})
	for _, model := range config.Models {
		key := modelProviderProfileKey(config, model.ID)
		err := tx.QueryRow(ctx, queryModelProviderRegisterProfile, pgx.StrictNamedArgs{
			"profile_key": key, "definition_key": selectedKey, "model": model.ID,
			"runtime_revision": "opencode-v1-" + digest[:24], "resource_limits": limits,
		}).Scan(&recorded)
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		if err != nil || recorded != key {
			return bootstrapFailure("register model provider runtime profile", err)
		}
	}
	return nil
}
