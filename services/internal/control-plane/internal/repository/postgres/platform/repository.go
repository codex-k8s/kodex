// Package platform реализует PostgreSQL-порт универсального control-plane.
package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultRuntimeKey    = "builtin-safe-runtime"
	maximumArtifactBytes = platformrepo.MaximumArtifactBytes
)

type Repository struct {
	pool                   *pgxpool.Pool
	defaultRuntimeProvider string
	defaultRuntimeModel    string
	providerCredential     ProviderCredentialConfig
	roleImages             RoleImageConfig
	objects                objectstorage.Store
	integrationDefinitions map[string]integrationpackage.Package
	runtimeSecretNamespace string
}

// ProviderCredentialConfig содержит только безопасную identity неизменяемой
// Kubernetes Secret revision. Значение credential в control-plane не попадает.
type ProviderCredentialConfig struct {
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// RoleImageConfig связывает supply-chain lifecycle с точной policy, runtime ABI
// и secret, которым control-plane детерминированно подписывает fenced claims.
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
		objects: objects, integrationDefinitions: definitions, runtimeSecretNamespace: "kodex-runtime",
	}, nil
}

func (repository *Repository) ConfigureRuntimeSecrets(namespace string) error {
	if !validDNSLabel(namespace) {
		return errors.New("runtime secret namespace is invalid")
	}
	repository.runtimeSecretNamespace = namespace
	return nil
}

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

func (repository *Repository) ConfigureProviderCredential(config ProviderCredentialConfig) error {
	if !validDNSLabel(config.SecretName) || uuid.Validate(config.SecretUID) != nil ||
		config.SecretResourceVersion == "" || len(config.SecretResourceVersion) > 128 ||
		len(config.ContentSHA256) != sha256.Size*2 {
		return errors.New("provider credential metadata is invalid")
	}
	for _, character := range config.ContentSHA256 {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return errors.New("provider credential metadata is invalid")
		}
	}
	repository.providerCredential = config
	return nil
}

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
		if !valid || character == '-' && (index == 0 || index == len(value)-1) {
			return false
		}
	}
	return true
}

func (repository *Repository) Ready(ctx context.Context) error {
	var schemaVersion int
	if err := repository.pool.QueryRow(ctx, queryRepositoryReadySelectInstallationSingleton).Scan(&schemaVersion); err != nil {
		return errors.New("control-plane schema is unavailable")
	}
	if schemaVersion != 1 {
		return errors.New("control-plane schema version is unsupported")
	}
	if err := repository.objects.Check(ctx); err != nil {
		return errors.New("artifact object storage is unavailable")
	}
	return nil
}

