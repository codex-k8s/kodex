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

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Repository{pool: pool}, nil
}

func (repository *Repository) Ready(ctx context.Context) error {
	var schemaVersion int
	if err := repository.pool.QueryRow(ctx, `SELECT schema_version FROM control_plane.installation WHERE singleton`).Scan(&schemaVersion); err != nil {
		return errors.New("control-plane schema is unavailable")
	}
	if schemaVersion != 1 {
		return errors.New("control-plane schema version is unsupported")
	}
	var assistantReady bool
	if err := repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM control_plane.assistant_runtime WHERE stable_key='system-assistant' AND runtime_state='READY' AND runtime_revision=desired_runtime_revision AND last_heartbeat_at>clock_timestamp()-interval '45 seconds')`).Scan(&assistantReady); err != nil || !assistantReady {
		return errors.New("system assistant warm runtime is unavailable")
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
	if err := tx.QueryRow(ctx, `SELECT bootstrapped_at FROM control_plane.installation WHERE singleton FOR UPDATE`).Scan(&bootstrappedAt); err != nil {
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
	if err := tx.QueryRow(ctx, `
		INSERT INTO control_plane.organizations (ref, name)
		VALUES ($1, 'MatterCodex') RETURNING id::text`, organizationRef).Scan(&organizationID); err != nil {
		return errors.New("create bootstrap organization")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO control_plane.owner_claim_contracts (organization_id, stable_key, state)
		VALUES ($1::uuid, 'installation-owner', 'PENDING_CLAIM')`, organizationID); err != nil {
		return errors.New("create owner claim contract")
	}
	systemDigest := sha256.Sum256([]byte("mattercodex-system-subject"))
	var systemSubjectID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.subjects
		(ref,organization_id,issuer,external_subject_digest,display_name,email_masked)
		VALUES ('sys_platform',$1::uuid,'mattercodex-system',$2,'MatterCodex','') RETURNING id::text`,
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
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.platform_capabilities
			(stable_key, name, description, risk) VALUES ($1,$2,$3,$4)`,
			capability.key, capability.name, capability.description, capability.risk); err != nil {
			return errors.New("seed platform capability")
		}
	}
	limits, _ := json.Marshal(map[string]any{"cpu": "1000m", "memory": "2Gi", "maxConcurrentTurns": 1})
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.runtime_profiles
		(stable_key,name,provider,model,runtime_revision,resource_limits)
		VALUES ($1,'Базовая среда','provider-neutral','configured-by-installation','runtime-v1',$2)`, defaultRuntimeKey, limits); err != nil {
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
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.integration_definitions
			(stable_key,name,description,category,capabilities,configuration_schema)
			VALUES ($1,$2,$3,$4,$5,'{"type":"object","additionalProperties":false}'::jsonb)`,
			definition.key, definition.name, definition.description, definition.category, capabilityJSON); err != nil {
			return errors.New("seed integration definition")
		}
	}
	agentRef, err := newRef("agt")
	if err != nil {
		return err
	}
	var agentID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.agents
		(ref,organization_id,system_key,name,purpose,role_description,runtime_key,state,enabled)
		VALUES ($1,$2::uuid,'system-assistant','Системный помощник','Настраивает платформу и объясняет её состояние',
		'Встроенный неудаляемый помощник с типизированными tools',$3,'READY',true) RETURNING id::text`,
		agentRef, organizationID, defaultRuntimeKey).Scan(&agentID); err != nil {
		return errors.New("create system assistant")
	}
	promptRef, err := newRef("ins")
	if err != nil {
		return err
	}
	promptDigest := sha256.Sum256([]byte(corePrompt))
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.instruction_versions
		(ref,organization_id,agent_id,version_number,state,content,digest,core,published_at)
		VALUES ($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,true,clock_timestamp())`,
		promptRef, organizationID, agentID, corePrompt, hex.EncodeToString(promptDigest[:])); err != nil {
		return errors.New("create system assistant core prompt")
	}
	systemSessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.sessions
		(ref,organization_id,target_type,target_ref,state,created_by)
		VALUES ($1,$2::uuid,'SYSTEM_ASSISTANT','system-assistant','ACTIVE',$3::uuid)`,
		systemSessionRef, organizationID, systemSubjectID); err != nil {
		return errors.New("create system assistant warm session")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.assistant_runtime
		(organization_id,agent_id,stable_key,core_prompt_ref,core_prompt_revision,runtime_state,
		runtime_revision,desired_runtime_revision,system_session_ref,resource_limits)
		VALUES ($1::uuid,$2::uuid,'system-assistant',$3,$4,'STARTING','',$4,$5,$6)`,
		organizationID, agentID, promptRef, corePromptRevision, systemSessionRef, limits); err != nil {
		return errors.New("create system assistant runtime contract")
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.installation SET bootstrapped_at=clock_timestamp() WHERE singleton`); err != nil {
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
	if err := tx.QueryRow(ctx, `SELECT o.id::text,o.ref,COALESCE(o.authority_tenant_ref,''),c.state
		FROM control_plane.organizations o JOIN control_plane.owner_claim_contracts c ON c.organization_id=o.id
		LIMIT 1 FOR UPDATE OF o,c`).Scan(&organizationID, &organizationRef, &authorityTenant, &claimState); err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	if authorityTenant != "" && authorityTenant != principal.AuthorityTenant {
		return value.Principal{}, errs.ErrForbidden
	}
	actorDigest := sha256.Sum256([]byte(principal.AuthorityTenant + "\x00" + principal.ActorID))
	var actorID, actorRef string
	err = tx.QueryRow(ctx, `SELECT id::text,ref FROM control_plane.subjects
		WHERE organization_id=$1::uuid AND external_subject_digest=$2`, organizationID, hex.EncodeToString(actorDigest[:])).Scan(&actorID, &actorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		if claimState != "PENDING_CLAIM" || principal.Permission != "platform.bootstrap.claim" {
			return value.Principal{}, errs.ErrForbidden
		}
		actorRef, err = newRef("usr")
		if err != nil {
			return value.Principal{}, err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO control_plane.subjects
			(ref,organization_id,issuer,external_subject_digest,display_name)
			VALUES ($1,$2::uuid,'verified-internal-authority',$3,'Владелец') RETURNING id::text`,
			actorRef, organizationID, hex.EncodeToString(actorDigest[:])).Scan(&actorID); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		membershipRef, refErr := newRef("mem")
		if refErr != nil {
			return value.Principal{}, refErr
		}
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.memberships
			(ref,organization_id,subject_id,role,permissions) VALUES ($1,$2::uuid,$3::uuid,'OWNER',$4)`,
			membershipRef, organizationID, actorID, allPermissions()); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.owner_claim_contracts
			SET state='CLAIMED',subject_id=$2::uuid,claimed_at=clock_timestamp(),version=version+1 WHERE organization_id=$1::uuid`,
			organizationID, actorID); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.organizations SET authority_tenant_ref=$2 WHERE id=$1::uuid`, organizationID, principal.AuthorityTenant); err != nil {
			return value.Principal{}, errs.ErrUnavailable
		}
	} else if err != nil {
		return value.Principal{}, errs.ErrUnavailable
	}
	if claimState == "CLAIMED" {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_or(active),false) FROM control_plane.memberships
			WHERE organization_id=$1::uuid AND subject_id=$2::uuid`, organizationID, actorID).Scan(&active); err != nil || !active {
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
	err := repository.pool.QueryRow(ctx, `SELECT o.id::text,o.ref,s.id::text,s.ref,s.display_name,
		COALESCE((SELECT m.role FROM control_plane.memberships m WHERE m.organization_id=o.id AND m.subject_id=s.id AND m.project_id IS NULL AND m.active LIMIT 1),'MEMBER')
		FROM control_plane.organizations o
		JOIN control_plane.subjects s ON s.organization_id=o.id AND s.ref=$1
		WHERE o.ref=$2 AND EXISTS (SELECT 1 FROM control_plane.memberships m WHERE m.organization_id=o.id AND m.subject_id=s.id AND m.active)`, principal.ActorID, principal.AuthorityTenant).Scan(
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
