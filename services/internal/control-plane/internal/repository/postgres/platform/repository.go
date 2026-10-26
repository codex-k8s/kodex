// Package platform implements PostgreSQL adapters for control-plane owner state and transactions.
// Package platform реализует PostgreSQL adapters для owner state и транзакций control-plane.
package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// defaultRuntimeKey identifies the built-in runtime profile seeded and selected for the initial
	// system assistant.
	// defaultRuntimeKey обозначает встроенный runtime profile, создаваемый и выбираемый для начального
	// системного ассистента.
	defaultRuntimeKey = "builtin-safe-runtime"
	// maximumArtifactBytes aliases the domain repository artifact-byte bound so adapter paths use the
	// same storage limit.
	// maximumArtifactBytes ссылается на доменный предел байтов артефакта, чтобы пути adapter
	// использовали один лимит хранения.
	maximumArtifactBytes = platformrepo.MaximumArtifactBytes
)

// Repository implements owner persistence using the supplied pool and object store with configured
// scanners, catalogs and credential descriptors; it does not own pool shutdown.
// Repository реализует хранение владельца через переданные pool и object store с настроенными
// scanners, каталогами и credential descriptors; закрытие pool ему не принадлежит.
type Repository struct {
	pool                            *pgxpool.Pool
	trustedCluster                  bool
	defaultRuntimeProvider          string
	defaultRuntimeModel             string
	providerCredential              ProviderCredentialConfig
	roleImages                      RoleImageConfig
	objects                         objectstorage.Store
	projectPurgeStorageIdentity     string
	skillScanner                    skillpolicy.Scanner
	artifactScanner                 artifactpolicy.Scanner
	integrationDefinitions          map[string]integrationpackage.Package
	roleImageCatalogResolver        func(entity.RoleEnvironmentSelection) (entity.RoleImageRecipeInput, error)
	roleImageRecommendedSelection   func() (entity.RoleEnvironmentSelection, error)
	roleImageCatalogEntries         func() []roleimageservice.Environment
	roleImageBootstrapCopySelection func(entity.RoleImageRecipeInput) (entity.RoleEnvironmentSelection, error)
	runtimeSecretNamespace          string
	runtimeSecretStagingNamespace   string
}

// ProviderCredentialConfig contains only immutable Kubernetes Secret identity, resource version
// and content digest metadata, never the credential value.
// ProviderCredentialConfig содержит только метаданные неизменяемых identity Kubernetes Secret,
// resource version и content digest без значения credential.
type ProviderCredentialConfig struct {
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// RoleImageConfig binds role-image policy, runtime contract, claim lifetimes, repositories and
// default image to the signing key used for fenced supply-chain tokens.
// RoleImageConfig связывает role-image policy, runtime contract, сроки claims, repositories и
// default image с ключом подписания fenced supply-chain tokens.
type RoleImageConfig struct {
	PolicyRevision, RoleRuntimeContractRevision uint64
	PolicySHA256, RoleRuntimeContractSHA256     string
	BuildLeaseDuration, AdmissionClaimTTL       time.Duration
	PromotionClaimTTL                           time.Duration
	MaximumAttempts                             uint32
	StagingRepository, PromotedRepository       string
	DefaultImageReference, DefaultImageDigest   string
	LeaseSigningKey                             []byte
}

// New requires pool, object store, the supported provider and a model, loads shipped definitions
// and sets default namespaces without connecting, bootstrapping or starting workers.
// New требует pool, object store, поддерживаемого провайдера и модель, загружает shipped
// definitions и назначает default namespaces без подключения, bootstrap либо запуска workers.
func New(pool *pgxpool.Pool, defaultRuntimeProvider, defaultRuntimeModel string, objects objectstorage.Store) (*Repository, error) {
	if pool == nil || defaultRuntimeProvider != "openai-codex" || defaultRuntimeModel == "" || objects == nil {
		return nil, errors.New("control-plane repository dependencies are required")
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return nil, errors.New("load shipped integration definitions")
	}
	return &Repository{
		pool: pool, defaultRuntimeProvider: defaultRuntimeProvider, defaultRuntimeModel: defaultRuntimeModel,
		objects: objects, integrationDefinitions: definitions, runtimeSecretNamespace: "kodex-runtime", runtimeSecretStagingNamespace: "kodex-secret-drafts",
	}, nil
}

// ConfigureRuntimeSecrets requires the shared DNS-label grammar before storing the runtime Secret
// namespace for subsequent owner operations.
// ConfigureRuntimeSecrets требует общую грамматику DNS-label до сохранения namespace runtime
// Secret для последующих операций владельца.
func (repository *Repository) ConfigureRuntimeSecrets(namespace string) error {
	if !validDNSLabel(namespace) {
		return errors.New("runtime secret namespace is invalid")
	}
	repository.runtimeSecretNamespace = namespace
	return nil
}

// ConfigureRuntimeSecretStaging requires a valid DNS label distinct from the current runtime
// Secret namespace before storing draft staging namespace.
// ConfigureRuntimeSecretStaging требует допустимую DNS-label, отличную от текущего runtime Secret
// namespace, до сохранения staging namespace черновиков.
func (repository *Repository) ConfigureRuntimeSecretStaging(namespace string) error {
	if !validDNSLabel(namespace) || namespace == repository.runtimeSecretNamespace {
		return errors.New("runtime secret staging namespace is invalid")
	}
	repository.runtimeSecretStagingNamespace = namespace
	return nil
}

// ConfigureRoleImages checks configured lengths, lifetimes, attempt bounds and default image
// reference shape, derives its digest and copies the signing key before storing configuration.
// ConfigureRoleImages проверяет заданные длины, сроки, предел попыток и форму default image
// reference, выделяет её digest и копирует signing key до сохранения конфигурации.
func (repository *Repository) ConfigureRoleImages(config RoleImageConfig) error {
	if config.PolicyRevision == 0 || config.RoleRuntimeContractRevision == 0 ||
		len(config.PolicySHA256) != 64 || len(config.RoleRuntimeContractSHA256) != 64 ||
		config.BuildLeaseDuration < 30*time.Second || config.AdmissionClaimTTL < time.Minute ||
		config.PromotionClaimTTL < time.Minute || config.MaximumAttempts < 1 || config.MaximumAttempts > 10 ||
		config.StagingRepository == "" || config.PromotedRepository == "" || len(config.LeaseSigningKey) < 32 {
		return errors.New("role image configuration is invalid")
	}
	separator := strings.LastIndex(config.DefaultImageReference, "@")
	if separator < 1 || separator == len(config.DefaultImageReference)-1 ||
		!strings.HasPrefix(config.DefaultImageReference[separator+1:], "sha256:") ||
		len(config.DefaultImageReference[separator+1:]) != 71 {
		return errors.New("default role image reference is invalid")
	}
	config.DefaultImageDigest = config.DefaultImageReference[separator+1:]
	config.LeaseSigningKey = append([]byte(nil), config.LeaseSigningKey...)
	repository.roleImages = config
	return nil
}

// ConfigureProviderCredential allows an entirely empty optional configuration, otherwise validates
// Secret name, UUID, bounded resource version and lowercase SHA-256 before replacing stored
// metadata.
// ConfigureProviderCredential допускает полностью пустую необязательную конфигурацию, иначе
// проверяет имя Secret, UUID, ограниченный resource version и строчный SHA-256 до замены
// метаданных.
func (repository *Repository) ConfigureProviderCredential(config ProviderCredentialConfig) error {
	if config == (ProviderCredentialConfig{}) {
		repository.providerCredential = config
		return nil
	}
	if !validDNSLabel(config.SecretName) || uuid.Validate(config.SecretUID) != nil ||
		config.SecretResourceVersion == "" || len(config.SecretResourceVersion) > 128 ||
		!exactSHA256(config.ContentSHA256) {
		return errors.New("provider credential metadata is invalid")
	}
	repository.providerCredential = config
	return nil
}

// validDNSLabel delegates provider Secret name syntax to the shared resource-label parser while
// leaving exact credential metadata, scope and content verification with the repository owner.
// validDNSLabel делегирует синтаксис имени Secret провайдера общему разбору меток ресурсов,
// оставляя точную проверку метаданных реквизитов, области и содержимого владельцу хранилища.
func validDNSLabel(value string) bool {
	return runtimecontract.ValidDNSLabel(value)
}

// Ready checks installation schema version and secret-draft/purge-recovery schema predicates
// through the supplied pool, returning fixed diagnostics without probing downstream workers.
// Ready проверяет версию схемы установки и schema predicates secret-draft и purge-recovery через
// переданный pool, возвращая фиксированную диагностику без проверки downstream workers.
func (repository *Repository) Ready(ctx context.Context) error {
	var schemaVersion int
	if err := repository.pool.QueryRow(ctx, queryRepositoryReadySelectInstallationSingleton).Scan(&schemaVersion); err != nil {
		return errors.New("control-plane schema is unavailable")
	}
	if schemaVersion != 1 {
		return errors.New("control-plane schema version is unsupported")
	}
	var draftsReady bool
	if repository.pool.QueryRow(ctx, querySecretDraftReadiness).Scan(&draftsReady) != nil || !draftsReady {
		return errors.New("runtime secret draft schema is unavailable")
	}
	var recoveryReady bool
	if repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryReadiness).Scan(&recoveryReady) != nil || !recoveryReady {
		return errors.New("project purge recovery schema is unavailable")
	}
	return nil
}