func (repository *Repository) Bootstrap(ctx context.Context) error {
	if repository.providerCredential.SecretName == "" {
		return errors.New("provider credential metadata is required")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin bootstrap transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var bootstrappedAt *time.Time
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapSelectInstallationSingleton).Scan(&bootstrappedAt); err != nil {
		return errors.New("lock installation bootstrap")
	}
	if bootstrappedAt != nil {
		if err := repository.reconcileProviderCredential(ctx, tx); err != nil {
			return err
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
		return tx.Commit(ctx)
	}
	organizationRef, err := newRef("org")
	if err != nil {
		return err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertOrganizationsRefName, organizationRef).Scan(&organizationID); err != nil {
		return errors.New("create bootstrap organization")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertOwnerClaimContractsOrganizationIdStableKeyState, organizationID); err != nil {
		return errors.New("create owner claim contract")
	}
	systemDigest := sha256.Sum256([]byte("kodex-system-subject"))
	var systemSubjectID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName,
		organizationID, hex.EncodeToString(systemDigest[:])).Scan(&systemSubjectID); err != nil {
		return errors.New("create system subject")
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
			return errors.New("seed platform capability")
		}
	}
	limits, _ := json.Marshal(map[string]any{"cpu": "1000m", "memory": "2Gi", "maxConcurrentTurns": 1})
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertRuntimeProfilesStableKeyProviderRuntimeRevision, defaultRuntimeKey, repository.defaultRuntimeProvider, repository.defaultRuntimeModel, limits); err != nil {
		return errors.New("seed runtime profile")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertProviderDefinition); err != nil {
		return errors.New("seed provider definition")
	}
	providerAccountRef, err := newRef("pacc")
	if err != nil {
		return err
	}
	var providerAccountID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertProviderAccount,
		providerAccountRef, organizationID, systemSubjectID).Scan(&providerAccountID); err != nil {
		return errors.New("seed provider account")
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
		return errors.New("seed provider credential revision")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapActivateProviderCredentialRevision,
		providerAccountID, providerCredentialID); err != nil {
		return errors.New("activate provider credential revision")
	}
	roleRef, err := newRef("role")
	if err != nil {
		return err
	}
	var systemRoleID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertRoleDefinitionsRefStableKeyName,
		roleRef, organizationID, systemSubjectID).Scan(&systemRoleID); err != nil {
		return errors.New("seed system assistant role definition")
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
		return errors.New("create system assistant")
	}
	defaultRuntime, err := resolveEnabledRuntime(ctx, tx, defaultRuntimeKey)
	if err != nil {
		return errors.New("resolve system assistant runtime")
	}
	if err := repository.bootstrapAgentRuntime(ctx, tx, organizationID, agentID, "", defaultRuntime, systemSubjectID); err != nil {
		return fmt.Errorf("create system assistant runtime configuration: %w", err)
	}
	promptRef, err := newRef("ins")
	if err != nil {
		return err
	}
	corePrompt := systemassistant.CorePrompt()
	promptDigest := sha256.Sum256([]byte(corePrompt))
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertInstructionVersionsRefAgentIdState,
		promptRef, organizationID, agentID, corePrompt, hex.EncodeToString(promptDigest[:])); err != nil {
		return errors.New("create system assistant core prompt")
	}
	systemSessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertSessionsRefTargetTypeState,
		systemSessionRef, organizationID, providerAccountID, systemSubjectID); err != nil {
		return errors.New("create system assistant warm session")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertAssistantRuntimeOrganizationIdStableKeyCorePromptRevision,
		organizationID, agentID, promptRef, systemassistant.CorePromptRevision, systemSessionRef, limits); err != nil {
		return errors.New("create system assistant runtime contract")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapUpdateInstallationBootstrappedAt); err != nil {
		return errors.New("complete bootstrap")
	}
	return tx.Commit(ctx)
}

