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
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	corePromptRevision   = "system-assistant-core-v1"
	corePrompt           = `Ты — встроенный Системный помощник MatterCodex. Помогай проверенному пользователю безопасно настраивать проекты, агентов, workflows, интеграции, разрешения и расписания. Любое изменение сначала представь как типизированный план, затем выполняй только специализированной командой control-plane в пределах полномочий пользователя. Никогда не обращайся напрямую к PostgreSQL, Kubernetes, secret storage или произвольному внешнему API. Не запрашивай и не показывай секретные значения.`
	defaultRuntimeKey    = "builtin-safe-runtime"
	maximumArtifactBytes = 16 << 20
)

type Repository struct {
	pool                   *pgxpool.Pool
	defaultRuntimeProvider string
	defaultRuntimeModel    string
}

func New(pool *pgxpool.Pool, defaultRuntimeProvider, defaultRuntimeModel string) (*Repository, error) {
	if pool == nil || defaultRuntimeProvider != "openai-codex" || defaultRuntimeModel == "" {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Repository{pool: pool, defaultRuntimeProvider: defaultRuntimeProvider, defaultRuntimeModel: defaultRuntimeModel}, nil
}

func (repository *Repository) Ready(ctx context.Context) error {
	var schemaVersion int
	if err := repository.pool.QueryRow(ctx, queryRepositoryReady1).Scan(&schemaVersion); err != nil {
		return errors.New("control-plane schema is unavailable")
	}
	if schemaVersion != 1 {
		return errors.New("control-plane schema version is unsupported")
	}
	return nil
}

func (repository *Repository) Bootstrap(ctx context.Context) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin bootstrap transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var bootstrappedAt *time.Time
	if err := tx.QueryRow(ctx, queryRepositoryBootstrap1).Scan(&bootstrappedAt); err != nil {
		return errors.New("lock installation bootstrap")
	}
	if bootstrappedAt != nil {
		return tx.Commit(ctx)
	}
	organizationRef, err := newRef("org")
	if err != nil {
		return err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrap2, organizationRef).Scan(&organizationID); err != nil {
		return errors.New("create bootstrap organization")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap3, organizationID); err != nil {
		return errors.New("create owner claim contract")
	}
	systemDigest := sha256.Sum256([]byte("mattercodex-system-subject"))
	var systemSubjectID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrap4,
		organizationID, hex.EncodeToString(systemDigest[:])).Scan(&systemSubjectID); err != nil {
		return errors.New("create system subject")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrapSystemMembership, organizationID, systemSubjectID, allPermissions()); err != nil {
		return errors.New("create system subject membership")
	}
	capabilities := []struct{ key, name, description, risk string }{
		{"platform.project.manage", "Управление проектами", "Создание и настройка проектов", "LOW"},
		{"platform.agent.manage", "Управление агентами", "Настройка агентов и инструкций", "MEDIUM"},
		{"platform.run.launch", "Запуск работы", "Запуск агентов и workflows", "MEDIUM"},
		{"platform.run.delegate", "Делегирование", "Запуск дочерних агентов через server-owned edge", "MEDIUM"},
		{"platform.gate.resolve", "Решения человека", "Разрешение Human Gate", "HIGH"},
		{"platform.artifact.manage", "Файлы", "Чтение и создание artifacts", "MEDIUM"},
		{"platform.schedule.manage", "Автоматизации", "Управление schedules", "HIGH"},
		{"platform.integration.grant", "Интеграции", "Выдача типизированных grants", "HIGH"},
	}
	for _, capability := range capabilities {
		if _, err := tx.Exec(ctx, queryRepositoryBootstrap5,
			capability.key, capability.name, capability.description, capability.risk); err != nil {
			return errors.New("seed platform capability")
		}
	}
	limits, _ := json.Marshal(map[string]any{"cpu": "1000m", "memory": "2Gi", "maxConcurrentTurns": 1})
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap6, defaultRuntimeKey, repository.defaultRuntimeProvider, repository.defaultRuntimeModel, limits); err != nil {
		return errors.New("seed runtime profile")
	}
	definitions := []struct {
		key, name, description, category string
		capabilities                     []entity.IntegrationCapability
	}{
		{"github", "GitHub", "Репозитории, issues и pull requests", "Разработка", []entity.IntegrationCapability{{Key: "github.repository.read", Name: "Чтение репозитория", Description: "Читать разрешённый репозиторий", Risk: "LOW"}, {Key: "github.pull_request.write", Name: "Изменение pull request", Description: "Создавать и изменять pull requests", Risk: "HIGH"}}},
		{"kubernetes", "Kubernetes", "Наблюдение и ограниченные операции workloads", "Инфраструктура", []entity.IntegrationCapability{{Key: "kubernetes.workload.read", Name: "Наблюдение workloads", Description: "Читать разрешённые workloads", Risk: "MEDIUM"}}},
		{"mattermost", "Mattermost", "Необязательные входящие сообщения, уведомления, зеркало и решения", "Коммуникации", []entity.IntegrationCapability{{Key: "mattermost.inbound", Name: "Входящие сообщения", Description: "Создавать задания из Mattermost", Risk: "MEDIUM"}, {Key: "mattermost.notifications", Name: "Уведомления", Description: "Отправлять уведомления", Risk: "LOW"}, {Key: "mattermost.result_mirror", Name: "Зеркало результатов", Description: "Публиковать безопасные итоги", Risk: "LOW"}, {Key: "mattermost.gate_decisions", Name: "Решения Human Gate", Description: "Принимать first-winner решения", Risk: "HIGH"}}},
	}
	for _, definition := range definitions {
		capabilityJSON, _ := json.Marshal(definition.capabilities)
		if _, err := tx.Exec(ctx, queryRepositoryBootstrap7,
			definition.key, definition.name, definition.description, definition.category, capabilityJSON); err != nil {
			return errors.New("seed integration definition")
		}
	}
	agentRef, err := newRef("agt")
	if err != nil {
		return err
	}
	var agentID string
	if err := tx.QueryRow(ctx, queryRepositoryBootstrap8,
		agentRef, organizationID, defaultRuntimeKey).Scan(&agentID); err != nil {
		return errors.New("create system assistant")
	}
	promptRef, err := newRef("ins")
	if err != nil {
		return err
	}
	promptDigest := sha256.Sum256([]byte(corePrompt))
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap9,
		promptRef, organizationID, agentID, corePrompt, hex.EncodeToString(promptDigest[:])); err != nil {
		return errors.New("create system assistant core prompt")
	}
	systemSessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap10,
		systemSessionRef, organizationID, systemSubjectID); err != nil {
		return errors.New("create system assistant warm session")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap11,
		organizationID, agentID, promptRef, corePromptRevision, systemSessionRef, limits); err != nil {
		return errors.New("create system assistant runtime contract")
	}
	if _, err := tx.Exec(ctx, queryRepositoryBootstrap12); err != nil {
		return errors.New("complete bootstrap")
	}
	return tx.Commit(ctx)
}

