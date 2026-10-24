package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProjectAssistantGet reads an ACTIVE-project non-system profile under exact
// organization/project/authority-project constraints and derives display lifecycle from its agent.
// Archived/disabled agents remain representable as profile state rather than being silently
// recreated.
// queryProjectAssistantGet читает несистемный профиль активного проекта с точными ограничениями
// организации, проекта и области полномочий. Состояние профиля выводится из агента; архивный или
// выключенный агент остаётся представимым без скрытого повторного создания.
//
//go:embed sql/project_assistant__get.sql
var queryProjectAssistantGet string

// queryProjectAssistantLockProject locks an exact ACTIVE project inside organization and optional
// authority-project constraints before profile creation. Locking does not replace the command
// permission check.
// queryProjectAssistantLockProject блокирует точный ACTIVE проект организации в необязательной
// проектной границе перед созданием профиля. Блокировка не заменяет проверку права команды.
//
//go:embed sql/project_assistant__lock_project.sql
var queryProjectAssistantLockProject string

// queryProjectAssistantCreate inserts a project profile only for a matching enabled nonarchived
// non-system agent in the same organization/project, returning the canonical public profile
// projection. Caller transaction and uniqueness constraints own creation concurrency.
// queryProjectAssistantCreate вставляет профиль для совпавшего включённого неархивного
// несистемного агента той же организации и проекта, возвращая публичную проекцию. Конкурентным
// созданием управляют транзакция вызывающего и ограничения уникальности.
//
//go:embed sql/project_assistant__create.sql
var queryProjectAssistantCreate string

// queryProjectAssistantResolveCandidate resolves an enabled nonarchived canonical SYSTEM assistant
// or exact active-project PROJECT profile with optional authority-project restriction. This
// selection supplies identity metadata; actor-specific creation authorization remains in the
// surrounding command path.
// queryProjectAssistantResolveCandidate разрешает включённого неархивного канонического SYSTEM
// ассистента либо точный PROJECT профиль активного проекта с необязательной проектной границей.
// Возвращает идентичность; авторизация создания для субъекта остаётся у командного пути.
//
//go:embed sql/project_assistant__resolve_candidate.sql
var queryProjectAssistantResolveCandidate string

// queryProjectAssistantResolveConversation resolves a conversation owned by the exact
// actor/organization and authority project, validating canonical SYSTEM identity or exact PROJECT
// profile/agent/project lineage. It preserves existing conversations without adding the candidate
// query enabled/archive filter.
// queryProjectAssistantResolveConversation разрешает беседу точных субъекта, организации и
// проектной границы с проверкой канонического SYSTEM ассистента либо точной цепочки PROJECT
// профиля, агента и проекта. Сохраняет прежние беседы без дополнительного фильтра включённости и
// архива кандидатов.
//
//go:embed sql/project_assistant__resolve_conversation.sql
var queryProjectAssistantResolveConversation string

// queryProjectAssistantPlanConversation resolves a plan to its same-organization conversation only
// when that conversation belongs to the actor. It does not authorize applying the plan or prove
// the plan revision is current.
// queryProjectAssistantPlanConversation разрешает план в беседу той же организации только при её
// принадлежности субъекту. Не разрешает применение плана и не подтверждает свежесть его ревизии.
//
//go:embed sql/project_assistant__plan_conversation.sql
var queryProjectAssistantPlanConversation string

// queryProjectAssistantProtectedAgent tests whether an organization agent is the canonical system
// assistant or belongs to any project assistant profile, allowing the caller to reject ordinary
// archive. It returns no row for a missing agent and does not mutate state.
// queryProjectAssistantProtectedAgent проверяет, является ли агент организации каноническим
// системным ассистентом либо участником любого проектного профиля, чтобы отклонить обычное
// архивирование. Отсутствующий агент не даёт строки; состояние не меняется.
//
//go:embed sql/project_assistant__protected_agent.sql
var queryProjectAssistantProtectedAgent string

// queryProjectAssistantContextCreationAllowed asks the owner function whether profile creation may
// be presented for the resolved actor/project/assistant scope/context kind and requires a named
// project to exist. It is a context operation projection, not execution or approval of profile
// creation.
// queryProjectAssistantContextCreationAllowed запрашивает функцию владельца о возможности показать
// создание профиля для разрешённых субъекта, проекта, области ассистента и вида контекста, требуя
// существования указанного проекта. Это подсказка действия, а не разрешение исполнения.
//
//go:embed sql/project_assistant__context_creation_allowed.sql
var queryProjectAssistantContextCreationAllowed string