func (repository *Repository) reconcileIntegrationDefinitions(ctx context.Context, tx pgx.Tx) error {
	keys := make([]string, 0, len(repository.integrationDefinitions))
	for _, definition := range integrationpackage.Sorted(repository.integrationDefinitions) {
		var storedVersion, storedDigest string
		err := tx.QueryRow(ctx, queryRepositoryReconcileIntegrationDefinition, definition.Metadata.Key).Scan(&storedVersion, &storedDigest)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return errors.New("read integration definition revision")
		}
		if err == nil {
			comparison, compareErr := compareIntegrationDefinitionVersions(storedVersion, definition.Metadata.Version)
			if compareErr != nil || comparison > 0 || comparison == 0 && storedDigest != definition.Digest {
				return errors.New("integration definition revision rollback or digest mismatch")
			}
		}
		capabilities := make([]entity.IntegrationCapability, 0, len(definition.Spec.Capabilities))
		for _, capability := range definition.Spec.Capabilities {
			capabilities = append(capabilities, entity.IntegrationCapability{
				Key: capability.Key, Name: capability.Name, Description: capability.Description,
				Operation: capability.Operation, Risk: capability.Risk,
				ApprovalPolicy: capability.ApprovalPolicy, ResourceKind: capability.ResourceScope.Kind,
				InputFields: integrationConfigurationFields(capability.InputFields),
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
		); err != nil {
			return errors.New("reconcile integration definition")
		}
		keys = append(keys, definition.Metadata.Key)
	}
	if _, err := tx.Exec(ctx, queryRepositoryDisableUnshippedIntegrationDefinitions, keys); err != nil {
		return errors.New("disable unshipped integration definitions")
	}
	return nil
}

func integrationConfigurationFields(fields []integrationpackage.Field) []entity.IntegrationConfigurationField {
	result := make([]entity.IntegrationConfigurationField, 0, len(fields))
	for _, field := range fields {
		valueType := "TEXT"
		if field.Format == "HTTPS_ORIGIN" || field.Format == "HTTPS_URL" {
			valueType = "URL"
		}
		result = append(result, entity.IntegrationConfigurationField{
			Key: field.Key, Label: field.Key, ValueType: valueType, Required: field.Required,
		})
	}
	return result
}

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
		return errors.New("read current provider credential revision")
	}
	configured := repository.providerCredential
	if secretName == configured.SecretName && secretUID == configured.SecretUID &&
		secretResourceVersion == configured.SecretResourceVersion && contentSHA256 == configured.ContentSHA256 {
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
		return errors.New("create reconciled provider credential revision")
	}
	tag, err := tx.Exec(ctx, queryProviderAccountActivateReconciledCredential, pgx.StrictNamedArgs{
		"provider_account_id":            accountID,
		"current_credential_revision_id": currentCredentialID,
		"next_credential_revision_id":    nextCredentialID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("activate reconciled provider credential revision")
	}
	if err := repository.scheduleProviderCredentialCleanup(ctx, tx, organizationID, accountID,
		currentCredentialID, time.Now().UTC().Add(providerCredentialCleanupRetention)); err != nil {
		return errors.New("schedule reconciled provider credential cleanup")
	}
	return nil
}

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
		return errors.New("read system assistant core prompt")
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
		return errors.New("create system assistant core prompt revision")
	}
	tag, err := tx.Exec(ctx, queryRepositoryBootstrapUpdateAssistantCorePrompt, pgx.StrictNamedArgs{
		"prompt_ref":       promptRef,
		"next_revision":    expectedRevision,
		"organization_id":  organizationID,
		"agent_id":         agentID,
		"current_revision": currentRevision,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("activate system assistant core prompt revision")
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
		return errors.New("audit system assistant core prompt revision")
	}
	return nil
}

func (repository *Repository) reconcileSystemAssistantRuntimeEnvironment(ctx context.Context, tx pgx.Tx) error {
	var organizationID, agentID, environmentID, currentVersionID, currentCoreDigest, currentDigest string
	var currentVersion int64
	var rawValues, rawSecrets, rawTools []byte
	var rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte
	var resourcesDigest, volumesDigest, networkDigest, rbacDigest string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapSelectAssistantRuntimeEnvironment).Scan(
		&organizationID, &agentID, &environmentID, &currentVersionID, &currentVersion,
		&rawValues, &rawSecrets, &rawTools, &currentCoreDigest, &currentDigest,
		&rawResources, &rawVolumes, &rawNetwork, &rawKubernetesAccess,
		&resourcesDigest, &volumesDigest, &networkDigest, &rbacDigest,
	); err != nil {
		return errors.New("read system assistant runtime environment")
	}
	var values []runtimecontract.RuntimeEnvironmentValue
	var secrets []runtimecontract.RuntimeSecretProjection
	if err := decodeStoredRuntimeEnvironment(rawValues, rawSecrets, &values, &secrets); err != nil {
		return errors.New("decode system assistant runtime environment")
	}
	var tools []entity.RuntimeEnvironmentTool
	if err := decodeStrict(rawTools, &tools); err != nil {
		return errors.New("decode system assistant runtime tools")
	}
	policy, err := decodeRuntimeEnvironmentPolicy(rawResources, rawVolumes, rawNetwork, rawKubernetesAccess,
		resourcesDigest, volumesDigest, networkDigest, rbacDigest)
	if err != nil || policy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone {
		return errors.New("verify system assistant runtime policy")
	}
	image := entity.RuntimeEnvironmentImage{
		Reference: repository.roleImages.DefaultImageReference,
		Digest:    repository.roleImages.DefaultImageDigest,
	}
	expectedCoreDigest, expectedDigest, err := runtimeEnvironmentConfigurationDigests(values, secrets, image, tools, policy)
	if err != nil {
		return errors.New("compute system assistant runtime environment digest")
	}
	if currentCoreDigest == expectedCoreDigest && currentDigest == expectedDigest {
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
		"next_runtime_revision": nextRuntimeRevision,
	}).Scan(&activatedVersionID); err != nil || activatedVersionID == "" {
		return errors.New("activate system assistant runtime environment revision")
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
		return errors.New("audit system assistant runtime environment revision")
	}
	return nil
}