type scope struct{ organizationID, organizationRef, actorID, actorRef, actorName, role, correlationRef string }

func (repository *Repository) ResolvePrincipal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if uuid.Validate(principal.ActorID) != nil || uuid.Validate(principal.AuthorityTenant) != nil {
		return value.Principal{}, errs.ErrForbidden
	}
	var actorRef, organizationRef string
	if err := repository.pool.QueryRow(ctx, queryRepositoryResolveprincipal1, principal.ActorID, principal.AuthorityTenant).Scan(&actorRef, &organizationRef); errors.Is(err, pgx.ErrNoRows) {
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
		if err := repository.pool.QueryRow(ctx, queryRepositoryResolveProofAuthoritySystem1).Scan(
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var authority platformrepo.ProofAuthority
	var authorityTenant, claimState string
	if err := tx.QueryRow(ctx, queryRepositoryResolveProofAuthorityOwner1).Scan(
		&authority.OrganizationID, &authority.OrganizationVersion, &authorityTenant, &claimState,
	); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if authorityTenant != "" && authorityTenant != input.ExternalTenantID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	actorDigest := sha256.Sum256([]byte(input.ExternalTenantID + "\x00" + input.ExternalActorID))
	err = tx.QueryRow(ctx, queryRepositoryResolveProofAuthorityOwner2, authority.OrganizationID, hex.EncodeToString(actorDigest[:])).Scan(&authority.ActorID)
	if errors.Is(err, pgx.ErrNoRows) {
		if claimState != "PENDING_CLAIM" {
			return platformrepo.ProofAuthority{}, errs.ErrForbidden
		}
		actorRef, refErr := newRef("usr")
		if refErr != nil {
			return platformrepo.ProofAuthority{}, refErr
		}
		if err := tx.QueryRow(ctx, queryRepositoryResolveProofAuthorityOwner3,
			actorRef, authority.OrganizationID, hex.EncodeToString(actorDigest[:])).Scan(&authority.ActorID); err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		membershipRef, refErr := newRef("mem")
		if refErr != nil {
			return platformrepo.ProofAuthority{}, refErr
		}
		if _, err := tx.Exec(ctx, queryRepositoryResolveProofAuthorityOwner4,
			membershipRef, authority.OrganizationID, authority.ActorID, allPermissions()); err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRepositoryResolveProofAuthorityOwner5,
			authority.OrganizationID, authority.ActorID, input.ExternalTenantID); err != nil {
			return platformrepo.ProofAuthority{}, errs.ErrUnavailable
		}
		authority.OrganizationVersion++
	} else if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	var active bool
	if err := tx.QueryRow(ctx, queryRepositoryResolveProofAuthorityOwner6, authority.OrganizationID, authority.ActorID).Scan(&active); err != nil || !active {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if input.ProjectRef != "" {
		if err := tx.QueryRow(ctx, queryRepositoryResolveProofAuthorityProject1,
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
	if err := repository.pool.QueryRow(ctx, queryRepositoryNextProofRevision1).Scan(&revision); err != nil {
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
	err := repository.pool.QueryRow(ctx, queryRepositoryResolvescope1, principal.ActorID, principal.AuthorityTenant).Scan(
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