// Bootstrap uses one serializable transaction to initialize owner state or reconcile configured
// credential, core prompt, environment and shipped definitions; commit and deferred rollback
// remain caller-context operations.
// Bootstrap использует одну serializable-транзакцию для начального owner state либо сверки
// credential, core prompt, среды и shipped definitions; commit и deferred rollback остаются
// операциями с caller context.
func (repository *Repository) Bootstrap(ctx context.Context) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return bootstrapFailure("begin bootstrap transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var bootstrappedAt *time.Time
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapSelectInstallationSingleton).Scan(&bootstrappedAt); err != nil {
		return bootstrapFailure("lock installation bootstrap", err)
	}
	if bootstrappedAt != nil {
		if repository.providerCredential.SecretName != "" {
			if err := repository.reconcileProviderCredential(ctx, tx); err != nil {
				return err
			}
		}
		if err := repository.reconcileSystemAssistantCorePrompt(
			ctx,
			tx,
			systemassistant.CorePromptRevision,
			systemassistant.CorePrompt(),
		); err != nil {
			return err
		}
		if err := repository.reconcileSystemAssistantRuntimeEnvironment(ctx, tx); err != nil {
			return err
		}
		if err := repository.reconcileIntegrationDefinitions(ctx, tx); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return bootstrapFailure("commit bootstrap transaction", err)
		}
		return nil
	}
	organizationRef, err := newRef("org")
	if err != nil {
		return err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertOrganizationsRefName, organizationRef).Scan(&organizationID); err != nil {
		return bootstrapFailure("create bootstrap organization", err)
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertOwnerClaimContractsOrganizationIdStableKeyState, organizationID); err != nil {
		return bootstrapFailure("create owner claim contract", err)
	}
	systemDigest := sha256.Sum256([]byte("kodex-system-subject"))
	var systemSubjectID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName,
		organizationID, hex.EncodeToString(systemDigest[:])).Scan(&systemSubjectID); err != nil {
		return bootstrapFailure("create system subject", err)
	}
	if err := repository.bootstrapAccess(ctx, tx, organizationID, organizationRef, systemSubjectID); err != nil {
		return err
	}
	capabilities := []struct{ key, name, description, risk string }{
		{"platform.project.manage", "i18n:CAPABILITY_PROJECT_MANAGE_NAME", "i18n:CAPABILITY_PROJECT_MANAGE_DESCRIPTION", "LOW"},
		{"platform.agent.manage", "i18n:CAPABILITY_AGENT_MANAGE_NAME", "i18n:CAPABILITY_AGENT_MANAGE_DESCRIPTION", "MEDIUM"},
		{"platform.run.launch", "i18n:CAPABILITY_RUN_LAUNCH_NAME", "i18n:CAPABILITY_RUN_LAUNCH_DESCRIPTION", "MEDIUM"},
		{"platform.run.delegate", "i18n:CAPABILITY_RUN_DELEGATE_NAME", "i18n:CAPABILITY_RUN_DELEGATE_DESCRIPTION", "MEDIUM"},
		{"platform.gate.resolve", "i18n:CAPABILITY_GATE_RESOLVE_NAME", "i18n:CAPABILITY_GATE_RESOLVE_DESCRIPTION", "HIGH"},
		{"platform.artifact.manage", "i18n:CAPABILITY_ARTIFACT_MANAGE_NAME", "i18n:CAPABILITY_ARTIFACT_MANAGE_DESCRIPTION", "MEDIUM"},
		{"platform.schedule.manage", "i18n:CAPABILITY_SCHEDULE_MANAGE_NAME", "i18n:CAPABILITY_SCHEDULE_MANAGE_DESCRIPTION", "HIGH"},
		{"platform.integration.grant", "i18n:CAPABILITY_INTEGRATION_GRANT_NAME", "i18n:CAPABILITY_INTEGRATION_GRANT_DESCRIPTION", "HIGH"},
	}
	for _, capability := range capabilities {
		if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertPlatformCapabilitiesStableKeyNameDescription,
			capability.key, capability.name, capability.description, capability.risk); err != nil {
			return bootstrapFailure("seed platform capability", err)
		}
	}
	limits, _ := json.Marshal(map[string]any{"cpu": "1000m", "memory": "2Gi", "maxConcurrentTurns": 1})
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertRuntimeProfilesStableKeyProviderRuntimeRevision, defaultRuntimeKey, repository.defaultRuntimeProvider, repository.defaultRuntimeModel, limits); err != nil {
		return bootstrapFailure("seed runtime profile", err)
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertProviderDefinition); err != nil {
		return bootstrapFailure("seed provider definition", err)
	}
	var providerAccountID string
	if repository.providerCredential.SecretName != "" {
		providerAccountRef, err := newRef("pacc")
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertProviderAccount,
			providerAccountRef, organizationID, systemSubjectID).Scan(&providerAccountID); err != nil {
			return bootstrapFailure("seed provider account", err)
		}
		providerCredentialRef, err := newRef("pcr")
		if err != nil {
			return err
		}
		var providerCredentialID string
		if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertProviderCredentialRevision,
			providerCredentialRef, organizationID, providerAccountID,
			repository.providerCredential.SecretName, repository.providerCredential.SecretUID,
			repository.providerCredential.SecretResourceVersion, repository.providerCredential.ContentSHA256,
		).Scan(&providerCredentialID); err != nil {
			return bootstrapFailure("seed provider credential revision", err)
		}
		if _, err := tx.Exec(ctx, queryRepositoryBootstrapActivateProviderCredentialRevision,
			providerAccountID, providerCredentialID); err != nil {
			return bootstrapFailure("activate provider credential revision", err)
		}
	}
	roleRef, err := newRef("role")
	if err != nil {
		return err
	}
	var systemRoleID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertRoleDefinitionsRefStableKeyName,
		roleRef, organizationID, systemSubjectID).Scan(&systemRoleID); err != nil {
		return bootstrapFailure("seed system assistant role definition", err)
	}
	if err := repository.reconcileIntegrationDefinitions(ctx, tx); err != nil {
		return err
	}
	agentRef, err := newRef("agt")
	if err != nil {
		return err
	}
	var agentID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertAgentsRefSystemKeyPurpose,
		agentRef, organizationID, systemRoleID, defaultRuntimeKey).Scan(&agentID); err != nil {
		return bootstrapFailure("create system assistant", err)
	}
	defaultRuntime, err := resolveEnabledRuntime(ctx, tx, defaultRuntimeKey)
	if err != nil {
		return bootstrapFailure("resolve system assistant runtime", err)
	}
	if err := repository.bootstrapAgentRuntime(ctx, tx, organizationID, agentID, "", defaultRuntime, systemSubjectID); err != nil {
		return bootstrapFailure("create system assistant runtime configuration", err)
	}
	promptRef, err := newRef("ins")
	if err != nil {
		return err
	}
	corePrompt := systemassistant.CorePrompt()
	promptDigest := sha256.Sum256([]byte(corePrompt))
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertInstructionVersionsRefAgentIdState,
		promptRef, organizationID, agentID, corePrompt, hex.EncodeToString(promptDigest[:])); err != nil {
		return bootstrapFailure("create system assistant core prompt", err)
	}
	if _, _, err := assignInstructionBinding(ctx, tx, organizationID, agentID, promptRef); err != nil {
		return err
	}
	var systemSessionRef string
	if providerAccountID != "" {
		systemSessionRef, err = newRef("ses")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertSessionsRefTargetTypeState,
			systemSessionRef, organizationID, providerAccountID, systemSubjectID); err != nil {
			return bootstrapFailure("create system assistant warm session", err)
		}
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertAssistantRuntimeOrganizationIdStableKeyCorePromptRevision,
		organizationID, agentID, promptRef, systemassistant.CorePromptRevision, systemSessionRef, limits); err != nil {
		return bootstrapFailure("create system assistant runtime contract", err)
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapUpdateInstallationBootstrappedAt); err != nil {
		return bootstrapFailure("complete bootstrap", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return bootstrapFailure("commit bootstrap transaction", err)
	}
	return nil
}

