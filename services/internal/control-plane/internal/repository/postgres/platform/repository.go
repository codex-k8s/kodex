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
	repository "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var organizationID, organizationRef, authorityTenant, claimState string
	if err := tx.QueryRow(ctx, queryRepositoryResolveprincipal1).Scan(&organizationID, &organizationRef, &authorityTenant, &claimState); err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	if authorityTenant != "" && authorityTenant != principal.AuthorityTenant {
		return value.Principal{}, errs.ErrForbidden
	}
	actorDigest := sha256.Sum256([]byte(principal.AuthorityTenant + "\x00" + principal.ActorID))
	var actorID, actorRef string
	err = tx.QueryRow(ctx, queryRepositoryResolveprincipal2, organizationID, hex.EncodeToString(actorDigest[:])).Scan(&actorID, &actorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		if claimState != "PENDING_CLAIM" || principal.Permission != "platform.bootstrap.claim" {
			return value.Principal{}, errs.ErrForbidden
		}
		actorRef, err = newRef("usr")
		if err != nil {
			return value.Principal{}, err
		}
		if err := tx.QueryRow(ctx, queryRepositoryResolveprincipal3,
			actorRef, organizationID, hex.EncodeToString(actorDigest[:])).Scan(&actorID); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		membershipRef, refErr := newRef("mem")
		if refErr != nil {
			return value.Principal{}, refErr
		}
		if _, err := tx.Exec(ctx, queryRepositoryResolveprincipal4,
			membershipRef, organizationID, actorID, allPermissions()); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRepositoryResolveprincipal5,
			organizationID, actorID); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryRepositoryResolveprincipal6, organizationID, principal.AuthorityTenant); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
	} else if err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	if claimState == "CLAIMED" {
		var active bool
		if err := tx.QueryRow(ctx, queryRepositoryResolveprincipal7, organizationID, actorID).Scan(&active); err != nil || !active {
			return value.Principal{}, errs.ErrForbidden
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return value.Principal{}, errs.ErrConflict
	}
	principal.ActorID = actorRef
	principal.AuthorityTenant = organizationRef
	return principal, nil
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

var _ repository.Repository = (*Repository)(nil)