func systemAssistantCoreRevisionNumber(revision string) (uint64, bool) {
	const prefix = "system-assistant-core-v"
	if !strings.HasPrefix(revision, prefix) {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(revision, prefix), 10, 32)
	return number, err == nil && number > 0
}

type scope struct {
	organizationID, organizationRef, actorID, actorRef, actorName, role, correlationRef string
	credentialAuthenticatedAt                                                           time.Time
}

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

func (repository *Repository) ResolveProofAuthority(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if input.CallerWorkload == "" || input.Operation == "" {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if input.CallerWorkload != "control-api-gateway" {
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
		if input.ProjectRef != "" {
			if err := tx.QueryRow(ctx, queryAuthorizeProjectMembership,
				input.ProjectRef, authority.OrganizationID, authority.ActorID).Scan(&authority.ProjectID, &authority.ProjectVersion); errors.Is(err, pgx.ErrNoRows) {
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
	if input.ProjectRef != "" {
		if err := tx.QueryRow(ctx, queryAuthorizeProjectMembership,
			input.ProjectRef, authority.OrganizationID, authority.ActorID).Scan(&authority.ProjectID, &authority.ProjectVersion); errors.Is(err, pgx.ErrNoRows) {
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

func (repository *Repository) subjectHasProofAccess(ctx context.Context, tx pgx.Tx, organizationID, subjectID string) (bool, error) {
	var allowed bool
	if err := tx.QueryRow(ctx, queryProofSubjectHasActiveBinding, organizationID, subjectID).Scan(&allowed); err != nil {
		return false, errs.ErrUnavailable
	}
	return allowed, nil
}

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

func (repository *Repository) AcceptWorkerGrant(ctx context.Context, input platformrepo.WorkerGrantInput) error {
	if input.WorkloadID == "" || input.CredentialGeneration == 0 || input.Revision == 0 || input.Revision > 9007199254740991 ||
		input.IssuedAt.IsZero() || !input.ExpiresAt.After(input.IssuedAt) {
		return errs.ErrForbidden
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

func (repository *Repository) resolveScope(ctx context.Context, principal value.Principal) (scope, error) {
	var result scope
	err := repository.pool.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, principal.ActorID, principal.AuthorityTenant).Scan(
		&result.organizationID, &result.organizationRef, &result.actorID, &result.actorRef, &result.actorName, &result.role)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope{}, errs.ErrForbidden
	}
	if err != nil {
		return scope{}, errs.ErrUnavailable
	}
	result.correlationRef = principal.CorrelationRef
	result.credentialAuthenticatedAt = principal.CredentialAuthenticatedAt
	return result, nil
}

func allPermissions() []string {
	return []string{"VIEW", "MANAGE", "MANAGE_MEMBERS", "MANAGE_AGENTS", "MANAGE_WORKFLOWS", "LAUNCH_RUNS", "CANCEL_RUNS", "RESOLVE_GATES", "MANAGE_ARTIFACTS", "MANAGE_SCHEDULES", "MANAGE_INTEGRATIONS", "VIEW_AUDIT"}
}

func newRef(prefix string) (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate opaque reference")
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func boundedPage(page query.Page) int32 {
	if page.Size < 1 {
		return 50
	}
	if page.Size > 200 {
		return 200
	}
	return page.Size
}

func asJSON(value any) []byte { encoded, _ := json.Marshal(value); return encoded }

func scanTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return value
}

var _ platformrepo.Repository = (*Repository)(nil)