// reconcileIntegrationDefinitions reconciles sorted shipped packages with forward version/digest
// checks, projects capability schemas and configuration fields and disables absent packages within
// the supplied transaction.
// reconcileIntegrationDefinitions сверяет отсортированные shipped packages с forward
// version/digest checks, преобразует capability schemas и configuration fields и отключает
// отсутствующие packages в переданной транзакции.
func (repository *Repository) reconcileIntegrationDefinitions(ctx context.Context, tx pgx.Tx) error {
	keys := make([]string, 0, len(repository.integrationDefinitions))
	for _, definition := range integrationpackage.Sorted(repository.integrationDefinitions) {
		var storedVersion, storedDigest string
		err := tx.QueryRow(ctx, queryRepositoryReconcileIntegrationDefinition, definition.Metadata.Key).Scan(&storedVersion, &storedDigest)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return bootstrapFailure("read integration definition revision", err)
		}
		if err == nil {
			comparison, compareErr := compareIntegrationDefinitionVersions(storedVersion, definition.Metadata.Version)
			if compareErr != nil || comparison > 0 || comparison == 0 && storedDigest != definition.Digest {
				return errors.New("integration definition revision rollback or digest mismatch")
			}
		}
		capabilities := make([]entity.IntegrationCapability, 0, len(definition.Spec.Capabilities))
		for _, capability := range definition.Spec.Capabilities {
			inputSchema, schemaErr := capability.InputSchema()
			inputSchemaDigest, digestErr := capability.InputSchemaDigest()
			if schemaErr != nil || digestErr != nil {
				return errors.New("encode integration capability input schema")
			}
			capabilities = append(capabilities, entity.IntegrationCapability{
				Key: capability.Key, Name: capability.Name, Description: capability.Description,
				Operation: capability.Operation, Risk: capability.Risk,
				ApprovalPolicy: capability.ApprovalPolicy, ResourceKind: capability.ResourceScope.Kind,
				AllowedApprovalPolicies: append([]string{}, capability.AllowedApprovalPolicies...),
				InputFields:             integrationConfigurationFields(capability.InputFields),
				InputSchema:             string(inputSchema), InputSchemaSHA256: inputSchemaDigest,
			})
		}
		fields := integrationConfigurationFields(definition.Spec.ConfigurationFields)
		capabilityJSON, capabilityErr := json.Marshal(capabilities)
		configurationJSON, configurationErr := json.Marshal(fields)
		if capabilityErr != nil || configurationErr != nil {
			return errors.New("encode integration definition")
		}
		credentialKey := ""
		if definition.Spec.Credential != nil {
			credentialKey = definition.Spec.Credential.SecretKey
		}
		if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertIntegrationDefinitionsStableKeyDescriptionCapabilities,
			definition.Metadata.Key, definition.Spec.Name, definition.Spec.Description, definition.Spec.Category,
			capabilityJSON, configurationJSON, definition.APIVersion, definition.Metadata.Version,
			definition.Metadata.Origin, definition.Digest, definition.Spec.Adapter, credentialKey,
			definition.Spec.AdapterOwner, definition.Spec.ExecutionRoute, definition.Spec.Readiness,
		); err != nil {
			return bootstrapFailure("reconcile integration definition", err)
		}
		keys = append(keys, definition.Metadata.Key)
	}
	if _, err := tx.Exec(ctx, queryRepositoryDisableUnshippedIntegrationDefinitions, keys); err != nil {
		return bootstrapFailure("disable unshipped integration definitions", err)
	}
	return nil
}