// querySystemAssistantIntegrationGrantOperations reads owner-derived system-assistant grant
// operation hints using exact organization/actor/assistant identity and optional authority
// project. Returned names do not bypass specialized grant command checks.
// querySystemAssistantIntegrationGrantOperations читает подсказки операций разрешений системного
// ассистента по точным организации, субъекту, ассистенту и необязательной проектной границе.
// Полученные имена не обходят проверки специализированных команд.
//
//go:embed sql/system_assistant_integration_grants__operations.sql
var querySystemAssistantIntegrationGrantOperations string

// queryAssistantFileRevisionOperations reads allowed file-revision operation hints from the shared
// context projection at transaction time with exact actor/authority/context/project/helper-scope
// inputs. The projection does not supply prepared content, OCC or mutation authority by itself.
// queryAssistantFileRevisionOperations читает подсказки файловых операций из функции владельца по
// точным организации, субъекту, проектной границе, контексту, проекту и области помощника.
// Передаваемое время транзакции не заменяет свежие проверки при выполнении.
//
//go:embed sql/assistant_file_revision_operations.sql
var queryAssistantFileRevisionOperations string

// rejectAssistantAgentArchive maps missing/projection failures to NotFound/Unavailable and rejects
// ordinary archival of a system or project-profile agent as Conflict. It is a protected-kind guard
// inside the caller transaction, not the full archive authorization path.
// rejectAssistantAgentArchive отклоняет обычное архивирование системного агента или агента
// проектного профиля с Conflict. Отсутствие строки даёт NotFound, прочая ошибка чтения —
// Unavailable; владение и право команды проверяются снаружи.
func (repository *Repository) rejectAssistantAgentArchive(ctx context.Context, tx pgx.Tx, current scope, agentRef string) error {
	var protected bool
	if err := tx.QueryRow(ctx, queryProjectAssistantProtectedAgent, current.organizationID, agentRef).Scan(&protected); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if protected {
		return errs.ErrConflict
	}
	return nil
}

// assistantCommandTarget resolves an actor-owned plan conversation when needed, reads its
// assistant lineage and chooses the specialized authorization target. SYSTEM uses
// organization.manage; PROJECT uses the exact project for view/manage or exact assistant agent for
// other permissions. It returns a target rather than executing or authorizing the command itself.
// assistantCommandTarget разрешает принадлежащую субъекту беседу плана при необходимости, затем
// идентичность ассистента. SYSTEM назначает цель organization.manage; PROJECT выбирает проект для
// project.view/manage и агента для прочих прав. Выбор цели не выполняет авторизацию.
func (repository *Repository) assistantCommandTarget(ctx context.Context, tx pgx.Tx, current scope, conversationRef, planRef, permission string) (string, resolvedAccessTarget, error) {
	if conversationRef == "" && planRef != "" {
		if err := tx.QueryRow(ctx, queryProjectAssistantPlanConversation, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID, "plan_ref": planRef,
		}).Scan(&conversationRef); errors.Is(err, pgx.ErrNoRows) {
			return "", resolvedAccessTarget{}, errs.ErrNotFound
		} else if err != nil {
			return "", resolvedAccessTarget{}, errs.ErrUnavailable
		}
	}
	assistant, err := repository.conversationAssistantTx(ctx, tx, current, conversationRef)
	if err != nil {
		return "", resolvedAccessTarget{}, err
	}
	if assistant.Scope == "SYSTEM" {
		return "organization.manage", resolvedAccessTarget{scope: organizationTarget(current.organizationRef)}, nil
	}
	if permission == "project.view" || permission == "project.manage" {
		return repository.resolveCommandTarget(ctx, tx, current, permission, "PROJECT", assistant.ProjectRef, assistant.ProjectRef)
	}
	return repository.resolveCommandTarget(ctx, tx, current, permission, "AGENT", assistant.Ref, assistant.ProjectRef)
}

// conversationAssistant holds internal/public assistant identity, SYSTEM/PROJECT scope and
// profile/project refs returned by trusted owner queries. It is a shared metadata projection, not
// a grant or a generic substitute for distinct candidate/conversation eligibility.
// conversationAssistant хранит внутренний ID агента и публичные ссылки агента, профиля, проекта с
// областью SYSTEM/PROJECT. Это проекция разрешённой беседы или кандидата, а не независимое
// подтверждение права команды.
type conversationAssistant struct {
	ID, Ref, Scope, ProfileRef, ProjectRef string
}

