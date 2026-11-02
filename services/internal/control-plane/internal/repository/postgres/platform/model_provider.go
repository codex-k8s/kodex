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

// ConfigureModelProvider installs an optional immutable Git-owned descriptor before bootstrap/workers.
// A nonempty default model explicitly selects its profile for new roles; existing agent choices remain intact.
// ConfigureModelProvider назначает optional immutable Git-owned descriptor до bootstrap/workers.
// Непустая default model явно выбирает profile для новых ролей; существующий выбор не переписывается.
func (repository *Repository) ConfigureModelProvider(config runtimecontract.ModelProviderConfiguration, defaultModel string) error {
	if config.Validate() != nil {
		return runtimecontract.ErrModelProviderConfiguration
	}
	if defaultModel != "" {
		if _, found := config.Model(defaultModel); !found {
			return runtimecontract.ErrModelProviderConfiguration
		}
	}
	config.Models = append([]runtimecontract.ModelProviderModel(nil), config.Models...)
	config.AddressCIDRs = append([]string(nil), config.AddressCIDRs...)
	repository.modelProvider, repository.modelProviderDefaultModel = &config, defaultModel
	return nil
}

// defaultRuntimeProfileKey selects a configured default only when the operator explicitly supplied it.
// defaultRuntimeProfileKey выбирает configured default только по явному указанию владельца установки.
func (repository *Repository) defaultRuntimeProfileKey() string {
	if repository.modelProvider != nil && repository.modelProviderDefaultModel != "" {
		return modelProviderProfileKey(*repository.modelProvider, repository.modelProviderDefaultModel)
	}
	return defaultRuntimeKey
}

// defaultProviderDefinitionKey keeps bootstrap credential affinity aligned with the explicit default engine.
// defaultProviderDefinitionKey сохраняет affinity bootstrap credential к явно выбранному default engine.
func (repository *Repository) defaultProviderDefinitionKey() string {
	if repository.modelProvider != nil && repository.modelProviderDefaultModel != "" {
		return repository.modelProvider.DefinitionKey()
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
	if definitionKey == "openai-codex" {
		return "openai", nil, nil
	}
	if repository.modelProvider == nil || repository.modelProvider.DefinitionKey() != definitionKey {
		return "", nil, errs.ErrConflict
	}
	config := *repository.modelProvider
	if config.Validate() != nil {
		return "", nil, errs.ErrConflict
	}
	if _, found := config.Model(model); !found {
		return "", nil, errs.ErrConflict
	}
	config.AddressCIDRs = append([]string(nil), config.AddressCIDRs...)
	config.Models = append([]runtimecontract.ModelProviderModel(nil), config.Models...)
	return "opencode", &config, nil
}

// reconcileModelProvider runs inside the installation lock; it never adopts foreign definitions or changes an endpoint under existing credentials.
// Disabling the file closes the definition, while immutable history and existing UI-owned role configurations remain stored.
// reconcileModelProvider работает под installation lock, не присваивает чужие definitions и не меняет endpoint существующих credentials.
// Отключение файла закрывает definition; immutable history и UI-owned role configurations остаются сохранёнными.
func (repository *Repository) reconcileModelProvider(ctx context.Context, tx pgx.Tx) error {
	selectedKey := ""
	if repository.modelProvider != nil {
		selectedKey = repository.modelProvider.DefinitionKey()
	}
	if _, err := tx.Exec(ctx, queryModelProviderDisableOtherDefinitions, pgx.StrictNamedArgs{"definition_key": selectedKey}); err != nil {
		return bootstrapFailure("reconcile model provider eligibility", err)
	}
	if repository.modelProvider == nil {
		return nil
	}
	config := *repository.modelProvider
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