// integrationConfigurationFields copies field constraints with URL formatting or the TEXT default,
// adding integer minimum and nonzero maximum pointers while preserving an independent
// allowed-value slice.
// integrationConfigurationFields копирует ограничения полей с URL formatting либо default TEXT,
// добавляя integer minimum и ненулевой maximum pointers и сохраняя независимый срез allowed
// values.
func integrationConfigurationFields(fields []integrationpackage.Field) []entity.IntegrationConfigurationField {
	result := make([]entity.IntegrationConfigurationField, 0, len(fields))
	for _, field := range fields {
		valueType := "TEXT"
		if field.Format == "HTTPS_ORIGIN" || field.Format == "HTTPS_URL" {
			valueType = "URL"
		}
		item := entity.IntegrationConfigurationField{
			Key: field.Key, Label: field.Key, ValueType: valueType, Format: field.Format,
			AllowedValues: append([]string(nil), field.AllowedValues...), MaximumLength: int32(field.MaximumLength), Required: field.Required,
		}
		if field.Type == "INTEGER" {
			minimum, maximum := field.Minimum, field.Maximum
			item.Minimum = &minimum
			if maximum != 0 {
				item.Maximum = &maximum
			}
		}
		result = append(result, item)
	}
	return result
}

// compareIntegrationDefinitionVersions parses exactly three decimal uint32 components with
// positive major and compares them numerically, rejecting malformed input without full
// semantic-version parsing.
// compareIntegrationDefinitionVersions разбирает ровно три десятичных uint32-компонента с
// положительным major и сравнивает их численно, отклоняя неверный ввод без полного
// semantic-version parsing.
func compareIntegrationDefinitionVersions(left, right string) (int, error) {
	parse := func(raw string) ([3]uint64, error) {
		var result [3]uint64
		parts := strings.Split(raw, ".")
		if len(parts) != len(result) {
			return result, errors.New("integration definition version is invalid")
		}
		for index, part := range parts {
			value, err := strconv.ParseUint(part, 10, 32)
			if err != nil || index == 0 && value == 0 {
				return result, errors.New("integration definition version is invalid")
			}
			result[index] = value
		}
		return result, nil
	}
	parsedLeft, err := parse(left)
	if err != nil {
		return 0, err
	}
	parsedRight, err := parse(right)
	if err != nil {
		return 0, err
	}
	for index := range parsedLeft {
		if parsedLeft[index] < parsedRight[index] {
			return -1, nil
		}
		if parsedLeft[index] > parsedRight[index] {
			return 1, nil
		}
	}
	return 0, nil
}