// assistantForConversationCreation requires SYSTEM or a project-bound PROJECT request, queries the
// corresponding candidate identity and delegates five-column decoding. SQL selection and caller
// creation authority stay separate; partial identity returned with a scan error must not be
// consumed.
// assistantForConversationCreation до запроса отклоняет неизвестную область и PROJECT без проекта,
// затем разрешает кандидата по организации, проекту и области полномочий. Частичный результат
// сканирования сохраняется при ошибке; отсутствие строки даёт NotFound, другой сбой — Unavailable.
func (repository *Repository) assistantForConversationCreation(ctx context.Context, tx pgx.Tx, current scope, input command.AssistantConversationInput) (conversationAssistant, error) {
	var assistant conversationAssistant
	if input.AssistantScope != "SYSTEM" && input.AssistantScope != "PROJECT" || input.AssistantScope == "PROJECT" && input.ProjectRef == "" {
		return assistant, errs.ErrInvalid
	}
	return scanConversationAssistant(tx.QueryRow(ctx, queryProjectAssistantResolveCandidate, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": input.ProjectRef,
		"assistant_scope": input.AssistantScope, "authority_project": current.authorityProjectID,
	}))
}

// conversationAssistantTx queries exact organization/actor/conversation/authority-project identity
// and delegates the same five-column decoder without replacing this query with candidate-selection
// policy. The caller owns transaction and current command authorization.
// conversationAssistantTx читает пять полей ассистента беседы точных организации, субъекта и
// проектной границы в транзакции вызывающего. Сохраняет частичный результат при ошибке; отсутствие
// строки даёт NotFound, другой сбой — Unavailable. Транзакцией не управляет.
func (repository *Repository) conversationAssistantTx(ctx context.Context, tx pgx.Tx, current scope, conversationRef string) (conversationAssistant, error) {
	return scanConversationAssistant(tx.QueryRow(ctx, queryProjectAssistantResolveConversation, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"conversation_ref": conversationRef, "authority_project": current.authorityProjectID,
	}))
}

// scanConversationAssistant scans the five internal assistant identity fields shared by creation
// and existing-conversation lookups. It preserves partial scan output on failure, maps wrapped
// missing rows to not-found and hides other driver failures. Callers retain their distinct SQL,
// actor/project boundaries, input validation and transaction ownership.
// scanConversationAssistant читает пять внутренних полей идентичности ассистента для создания
// и чтения беседы. Сохраняет частично заполненный результат при ошибке, преобразует обёрнутое
// отсутствие строки в not-found, остальные ошибки драйвера скрывает. Разные SQL, границы actor
// и проекта, проверка входа и управление транзакцией остаются у вызывающих.
func scanConversationAssistant(row pgx.Row) (conversationAssistant, error) {
	var assistant conversationAssistant
	err := row.Scan(&assistant.ID, &assistant.Ref, &assistant.Scope, &assistant.ProfileRef, &assistant.ProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return assistant, errs.ErrNotFound
	}
	if err != nil {
		return assistant, errs.ErrUnavailable
	}
	return assistant, nil
}

// appendAssistantContextOperations appends owner-projected operation hints in their original order,
// skipping values already present or repeated in this input without rewriting existing entries.
// Empty input leaves the slice unchanged; the helper performs no query or authorization and does
// not broaden the separate file-operation filter retained by its caller.
// appendAssistantContextOperations добавляет полученные от владельца подсказки операций в исходном
// порядке, пропуская уже присутствующие и повторяющиеся значения без изменения прежних элементов.
// Пустой вход сохраняет срез. Функция не выполняет запросы или авторизацию и не расширяет отдельный
// фильтр файловых операций, оставленный у вызывающего.
func appendAssistantContextOperations(descriptor *entity.AssistantContextDescriptor, operations []string) {
	for _, operation := range operations {
		if !contains(descriptor.AllowedOperations, operation) {
			descriptor.AllowedOperations = append(descriptor.AllowedOperations, operation)
		}
	}
}

// projectConversation copies assistant scope/reference/profile reference into a nonnil
// conversation projection without changing its project/context or evaluating access.
// projectConversation копирует область ассистента и публичные ссылки агента и профиля в непустую
// беседу. Проект и контекст беседы не меняет; авторизация здесь не выполняется.
func (assistant conversationAssistant) projectConversation(conversation *entity.AssistantConversation) {
	conversation.AssistantScope, conversation.AssistantRef, conversation.AssistantProfileRef = assistant.Scope, assistant.Ref, assistant.ProfileRef
}

