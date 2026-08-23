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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/systemassistant"
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

func New(pool *pgxpool.Pool, defaultRuntimeProvider, defaultRuntimeModel string) (*Repository, error) {
	if pool == nil || defaultRuntimeProvider != "openai-codex" || defaultRuntimeModel == "" {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Repository{pool: pool, defaultRuntimeProvider: defaultRuntimeProvider, defaultRuntimeModel: defaultRuntimeModel}, nil
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
		if err := repository.reconcileSystemAssistantCorePrompt(
			ctx,
			tx,
			systemassistant.CorePromptRevision,
			systemassistant.CorePrompt(),
		); err != nil {
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
	systemDigest := sha256.Sum256([]byte("mattercodex-system-subject"))
	var systemSubjectID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName,
		organizationID, hex.EncodeToString(systemDigest[:])).Scan(&systemSubjectID); err != nil {
		return errors.New("create system subject")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapSystemMembership, organizationID, systemSubjectID, allPermissions()); err != nil {
		return errors.New("create system subject membership")
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
	definitions := []struct {
		key, name, description, category string
		capabilities                     []entity.IntegrationCapability
		fields                           []entity.IntegrationConfigurationField
	}{
		{
			key: "github", name: "GitHub", description: "i18n:INTEGRATION_GITHUB_DESCRIPTION", category: "i18n:INTEGRATION_CATEGORY_DEVELOPMENT",
			capabilities: []entity.IntegrationCapability{{Key: "github.repository.read", Name: "i18n:INTEGRATION_GITHUB_REPOSITORY_READ_NAME", Description: "i18n:INTEGRATION_GITHUB_REPOSITORY_READ_DESCRIPTION", Risk: "READ"}},
			fields: []entity.IntegrationConfigurationField{
				{Key: "owner", Label: "i18n:INTEGRATION_FIELD_GITHUB_OWNER_LABEL", Help: "i18n:INTEGRATION_FIELD_GITHUB_OWNER_HELP", ValueType: "TEXT", Required: true, Placeholder: "i18n:INTEGRATION_FIELD_GITHUB_OWNER_PLACEHOLDER"},
				{Key: "repository", Label: "i18n:INTEGRATION_FIELD_GITHUB_REPOSITORY_LABEL", Help: "i18n:INTEGRATION_FIELD_GITHUB_REPOSITORY_HELP", ValueType: "TEXT", Required: true, Placeholder: "i18n:INTEGRATION_FIELD_GITHUB_REPOSITORY_PLACEHOLDER"},
			},
		},
		{
			key: "kubernetes", name: "Kubernetes", description: "i18n:INTEGRATION_KUBERNETES_DESCRIPTION", category: "i18n:INTEGRATION_CATEGORY_INFRASTRUCTURE",
			capabilities: []entity.IntegrationCapability{{Key: "kubernetes.workload.read", Name: "i18n:INTEGRATION_KUBERNETES_WORKLOAD_READ_NAME", Description: "i18n:INTEGRATION_KUBERNETES_WORKLOAD_READ_DESCRIPTION", Risk: "SENSITIVE"}},
			fields: []entity.IntegrationConfigurationField{
				{Key: "server_url", Label: "i18n:INTEGRATION_FIELD_SERVER_URL_LABEL", Help: "i18n:INTEGRATION_FIELD_SERVER_URL_HELP", ValueType: "URL", Required: true, Placeholder: "https://api.example.test"},
				{Key: "allowed_namespaces", Label: "i18n:INTEGRATION_FIELD_NAMESPACES_LABEL", Help: "i18n:INTEGRATION_FIELD_NAMESPACES_HELP", ValueType: "STRING_LIST", Required: true, Placeholder: "sales, support"},
			},
		},
		{
			key: "mattermost", name: "Mattermost", description: "i18n:INTEGRATION_MATTERMOST_DESCRIPTION", category: "i18n:INTEGRATION_CATEGORY_COMMUNICATIONS",
			capabilities: []entity.IntegrationCapability{
				{Key: "mattermost.inbound", Name: "i18n:INTEGRATION_MATTERMOST_INBOUND_NAME", Description: "i18n:INTEGRATION_MATTERMOST_INBOUND_DESCRIPTION", Risk: "READ"},
				{Key: "mattermost.notifications", Name: "i18n:INTEGRATION_MATTERMOST_NOTIFICATIONS_NAME", Description: "i18n:INTEGRATION_MATTERMOST_NOTIFICATIONS_DESCRIPTION", Risk: "WRITE"},
				{Key: "mattermost.result_mirror", Name: "i18n:INTEGRATION_MATTERMOST_RESULT_MIRROR_NAME", Description: "i18n:INTEGRATION_MATTERMOST_RESULT_MIRROR_DESCRIPTION", Risk: "WRITE"},
				{Key: "mattermost.gate_decisions", Name: "i18n:INTEGRATION_MATTERMOST_GATE_DECISIONS_NAME", Description: "i18n:INTEGRATION_MATTERMOST_GATE_DECISIONS_DESCRIPTION", Risk: "SENSITIVE"},
			},
			fields: []entity.IntegrationConfigurationField{
				{Key: "base_url", Label: "i18n:INTEGRATION_FIELD_BASE_URL_LABEL", Help: "i18n:INTEGRATION_FIELD_BASE_URL_HELP", ValueType: "URL", Required: true, Placeholder: "https://chat.example.test"},
				{Key: "team_name", Label: "i18n:INTEGRATION_FIELD_TEAM_LABEL", Help: "i18n:INTEGRATION_FIELD_TEAM_HELP", ValueType: "TEXT", Required: true, Placeholder: "operations"},
				{Key: "channel_name", Label: "i18n:INTEGRATION_FIELD_CHANNEL_LABEL", Help: "i18n:INTEGRATION_FIELD_CHANNEL_HELP", ValueType: "TEXT", Required: true, Placeholder: "ai-employees"},
			},
		},
	}
	for _, definition := range definitions {
		capabilityJSON, _ := json.Marshal(definition.capabilities)
		configurationJSON, _ := json.Marshal(definition.fields)
		if _, err := tx.Exec(ctx, queryRepositoryBootstrapInsertIntegrationDefinitionsStableKeyDescriptionCapabilities,
			definition.key, definition.name, definition.description, definition.category, capabilityJSON, configurationJSON); err != nil {
			return errors.New("seed integration definition")
		}
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

func systemAssistantCoreRevisionNumber(revision string) (uint64, bool) {
	const prefix = "system-assistant-core-v"
	if !strings.HasPrefix(revision, prefix) {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(revision, prefix), 10, 32)
	return number, err == nil && number > 0
}

func defaultProviderAccountID(ctx context.Context, tx pgx.Tx, organizationID string) (string, error) {
	var providerAccountID string
	if err := tx.QueryRow(ctx, queryRepositorySelectDefaultProviderAccount, organizationID).Scan(&providerAccountID); err != nil {
		return "", errs.ErrUnavailable
	}
	return providerAccountID, nil
}

type scope struct{ organizationID, organizationRef, actorID, actorRef, actorName, role, correlationRef string }

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
		if input.ExternalActorID != "mattercodex-system-subject" || input.ExternalTenantID != "mattercodex-installation" || input.ProjectRef != "" {
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var authority platformrepo.ProofAuthority
	var authorityTenant, claimState string
	if err := tx.QueryRow(ctx, queryLockInstallationOwnerClaim).Scan(
		&authority.OrganizationID, &authority.OrganizationVersion, &authorityTenant, &claimState,
	); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if authorityTenant != "" && authorityTenant != input.ExternalTenantID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	actorDigest := sha256.Sum256([]byte(input.ExternalTenantID + "\x00" + input.ExternalActorID))
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
			membershipRef, refErr := newRef("mem")
			if refErr != nil {
				return platformrepo.ProofAuthority{}, refErr
			}
			if _, err := tx.Exec(ctx, queryCreateInstallationOwnerMembership,
				membershipRef, authority.OrganizationID, authority.ActorID, allPermissions()); err != nil {
				return platformrepo.ProofAuthority{}, errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryClaimInstallationOwnership,
				authority.OrganizationID, authority.ActorID, input.ExternalTenantID); err != nil {
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
	var active bool
	if err := tx.QueryRow(ctx, queryCheckInstallationOwnerMembership, authority.OrganizationID, authority.ActorID).Scan(&active); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if !active {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return platformrepo.ProofAuthority{}, errs.ErrConflict
		}
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if input.ProjectRef != "" {
		if err := tx.QueryRow(ctx, queryAuthorizeProjectMembership,
			input.ProjectRef, authority.OrganizationID, authority.ActorID).Scan(&authority.ProjectID, &authority.ProjectVersion); errors.Is(err, pgx.ErrNoRows) {
			return platformrepo.ProofAuthority{}, errs.ErrForbidden
		} else if err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrConflict
	}
	authority.ActorVersion = 1
	return authority, nil
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
	if input.WorkloadID == "" || input.Revision == 0 || input.Revision > 9007199254740991 ||
		input.IssuedAt.IsZero() || !input.ExpiresAt.After(input.IssuedAt) {
		return errs.ErrForbidden
	}
	var accepted uint64
	if err := repository.pool.QueryRow(ctx, queryAcceptWorkerGrantHighWatermark,
		input.WorkloadID, input.Revision, input.IssuedAt.UTC(), input.ExpiresAt.UTC()).Scan(&accepted); errors.Is(err, pgx.ErrNoRows) {
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