// reconcileProviderCredential accepts current or exact historical bootstrap pins without rollback,
// otherwise permits same-name/content metadata renewal as a new revision and schedules
// old-credential cleanup atomically.
// reconcileProviderCredential принимает текущие либо точные historical bootstrap pins без отката,
// иначе допускает обновление метаданных того же name/content новой ревизией и атомарно планирует
// очистку прежней credential.
func (repository *Repository) reconcileProviderCredential(ctx context.Context, tx pgx.Tx) error {
	var organizationID, accountID, currentCredentialID, secretName, secretUID, secretResourceVersion, contentSHA256 string
	var revisionNumber int64
	if err := tx.QueryRow(ctx, queryProviderCredentialGetCurrentForReconcile).Scan(
		&organizationID,
		&accountID,
		&currentCredentialID,
		&revisionNumber,
		&secretName,
		&secretUID,
		&secretResourceVersion,
		&contentSHA256,
	); err != nil {
		return bootstrapFailure("read current provider credential revision", err)
	}
	configured := repository.providerCredential
	if secretName == configured.SecretName && secretUID == configured.SecretUID &&
		secretResourceVersion == configured.SecretResourceVersion && contentSHA256 == configured.ContentSHA256 {
		return nil
	}
	var historical bool
	if err := tx.QueryRow(ctx, queryProviderCredentialHistoricalBootstrapPin, pgx.StrictNamedArgs{
		"organization_id":         organizationID,
		"provider_account_id":     accountID,
		"secret_name":             configured.SecretName,
		"secret_uid":              configured.SecretUID,
		"secret_resource_version": configured.SecretResourceVersion,
		"content_sha256":          configured.ContentSHA256,
	}).Scan(&historical); err != nil {
		return bootstrapFailure("read historical provider credential bootstrap pin", err)
	}
	// Прежний exact pin подтверждает bootstrap, но не переключает current назад.
	if historical {
		return nil
	}
	if secretName != configured.SecretName || contentSHA256 != configured.ContentSHA256 {
		return errors.New("provider credential rotation requires an explicit revision")
	}
	credentialRef, err := newRef("pcr")
	if err != nil {
		return err
	}
	var nextCredentialID string
	if err := tx.QueryRow(ctx, queryProviderCredentialInsertReconciledRevision, pgx.StrictNamedArgs{
		"ref":                     credentialRef,
		"organization_id":         organizationID,
		"provider_account_id":     accountID,
		"revision_number":         revisionNumber + 1,
		"secret_name":             configured.SecretName,
		"secret_uid":              configured.SecretUID,
		"secret_resource_version": configured.SecretResourceVersion,
		"content_sha256":          configured.ContentSHA256,
	}).Scan(&nextCredentialID); err != nil {
		return bootstrapFailure("create reconciled provider credential revision", err)
	}
	tag, err := tx.Exec(ctx, queryProviderAccountActivateReconciledCredential, pgx.StrictNamedArgs{
		"provider_account_id":            accountID,
		"current_credential_revision_id": currentCredentialID,
		"next_credential_revision_id":    nextCredentialID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return bootstrapFailure("activate reconciled provider credential revision", err)
	}
	if err := repository.scheduleProviderCredentialCleanup(ctx, tx, organizationID, accountID,
		currentCredentialID, time.Now().UTC().Add(providerCredentialCleanupRetention)); err != nil {
		return bootstrapFailure("schedule reconciled provider credential cleanup", err)
	}
	return nil
}

// reconcileSystemAssistantCorePrompt rejects revision rollback and same-version integrity
// mismatch, otherwise inserts and binds the new core prompt, conditionally activates it and
// requires one audit row in the supplied transaction.
// reconcileSystemAssistantCorePrompt отклоняет rollback ревизии и нарушение целостности той же
// версии, иначе вставляет и привязывает новый core prompt, условно активирует его и требует одну
// audit row в переданной транзакции.
func (repository *Repository) reconcileSystemAssistantCorePrompt(
	ctx context.Context,
	tx pgx.Tx,
	expectedRevision string,
	expectedPrompt string,
) error {
	var organizationID, agentID, currentRevision, currentDigest, currentPrompt string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapSelectAssistantCorePrompt).Scan(
		&organizationID,
		&agentID,
		&currentRevision,
		&currentDigest,
		&currentPrompt,
	); err != nil {
		return bootstrapFailure("read system assistant core prompt", err)
	}
	expectedDigest := sha256.Sum256([]byte(expectedPrompt))
	expectedDigestText := hex.EncodeToString(expectedDigest[:])
	currentNumber, currentValid := systemAssistantCoreRevisionNumber(currentRevision)
	expectedNumber, expectedValid := systemAssistantCoreRevisionNumber(expectedRevision)
	if !currentValid || !expectedValid {
		return errors.New("system assistant core prompt revision is invalid")
	}
	if currentNumber > expectedNumber {
		return errors.New("system assistant core prompt rollback is forbidden")
	}
	if currentNumber == expectedNumber {
		if currentRevision != expectedRevision || currentDigest != expectedDigestText || currentPrompt != expectedPrompt {
			return errors.New("system assistant core prompt integrity mismatch")
		}
		return nil
	}
	promptRef, err := newRef("ins")
	if err != nil {
		return err
	}
	var insertedRef string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertAssistantCorePromptVersion, pgx.StrictNamedArgs{
		"prompt_ref":      promptRef,
		"organization_id": organizationID,
		"agent_id":        agentID,
		"content":         expectedPrompt,
		"digest":          expectedDigestText,
	}).Scan(&insertedRef); err != nil || insertedRef != promptRef {
		return bootstrapFailure("create system assistant core prompt revision", err)
	}
	if _, _, err := assignInstructionBinding(ctx, tx, organizationID, agentID, promptRef); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, queryRepositoryBootstrapUpdateAssistantCorePrompt, pgx.StrictNamedArgs{
		"prompt_ref":       promptRef,
		"next_revision":    expectedRevision,
		"organization_id":  organizationID,
		"agent_id":         agentID,
		"current_revision": currentRevision,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return bootstrapFailure("activate system assistant core prompt revision", err)
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return err
	}
	tag, err = tx.Exec(ctx, queryRepositoryBootstrapInsertAssistantCorePromptAudit, pgx.StrictNamedArgs{
		"audit_ref":       auditRef,
		"organization_id": organizationID,
		"agent_id":        agentID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return bootstrapFailure("audit system assistant core prompt revision", err)
	}
	return nil
}