// expandAssistantContext reads separate owner projections for profile creation, system grants,
// project connections and file revisions, adding only missing operation names in order. Only the
// recognized file-revision kind is appended from that projection; errors stop with possible
// partial in-memory hints, not committed authority or effects.
// expandAssistantContext последовательно добавляет подсказку создания проектного ассистента,
// операции разрешений и соединений, затем только разрешённую файловую операцию. Сохраняет порядок
// и уникальность добавлений; ошибка может оставить частичные подсказки в памяти. Право исполнения
// проверяется отдельно.
func (repository *Repository) expandAssistantContext(ctx context.Context, tx pgx.Tx, current scope, assistant conversationAssistant, projectRef string, descriptor *entity.AssistantContextDescriptor) error {
	var allowed bool
	if err := tx.QueryRow(ctx, queryProjectAssistantContextCreationAllowed, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"project_ref": projectRef, "assistant_scope": assistant.Scope, "context_kind": descriptor.EntityKind,
	}).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if allowed && !contains(descriptor.AllowedOperations, "CREATE_PROJECT_ASSISTANT") {
		descriptor.AllowedOperations = append(descriptor.AllowedOperations, "CREATE_PROJECT_ASSISTANT")
	}
	var grantOperations []string
	if err := tx.QueryRow(ctx, querySystemAssistantIntegrationGrantOperations, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_agent_id": assistant.ID, "assistant_scope": assistant.Scope,
		"authority_project": current.authorityProjectID,
	}).Scan(&grantOperations); err != nil {
		return errs.ErrUnavailable
	}
	appendAssistantContextOperations(descriptor, grantOperations)
	var connectionOperations []string
	if err := tx.QueryRow(ctx, queryProjectAssistantConnectionOperations, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_agent_id": assistant.ID, "assistant_scope": assistant.Scope,
		"authority_project": current.authorityProjectID,
	}).Scan(&connectionOperations); err != nil {
		return errs.ErrUnavailable
	}
	appendAssistantContextOperations(descriptor, connectionOperations)
	var fileOperations []string
	if err := tx.QueryRow(ctx, queryAssistantFileRevisionOperations, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"context_kind": descriptor.EntityKind, "context_ref": descriptor.EntityRef, "project_ref": projectRef, "helper_scope": assistant.Scope,
	}).Scan(&fileOperations); err != nil {
		return errs.ErrUnavailable
	}
	if contains(fileOperations, createProjectFileRevision) && !contains(descriptor.AllowedOperations, createProjectFileRevision) {
		descriptor.AllowedOperations = append(descriptor.AllowedOperations, createProjectFileRevision)
	}
	return nil
}

// constrainAssistantPlanScope re-resolves conversation assistant lineage, verifies file-revision
// targets belong to the conversation project and restricts project assistants to their exact
// project and supported self/project operations. It mutates the supplied scope authorityProjectID
// only after owner resolution; later per-operation authorization/OCC still apply.
// constrainAssistantPlanScope разрешает ассистента, проверяет общий проект и допустимый контекст
// файловых ревизий, для PROJECT сужает проектную границу и проверяет закрытый набор операций и
// ссылки ассистента. Последующая авторизация каждой команды сохраняется.
func (repository *Repository) constrainAssistantPlanScope(ctx context.Context, tx pgx.Tx, current *scope, conversationRef string, operations []entity.AssistantPlanOperation) error {
	assistant, err := repository.conversationAssistantTx(ctx, tx, *current, conversationRef)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Type != createProjectFileRevision {
			continue
		}
		context, projectRef, err := repository.assistantConversationContext(ctx, tx, *current, conversationRef, "")
		if err != nil {
			return err
		}
		if projectRef == "" || assistant.ProjectRef != projectRef || assistant.Scope == "SYSTEM" && context.EntityKind != "PROJECT" && context.EntityKind != "FILE" {
			return errs.ErrForbidden
		}
		item, err := repository.artifactRevisionReadHead(ctx, tx, *current, operation.Target.Ref)
		if err != nil {
			return err
		}
		if item.ProjectRef != projectRef {
			return errs.ErrNotFound
		}
	}
	if assistant.Scope != "PROJECT" {
		return nil
	}
	current.authorityProjectID = mustProjectID(ctx, tx, current.organizationID, assistant.ProjectRef)
	if current.authorityProjectID == "" {
		return errs.ErrNotFound
	}
	for _, operation := range operations {
		if operation.Type == prepareProjectAssistantConnection && assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref {
			return errs.ErrForbidden
		}
		if assistantProjectConfigurationOperation(operation) && assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref {
			return errs.ErrForbidden
		}
		if !projectAssistantOperation(operation.Type) {
			return errs.ErrForbidden
		}
		if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" && assistantString(operation.Parameters, "agentRef") != assistant.Ref {
			return errs.ErrForbidden
		}
	}
	return nil
}