// reconcileSystemAssistantRuntimeEnvironment preserves user-published images, validates stored
// bootstrap environment/policy and creates an audited version only when the expected
// image/configuration digests differ.
// reconcileSystemAssistantRuntimeEnvironment сохраняет пользовательские опубликованные образы,
// проверяет сохранённые bootstrap environment и policy и создаёт аудируемую версию только при
// несовпадении ожидаемых image/configuration digests.
func (repository *Repository) reconcileSystemAssistantRuntimeEnvironment(ctx context.Context, tx pgx.Tx) error {
	var organizationID, agentID, environmentID, currentVersionID, createdBy, imageArtifactID, currentCoreDigest, currentDigest string
	var platformBootstrapImage bool
	var currentVersion int64
	var rawValues, rawSecrets, rawTools []byte
	var rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte
	var resourcesDigest, volumesDigest, networkDigest, rbacDigest string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapSelectAssistantRuntimeEnvironment).Scan(
		&organizationID, &agentID, &environmentID, &currentVersionID, &createdBy, &imageArtifactID, &platformBootstrapImage, &currentVersion,
		&rawValues, &rawSecrets, &rawTools, &currentCoreDigest, &currentDigest,
		&rawResources, &rawVolumes, &rawNetwork, &rawKubernetesAccess,
		&resourcesDigest, &volumesDigest, &networkDigest, &rbacDigest,
	); err != nil {
		return bootstrapFailure("read system assistant runtime environment", err)
	}
	// Опубликованный exact artifact является пользовательской конфигурацией.
	// Bootstrap не перепривязывает и не переписывает такую immutable версию.
	if imageArtifactID != "" && !platformBootstrapImage {
		return nil
	}
	var values []runtimecontract.RuntimeEnvironmentValue
	var secrets []runtimecontract.RuntimeSecretProjection
	if err := decodeStoredRuntimeEnvironment(rawValues, rawSecrets, &values, &secrets); err != nil {
		return bootstrapFailure("decode system assistant runtime environment", err)
	}
	var tools []entity.RuntimeEnvironmentTool
	if err := decodeStrict(rawTools, &tools); err != nil {
		return bootstrapFailure("decode system assistant runtime tools", err)
	}
	policy, err := decodeRuntimeEnvironmentPolicy(rawResources, rawVolumes, rawNetwork, rawKubernetesAccess,
		resourcesDigest, volumesDigest, networkDigest, rbacDigest)
	if err != nil || policy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone {
		return bootstrapFailure("verify system assistant runtime policy", err)
	}
	imageArtifactID, image, _, err := repository.ensureBootstrapRuntimeEnvironmentImage(
		ctx, tx, organizationID, agentID, "", createdBy,
	)
	if err != nil || imageArtifactID == "" {
		return bootstrapFailure("materialize system assistant runtime image", err)
	}
	expectedCoreDigest, expectedDigest, err := runtimeEnvironmentConfigurationDigests(values, secrets, image, tools, policy)
	if err != nil {
		return bootstrapFailure("compute system assistant runtime environment digest", err)
	}
	if imageArtifactID != "" && image.ArtifactRef != "" &&
		currentCoreDigest == expectedCoreDigest && currentDigest == expectedDigest {
		return nil
	}
	versionRef, err := newRef("renvv")
	if err != nil {
		return err
	}
	nextRuntimeRevision := "system-assistant-runtime-" + expectedDigest
	var activatedVersionID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapReconcileAssistantRuntimeEnvironment, pgx.StrictNamedArgs{
		"organization_id": organizationID, "agent_id": agentID, "environment_id": environmentID,
		"current_version_id": currentVersionID, "current_version": currentVersion, "version_ref": versionRef,
		"expected_core_digest": expectedCoreDigest, "expected_digest": expectedDigest,
		"image_artifact_id":     imageArtifactID,
		"next_runtime_revision": nextRuntimeRevision,
	}).Scan(&activatedVersionID); err != nil || activatedVersionID == "" {
		return bootstrapFailure("activate system assistant runtime environment revision", err)
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, queryRepositoryBootstrapInsertAssistantRuntimeEnvironmentAudit, pgx.StrictNamedArgs{
		"audit_ref": auditRef, "organization_id": organizationID, "agent_id": agentID,
		"environment_id": environmentID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return bootstrapFailure("audit system assistant runtime environment revision", err)
	}
	return nil
}

// systemAssistantCoreRevisionNumber accepts the system-assistant-core-v prefix followed by a
// positive uint32 decimal revision and returns a validity flag.
// systemAssistantCoreRevisionNumber принимает префикс system-assistant-core-v с положительной
// десятичной uint32-ревизией и возвращает признак допустимости.
func systemAssistantCoreRevisionNumber(revision string) (uint64, bool) {
	const prefix = "system-assistant-core-v"
	if !strings.HasPrefix(revision, prefix) {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(revision, prefix), 10, 32)
	return number, err == nil && number > 0
}

// scope holds owner-resolved internal identity and authority references plus correlation and
// credential-authentication time for a repository operation.
// scope хранит разрешённые владельцем внутренние identity и authority references, correlation и
// время аутентификации credential для операции repository.
type scope struct {
	interactionIdentityID                                                               string
	authorityProjectID                                                                  string
	organizationID, organizationRef, actorID, actorRef, actorName, role, correlationRef string
	credentialAuthenticatedAt                                                           time.Time
}

// ResolvePrincipal requires UUID actor/tenant identities and resolves active public references
// through the verified-principal SQL path, preserving the remaining principal fields.
// ResolvePrincipal требует UUID actor и tenant и разрешает активные публичные ссылки через
// verified-principal SQL path, сохраняя остальные поля principal.
func (repository *Repository) ResolvePrincipal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if uuid.Validate(principal.ActorID) != nil || uuid.Validate(principal.AuthorityTenant) != nil {
		return value.Principal{}, errs.ErrForbidden
	}
	var actorRef, organizationRef string
	if err := repository.pool.QueryRow(ctx, queryResolveVerifiedPrincipal, principal.ActorID, principal.AuthorityTenant).Scan(&actorRef, &organizationRef); errors.Is(err, pgx.ErrNoRows) {
		return value.Principal{}, errs.ErrForbidden
	} else if err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	principal.ActorID = actorRef
	principal.AuthorityTenant = organizationRef
	return principal, nil
}