// scanProjectAssistant decodes the eight-column public profile projection, mapping NoRows to
// NotFound and other scan errors to Unavailable with an empty result. It performs no actor or
// project access evaluation.
// scanProjectAssistant сканирует восемь полей публичного проектного профиля в фиксированном
// порядке. При отсутствии строки возвращает нулевой профиль и NotFound, при другом сбое — нулевой
// профиль и Unavailable; частичные данные не выдаёт.
func scanProjectAssistant(row pgx.Row) (entity.ProjectAssistantProfile, error) {
	var profile entity.ProjectAssistantProfile
	if err := row.Scan(&profile.Ref, &profile.ProjectRef, &profile.AgentRef, &profile.Name,
		&profile.State, &profile.Version, &profile.CreatedAt, &profile.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return entity.ProjectAssistantProfile{}, errs.ErrNotFound
	} else if err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	return profile, nil
}

// GetProjectAssistant requires current project.view in a read-only RepeatableRead snapshot, scans
// the authority-project-filtered profile and commits before return. A deferred independent
// one-second rollback protects cleanup; access errors are hidden as NotFound and read/commit
// failures retain Unavailable mapping.
// GetProjectAssistant разрешает область, открывает read-only RepeatableRead транзакцию, проверяет
// project.view и читает точный профиль. Ошибка доступа скрывается как NotFound; commit
// предшествует выдаче. Независимый ограниченный rollback выполняется при выходе, его ошибка
// игнорируется.
func (repository *Repository) GetProjectAssistant(ctx context.Context, principal value.Principal, projectRef string) (entity.ProjectAssistantProfile, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ProjectAssistantProfile{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: projectRef,
	}); err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrNotFound
	}
	profile, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": projectRef,
		"authority_project": current.authorityProjectID,
	}))
	if err != nil {
		return entity.ProjectAssistantProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	return profile, nil
}

// createProjectAssistant validates Unicode-bounded profile text, locks the exact active project
// and rejects any existing profile. It creates a project agent with empty initial capabilities,
// inserts its profile and emits agent invalidation in the caller transaction. This method copies
// no system-assistant environment/secret/grant inputs; outer execution owns command
// authorization/audit/idempotency/commit.
// createProjectAssistant проверяет обязательные тексты и пределы Unicode, блокирует активный
// проект, отклоняет существующий профиль, создаёт несистемного агента без копирования системных
// полномочий и профиль с событием AGENT_CHANGED. Авторизация, аудит, идемпотентность и commit
// принадлежат внешней команде.
func (repository *Repository) createProjectAssistant(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectAssistantInput)
	if !ok || payload.ProjectRef == "" || strings.TrimSpace(payload.Name) == "" ||
		utf8.RuneCountInString(payload.Name) > 160 || strings.TrimSpace(payload.Purpose) == "" || utf8.RuneCountInString(payload.Purpose) > 2000 ||
		strings.TrimSpace(payload.Instructions) == "" || utf8.RuneCountInString(payload.Instructions) > 32768 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID string
	if err := tx.QueryRow(ctx, queryProjectAssistantLockProject, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": payload.ProjectRef,
		"authority_project": current.authorityProjectID,
	}).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": payload.ProjectRef,
		"authority_project": current.authorityProjectID,
	}))
	if err == nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return commandOutcome{}, err
	}
	// Создание использует только project-owned baseline. Системные привязки
	// окружения, секретов и grants не входят во вход нового агента.
	created, err := repository.createAgent(ctx, tx, current, command.AgentInput{
		ProjectRef: payload.ProjectRef, Name: payload.Name, Purpose: payload.Purpose,
		RoleDescription: "i18n:PROJECT_ASSISTANT_ROLE", Instructions: payload.Instructions,
		InitialCapabilities: []string{},
	})
	if err != nil {
		return commandOutcome{}, err
	}
	ref, err := newRef("asstp")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	profile, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantCreate, pgx.StrictNamedArgs{
		"profile_ref": ref, "organization_id": current.organizationID, "project_id": projectID,
		"agent_ref": created.result.Agent.Ref, "created_by": current.actorID,
	}))
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, current, "AGENT_CHANGED", payload.ProjectRef,
		created.result.Agent.Ref, "i18n:PROJECT_ASSISTANT_CREATED"); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{ProjectAssistant: &profile},
		projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "PROJECT_ASSISTANT",
		resourceRef: ref, summary: "i18n:PROJECT_ASSISTANT_CREATED"}, nil
}