// resolveProofIdentity resolves system workload or user/STT identity, synchronizes profile/groups
// and checks project or active access; only the public gateway may enter initial owner claim, and
// denial may follow a committed profile update.
// resolveProofIdentity разрешает system workload либо user/STT identity, синхронизирует
// profile/groups и проверяет project либо active access; только публичный gateway может впервые
// claim owner, а отказ может следовать после commit обновления профиля.
func (repository *Repository) resolveProofIdentity(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if input.CallerWorkload == "" || input.Operation == "" {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	_, trustedSTT := platformrepo.TrustedSTTAuthorityPermission(input.CallerWorkload, input.Operation)
	trustedSTT = trustedSTT && repository.trustedCluster && input.RPCProfile == "trusted-cluster" && input.ProjectRef == ""
	if input.CallerWorkload != "control-api-gateway" && !trustedSTT {
		if input.ExternalActorID != "kodex-system-subject" || input.ExternalTenantID != "kodex-installation" || input.ProjectRef != "" {
			return platformrepo.ProofAuthority{}, errs.ErrForbidden
		}
		var authority platformrepo.ProofAuthority
		var updatedAt time.Time
		if err := repository.pool.QueryRow(ctx, queryResolveSystemWorkloadIdentity).Scan(
			&authority.ActorID, &authority.OrganizationID, &updatedAt, &authority.OrganizationVersion,
		); errors.Is(err, pgx.ErrNoRows) {
			return platformrepo.ProofAuthority{}, errs.ErrForbidden
		} else if err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		authority.ActorVersion = 1
		return authority, nil
	}
	if uuid.Validate(input.ExternalActorID) != nil || uuid.Validate(input.ExternalTenantID) != nil {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	displayName := strings.TrimSpace(input.ExternalDisplayName)
	if displayName == "" {
		displayName = "i18n:OIDC_USER_NAME"
	}
	if utf8.RuneCountInString(displayName) > 160 || len(input.ExternalEmailHint) > 200 || strings.TrimSpace(input.ExternalEmailHint) != input.ExternalEmailHint || strings.ContainsAny(displayName+input.ExternalEmailHint, "\r\n\x00") {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if len(input.ExternalGroups) > 0 && (input.ExternalIssuer == "" || input.ExternalSessionRevision == 0) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	actorDigest := sha256.Sum256([]byte(input.ExternalTenantID + "\x00" + input.ExternalActorID))
	authority, found, err := repository.resolveClaimedProofAuthority(
		ctx,
		input,
		hex.EncodeToString(actorDigest[:]),
	)
	if err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	if found {
		tx, err := repository.pool.Begin(ctx)
		if err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, queryUpdateOIDCSubjectProfile, authority.OrganizationID, authority.ActorID, displayName, input.ExternalEmailHint); err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		if err := repository.syncOIDCGroups(ctx, tx, authority.OrganizationID, authority.ActorID, input); err != nil {
			return platformrepo.ProofAuthority{}, err
		}
		return repository.finishProofIdentityResolution(ctx, tx, input.ProjectRef, authority)
	}
	// Впервые назначить владельца установки может только публичный gateway.
	if trustedSTT {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	authority = platformrepo.ProofAuthority{}
	var authorityTenant, claimState string
	if err := tx.QueryRow(ctx, queryLockInstallationOwnerClaim).Scan(
		&authority.OrganizationID, &authority.OrganizationVersion, &authorityTenant, &claimState,
	); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if authorityTenant != "" && authorityTenant != input.ExternalTenantID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if claimState == "PENDING_CLAIM" && !input.OwnerClaim {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	err = tx.QueryRow(ctx, queryFindInstallationOwnerSubject, authority.OrganizationID, hex.EncodeToString(actorDigest[:])).Scan(&authority.ActorID)
	if errors.Is(err, pgx.ErrNoRows) {
		actorRef, refErr := newRef("usr")
		if refErr != nil {
			return platformrepo.ProofAuthority{}, refErr
		}
		if err := tx.QueryRow(ctx, queryCreateInstallationOwnerSubject,
			actorRef, authority.OrganizationID, hex.EncodeToString(actorDigest[:]), displayName, input.ExternalEmailHint).Scan(&authority.ActorID); err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		if claimState == "PENDING_CLAIM" {
			if _, err := tx.Exec(ctx, queryClaimInstallationOwnership,
				authority.OrganizationID, authority.ActorID, input.ExternalTenantID); err != nil {
				return platformrepo.ProofAuthority{}, errs.ErrUnavailable
			}
			bindingRef, refErr := newRef("abnd")
			if refErr != nil {
				return platformrepo.ProofAuthority{}, refErr
			}
			if _, err := tx.Exec(ctx, queryAccessInsertOwnerBinding, pgx.NamedArgs{
				"ref": bindingRef, "organization_id": authority.OrganizationID, "subject_id": authority.ActorID,
			}); err != nil {
				return platformrepo.ProofAuthority{}, errs.ErrUnavailable
			}
			authority.OrganizationVersion++
		}
	} else if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryUpdateOIDCSubjectProfile, authority.OrganizationID, authority.ActorID, displayName, input.ExternalEmailHint); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if err := repository.syncOIDCGroups(ctx, tx, authority.OrganizationID, authority.ActorID, input); err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	return repository.finishProofIdentityResolution(ctx, tx, input.ProjectRef, authority)
}

// subjectHasProofAccess reads the active-binding eligibility predicate for one resolved
// organization and subject inside the caller transaction.
// subjectHasProofAccess читает predicate доступности активной привязки для разрешённых организации
// и subject внутри транзакции caller.
func (repository *Repository) subjectHasProofAccess(ctx context.Context, tx pgx.Tx, organizationID, subjectID string) (bool, error) {
	var allowed bool
	if err := tx.QueryRow(ctx, queryProofSubjectHasActiveBinding, organizationID, subjectID).Scan(&allowed); err != nil {
		return false, errs.ErrUnavailable
	}
	return allowed, nil
}

// resolveClaimedProofAuthority reads a claimed organization/subject pair in a read-only
// repeatable-read transaction, distinguishing missing identity from backend failure and requiring
// commit before success.
// resolveClaimedProofAuthority читает claimed organization/subject pair в read-only
// repeatable-read транзакции, отличая отсутствие identity от отказа backend и требуя commit до
// успеха.
func (repository *Repository) resolveClaimedProofAuthority(
	ctx context.Context,
	input platformrepo.ProofPrincipalInput,
	actorDigest string,
) (platformrepo.ProofAuthority, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return platformrepo.ProofAuthority{}, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var authority platformrepo.ProofAuthority
	if err := tx.QueryRow(
		ctx,
		queryResolveClaimedOwnerSubject,
		input.ExternalTenantID,
		actorDigest,
	).Scan(
		&authority.OrganizationID,
		&authority.OrganizationVersion,
		&authority.ActorID,
	); errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, false, nil
	} else if err != nil {
		return platformrepo.ProofAuthority{}, false, errs.ErrUnavailable
	}

	if err := tx.Commit(ctx); err != nil {
		return platformrepo.ProofAuthority{}, false, errs.ErrConflict
	}
	return authority, true, nil
}

// NextAuthorityProofRevision obtains a revision from the database and rejects zero or values above
// the exact JavaScript-safe integer range.
// NextAuthorityProofRevision получает ревизию из БД и отклоняет ноль либо значения выше точного
// диапазона JavaScript-safe integer.
func (repository *Repository) NextAuthorityProofRevision(ctx context.Context) (uint64, error) {
	var revision uint64
	if err := repository.pool.QueryRow(ctx, queryNextAuthorityProofRevision).Scan(&revision); err != nil {
		return 0, errs.ErrUnavailable
	}
	if revision == 0 || revision > 9007199254740991 {
		return 0, errs.ErrConflict
	}
	return revision, nil
}

// AcceptWorkerGrant validates workload, generation, revision and lifetime bounds before
// instance-specific or durable high-watermark admission, requiring the accepted revision to match.
// AcceptWorkerGrant проверяет workload, generation, revision и lifetime bounds до
// instance-specific либо durable high-watermark admission, требуя совпадение принятой ревизии.
func (repository *Repository) AcceptWorkerGrant(ctx context.Context, input platformrepo.WorkerGrantInput) error {
	if input.WorkloadID == "" || input.CredentialGeneration == 0 || input.CredentialGeneration > 9007199254740991 || input.Revision == 0 || input.Revision > 9007199254740991 ||
		input.IssuedAt.IsZero() || !input.ExpiresAt.After(input.IssuedAt) {
		return errs.ErrForbidden
	}
	if input.InstanceID != "" {
		return repository.acceptWorkerGrantInstance(ctx, input)
	}
	var accepted uint64
	if err := repository.pool.QueryRow(ctx, queryAcceptWorkerGrantHighWatermark,
		input.WorkloadID, input.CredentialGeneration, input.Revision, input.IssuedAt.UTC(), input.ExpiresAt.UTC()).Scan(&accepted); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if accepted != input.Revision {
		return errs.ErrConflict
	}
	return nil
}

// resolveScope uses the same resolved-scope SQL and mapping as the observed path while supplying
// no error observer.
// resolveScope использует те же resolved-scope SQL и преобразование, что наблюдаемый путь, без
// передачи error observer.
func (repository *Repository) resolveScope(ctx context.Context, principal value.Principal) (scope, error) {
	return repository.resolveScopeObserved(ctx, principal, nil)
}

// resolveScopeObserved resolves actor/tenant scope, lets an optional in-process observer inspect
// query failure before domain mapping and copies principal correlation, project boundary and
// authentication time.
// resolveScopeObserved разрешает actor/tenant scope, передаёт query failure необязательному
// observer внутри процесса до domain mapping и копирует correlation, project boundary и
// authentication time principal.
func (repository *Repository) resolveScopeObserved(ctx context.Context, principal value.Principal, observe func(error)) (scope, error) {
	var result scope
	err := repository.pool.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, principal.ActorID, principal.AuthorityTenant).Scan(
		&result.organizationID, &result.organizationRef, &result.actorID, &result.actorRef, &result.actorName, &result.role)
	if err != nil && observe != nil {
		observe(err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return scope{}, errs.ErrForbidden
	}
	if err != nil {
		return scope{}, errs.ErrUnavailable
	}
	result.correlationRef = principal.CorrelationRef
	result.authorityProjectID = principal.ProjectRef
	result.credentialAuthenticatedAt = principal.CredentialAuthenticatedAt
	return result, nil
}

// allPermissions returns a fresh slice of the fixed project permission names without assigning
// those permissions to any principal.
// allPermissions возвращает новый срез фиксированных имён project permissions без назначения этих
// разрешений principal.
func allPermissions() []string {
	return []string{"VIEW", "MANAGE", "MANAGE_MEMBERS", "MANAGE_AGENTS", "MANAGE_WORKFLOWS", "LAUNCH_RUNS", "CANCEL_RUNS", "RESOLVE_GATES", "MANAGE_ARTIFACTS", "MANAGE_SCHEDULES", "MANAGE_INTEGRATIONS", "VIEW_AUDIT"}
}

// newRef reads eighteen cryptographically random bytes and returns a prefix plus unpadded URL-safe
// base64 reference, exposing only a fixed error on entropy failure.
// newRef читает восемнадцать криптографически случайных байтов и возвращает prefix с unpadded
// URL-safe base64 reference, выдавая фиксированную ошибку при отказе entropy.
func newRef(prefix string) (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate opaque reference")
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

// boundedPage uses fifty for nonpositive page size, caps positive sizes at two hundred and leaves
// the page token untouched.
// boundedPage выбирает пятьдесят для неположительного page size, ограничивает положительный
// двумястами и не изменяет page token.
func boundedPage(page query.Page) int32 {
	if page.Size < 1 {
		return 50
	}
	if page.Size > 200 {
		return 200
	}
	return page.Size
}

// asJSON returns encoding/json output while deliberately discarding its error, preserving the
// existing nil-byte fallback for unsupported values.
// asJSON возвращает результат encoding/json, намеренно отбрасывая ошибку и сохраняя прежнюю
// nil-byte замену для неподдерживаемых значений.
func asJSON(value any) []byte { encoded, _ := json.Marshal(value); return encoded }

// scanTime returns nil for absent or zero time and otherwise returns the original pointer without
// copying the timestamp.
// scanTime возвращает nil для отсутствующего либо нулевого времени, иначе исходный pointer без
// копирования timestamp.
func scanTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return value
}

var _ platformrepo.Repository = (*Repository)(nil)

// finishProofIdentityResolution performs the existing project/active-binding check after profile
// synchronization, committing before success or access denial and preserving conflict precedence.
// finishProofIdentityResolution выполняет прежнюю project/active-binding проверку после profile
// synchronization, делая commit перед успехом либо отказом в доступе и сохраняя приоритет conflict.
// The caller owns rollback on query failure; this helper does not resolve actor or tenant identity.
// Caller владеет rollback при query failure; helper не разрешает actor либо tenant identity.
func (repository *Repository) finishProofIdentityResolution(ctx context.Context, tx pgx.Tx, projectRef string, authority platformrepo.ProofAuthority) (platformrepo.ProofAuthority, error) {
	if projectRef != "" {
		if err := tx.QueryRow(ctx, queryAuthorizeProjectMembership,
			projectRef, authority.OrganizationID, authority.ActorID).Scan(&authority.ProjectID, &authority.ProjectVersion); errors.Is(err, pgx.ErrNoRows) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return platformrepo.ProofAuthority{}, errs.ErrConflict
			}
			return platformrepo.ProofAuthority{}, errs.ErrForbidden
		} else if err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
	} else if allowed, accessErr := repository.subjectHasProofAccess(ctx, tx, authority.OrganizationID, authority.ActorID); accessErr != nil {
		return platformrepo.ProofAuthority{}, accessErr
	} else if !allowed {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return platformrepo.ProofAuthority{}, errs.ErrConflict
		}
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrConflict
	}
	authority.ActorVersion = 1
	return authority, nil
}
