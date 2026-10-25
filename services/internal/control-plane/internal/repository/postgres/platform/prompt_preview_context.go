package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryPromptPreviewAgent embeds the enabled, nonarchived organization/agent prompt projection,
// preferring an exact managed binding revision over a published instruction binding. The system
// assistant may append owner instructions to returned content while retaining the selected
// instruction digest. Authorization and complete snapshot pinning belong to the surrounding
// preview path.
// queryPromptPreviewAgent встраивает prompt projection включённого неархивного organization/agent,
// предпочитая точную managed binding revision опубликованной instruction binding. Системный
// помощник может добавлять owner instructions к возвращаемому content с сохранением digest
// выбранной инструкции. Авторизация и полное закрепление snapshot принадлежат окружающему preview
// path.
//
//go:embed sql/prompt_preview_agent.sql
var queryPromptPreviewAgent string

// queryPromptPreviewAttachmentItems embeds ordered artifact refs for an attachment set with exact
// organization/project, revision, filename, media type, size, digest, source and content matches.
// Only CLEAN/ACTIVE artifacts are eligible; the caller separately checks item cardinality and
// actor access before materializing artifacts.
// queryPromptPreviewAttachmentItems встраивает упорядоченные artifact refs attachment set с точным
// совпадением organization/project, revision, filename, media type, size, digest, source и
// content. Пригодны только CLEAN/ACTIVE artifacts; вызывающий отдельно сверяет item cardinality и
// доступ субъекта до материализации artifacts.
//
//go:embed sql/prompt_preview_attachment_items.sql
var queryPromptPreviewAttachmentItems string

// GetPromptPreviewContextSnapshot validates target/basic input, resolves the principal and opens a
// five-second ReadOnly RepeatableRead snapshot. It assigns prospective mode server-side and uses
// the same scope as viewer and execution actor, then commits before returning the materialization.
// Validation, authorization and backend errors remain distinct; preview creates no Run, Turn,
// grant or runtime revision.
// GetPromptPreviewContextSnapshot проверяет target/basic input, разрешает principal и открывает
// ReadOnly RepeatableRead snapshot с бюджетом пять секунд. Назначает prospective mode на сервере и
// использует один scope для viewer и execution actor, затем требует commit до возврата
// materialization. Ошибки валидации, полномочий и backend различаются; preview не создаёт Run,
// Turn, grant или runtime revision.
func (repository *Repository) GetPromptPreviewContextSnapshot(ctx context.Context, principal value.Principal,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if (kind != promptservice.TargetAgent && kind != promptservice.TargetWorkflowStage && kind != promptservice.TargetSessionContinuation) || ref == "" ||
		input.ExpectedAgentVersion < 0 || input.ExpectedWorkflowVersion < 0 || !validBoundedRunInput(input.Input) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Prospective mode назначается только этому read port, не полем caller.
	// ScopeOnly declared-template eligibility сохраняет прежнюю семантику.
	snapshot, err := repository.promptPreviewContextForActorWithModeTx(ctx, tx, current, current, kind, ref, input, true)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	return snapshot, nil
}

// promptPreviewContextTx delegates preview assembly inside the caller transaction using current
// scope for both viewer and execution actor. The downstream wrapper selects nonprospective mode;
// this forwarding method does not acquire a transaction or change authority.
// promptPreviewContextTx делегирует сборку preview в транзакции вызывающего, используя current
// scope для viewer и execution actor. Следующая обёртка выбирает nonprospective mode; метод не
// открывает транзакцию и не меняет полномочия.
func (repository *Repository) promptPreviewContextTx(ctx context.Context, tx pgx.Tx, current scope,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptPreviewContextForActorTx(ctx, tx, current, current, kind, ref, input)
}

// promptPreviewContextForActorTx delegates nonprospective assembly while retaining separate viewer
// and execution scopes supplied by trusted callers. Viewer read eligibility remains independent of
// the owner-assigned execution actor; the wrapper does not derive either identity from input refs.
// promptPreviewContextForActorTx делегирует nonprospective assembly с отдельными viewer и
// execution scopes от доверенных callers. Доступ viewer к чтению остаётся независимым от
// назначенного владельцем execution actor; обёртка не выводит identity из input refs.
func (repository *Repository) promptPreviewContextForActorTx(ctx context.Context, tx pgx.Tx, current, execution scope,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptPreviewContextForActorWithModeTx(ctx, tx, current, execution, kind, ref, input, false)
}

// promptPreviewContextForActorWithModeTx dispatches session continuation to its specialized path;
// otherwise validates bounded target/task/input, exact workflow revision/stage and optional
// version pins, then enforces viewer agent/project access. It composes agent/workflow context,
// caller task/input, unavailable-variable markers and optional attachments, narrows integration
// scope and hashes actor plus the complete snapshot into the context pin. Prospective workflow
// mode permits missing inputs with explicit unavailability. It reads only in the caller
// transaction and issues no execution authority.
// promptPreviewContextForActorWithModeTx передаёт session continuation специализированному пути;
// иначе проверяет ограниченные target/task/input, точные workflow revision/stage и необязательные
// version pins, затем требует viewer agent/project access. Собирает agent/workflow context,
// task/input вызывающего, маркеры недоступных переменных и optional attachments, сужает
// integration scope и хеширует actor с полным snapshot в context pin. Prospective workflow mode
// разрешает отсутствующие inputs с явной недоступностью. Только читает в транзакции вызывающего и
// не выдаёт execution authority.
func (repository *Repository) promptPreviewContextForActorWithModeTx(ctx context.Context, tx pgx.Tx, current, execution scope,
	kind, ref string, input query.PromptPreviewContext, prospective bool,
) (entity.PromptMaterializationSnapshot, error) {
	if kind == promptservice.TargetSessionContinuation {
		return repository.promptContinuationPreviewForActorTx(ctx, tx, current, execution, ref, input)
	}
	if (kind != promptservice.TargetAgent && kind != promptservice.TargetWorkflowStage) || ref == "" ||
		input.ExpectedAgentVersion < 0 || input.ExpectedAgentVersion > 9007199254740991 || input.ExpectedWorkflowVersion < 0 || input.ExpectedWorkflowVersion > 9007199254740991 ||
		!validBoundedRunInput(input.Input) || len(input.Task) > 64<<10 || !utf8.ValidString(input.Task) || strings.ContainsRune(input.Task, 0) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	var workflow entity.Workflow
	var err error
	var version *entity.WorkflowVersion
	var step *entity.WorkflowStep
	agentRef := ref
	if kind == promptservice.TargetWorkflowStage {
		if input.WorkflowStageKey == "" {
			return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
		}
		_, target, resolveErr := repository.resolveCommandTarget(ctx, tx, current, "workflow.view", "WORKFLOW", ref, "")
		if resolveErr != nil || repository.requireAccess(ctx, tx, current, "workflow.view", target) != nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
		workflow, err = scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, current.organizationID, ref, current.role, current.actorID), true)
		if err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		if input.ExpectedWorkflowVersion > 0 && input.ExpectedWorkflowVersion != workflow.Version {
			return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
		}
		for _, candidate := range []*entity.WorkflowVersion{workflow.Draft, workflow.Published} {
			if candidate != nil && candidate.Ref == input.WorkflowRevisionRef {
				version = candidate
				break
			}
		}
		if input.WorkflowRevisionRef == "" {
			version = workflow.Draft
		}
		if version == nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
		}
		if selected, ok := promptWorkflowStep(*version, input.WorkflowStageKey); ok {
			step = &selected
		}
		if step == nil || !validPromptWorkflowInput(version.Inputs, input.Input, prospective || input.ScopeOnly) {
			return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
		}
		agentRef = step.AgentRef
	} else if input.WorkflowRevisionRef != "" || input.WorkflowStageKey != "" || input.ExpectedWorkflowVersion != 0 {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	if input.AgentRef != "" && input.AgentRef != agentRef {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agentRef)
	if err != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return entity.PromptMaterializationSnapshot{}, errs.ErrForbidden
	}
	snapshot, err := repository.promptPreviewAgentForActorTx(ctx, tx, current, execution, agentRef)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if input.ExpectedAgentVersion > 0 && input.ExpectedAgentVersion != snapshot.ContextPin.AgentVersion {
		return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
	}
	if kind == promptservice.TargetWorkflowStage {
		if snapshot.ProjectRef != workflow.ProjectRef {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
		applyWorkflowPromptContext(&snapshot, workflow.Ref, workflow.Version, *version, *step)
		if err := repository.hydrateWorkflowPromptTemplateTx(ctx, tx, current, &snapshot); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		if err := refreshPromptFileScopes(&snapshot); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		snapshot.Variables["task"] = step.Instructions
		snapshot.WorkflowStage = step.Name
	}
	snapshot.StructuredVariables["input"].(map[string]any)["values"] = input.Input
	if kind == promptservice.TargetWorkflowStage && prospective {
		markUnavailablePromptWorkflowInputs(&snapshot, version.Inputs, input.Input)
	}
	if input.Task != "" {
		snapshot.Variables["task"] = input.Task
	} else {
		snapshot.UnavailableVariables["task"] = "RUNTIME_CONTEXT_REQUIRED"
	}
	narrowPromptIntegrationScope(&snapshot)
	if input.AttachmentSetRef != "" {
		if err := repository.promptPreviewAttachmentsTx(ctx, tx, current, target.projectID, &snapshot, input.AttachmentSetRef); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
	}
	// Context pin исключает будущие server-issued Run/Turn refs, но связывает
	// весь выбранный immutable input и фактическую authority текущего actor.
	raw, err := json.Marshal(struct {
		Actor    string
		Snapshot entity.PromptMaterializationSnapshot
	}{current.actorRef, snapshot})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	snapshot.ContextPin.Digest = hex.EncodeToString(digest[:])
	return snapshot, nil
}

// validPromptWorkflowInput uses the runtime workflow-input validator directly in normal mode.
// Prospective mode filters declared fields to supplied keys before the same validator, allowing
// only absent fields to defer their required checks; supplied values and unknown input keys retain
// runtime validation.
// validPromptWorkflowInput использует runtime workflow-input validator напрямую в обычном режиме.
// Prospective mode перед тем же validator оставляет declared fields только для заданных keys,
// откладывая required checks лишь отсутствующих полей; заданные значения и неизвестные input keys
// сохраняют runtime validation.
func validPromptWorkflowInput(fields []entity.WorkflowInputField, input map[string]any, prospective bool) bool {
	if !prospective {
		return validWorkflowRunInput(fields, input)
	}
	// Проверяем все предоставленные значения тем же runtime validator.
	// Только отсутствующие поля не требуются до создания реального Run.
	provided := make([]entity.WorkflowInputField, 0, len(input))
	for _, field := range fields {
		if _, exists := input[field.Key]; exists {
			provided = append(provided, field)
		}
	}
	return validWorkflowRunInput(provided, input)
}

// markUnavailablePromptWorkflowInputs marks input.values and each omitted declared
// input.values.key as RUNTIME_CONTEXT_REQUIRED in the existing snapshot map. It mutates only
// availability metadata and neither inserts input values nor allocates a missing map; callers must
// initialize the snapshot.
// markUnavailablePromptWorkflowInputs помечает input.values и каждый пропущенный declared
// input.values.key как RUNTIME_CONTEXT_REQUIRED в существующей snapshot map. Меняет только
// availability metadata, не вставляет input values и не создаёт отсутствующую map; вызывающий
// обязан инициализировать snapshot.
func markUnavailablePromptWorkflowInputs(snapshot *entity.PromptMaterializationSnapshot, fields []entity.WorkflowInputField, input map[string]any) {
	for _, field := range fields {
		if _, exists := input[field.Key]; !exists {
			snapshot.UnavailableVariables["input.values"] = "RUNTIME_CONTEXT_REQUIRED"
			snapshot.UnavailableVariables["input.values."+field.Key] = "RUNTIME_CONTEXT_REQUIRED"
		}
	}
}

// promptPreviewAttachmentsTx selects RUN_INPUT, WORKFLOW_INPUT or SESSION_TURN purpose from the
// trusted snapshot target, resolves the finalized set under organization/project/purpose and
// materializes it with current-turn provenance. Lookup failures are hidden as NotFound; success
// pins set ref/manifest digest and refreshes file scopes. It neither finalizes a set nor grants
// artifact download.
// promptPreviewAttachmentsTx выбирает purpose RUN_INPUT, WORKFLOW_INPUT либо SESSION_TURN из
// доверенной snapshot target, разрешает finalized set внутри organization/project/purpose и
// материализует с current-turn provenance. Lookup failures скрываются как NotFound; успех
// закрепляет set ref/manifest digest и обновляет file scopes. Не финализирует set и не выдаёт
// artifact download grant.
func (repository *Repository) promptPreviewAttachmentsTx(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	snapshot *entity.PromptMaterializationSnapshot, ref string,
) error {
	purpose := "RUN_INPUT"
	if snapshot.TargetKind == promptservice.TargetWorkflowStage {
		purpose = "WORKFLOW_INPUT"
	}
	if snapshot.TargetKind == promptservice.TargetSessionContinuation {
		purpose = "SESSION_TURN"
	}
	var set sealedAttachmentSet
	if err := tx.QueryRow(ctx, queryAttachmentSetsResolveFinalized, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"project_id": projectID, "attachment_set_ref": ref, "purpose": purpose}).Scan(&set.ID, &set.Ref, &set.ManifestDigest, &set.Purpose, &set.ItemCount, &set.TotalSizeBytes); err != nil {
		return errs.ErrNotFound
	}
	if err := repository.promptPreviewAttachmentSetTx(ctx, tx, current, projectID, snapshot, set, false); err != nil {
		return err
	}
	snapshot.ContextPin.AttachmentSetRef, snapshot.ContextPin.AttachmentManifestDigest = set.Ref, set.ManifestDigest
	return refreshPromptFileScopes(snapshot)
}

// promptPreviewAttachmentSetTx collects exact eligible artifact refs, closes rows on all scan
// paths and requires cardinality to match the sealed set before viewer authorization. NotFound
// during authorization disables file scopes and refreshes them; other errors propagate. Eligible
// items append in place with current-turn or historical session provenance and a total cap of
// 2048. Failure can leave the in-memory snapshot modified, so the caller must discard it; no
// durable writes occur here.
// promptPreviewAttachmentSetTx собирает точные eligible artifact refs, закрывает rows на всех scan
// paths и требует совпадение cardinality с sealed set до viewer authorization. NotFound при
// авторизации отключает file scopes и обновляет их; прочие ошибки передаются выше. Пригодные items
// добавляются на месте с current-turn либо historical session provenance и общим пределом 2048.
// Ошибка может оставить in-memory snapshot изменённым, поэтому вызывающий обязан отбросить его;
// durable writes здесь нет.
func (repository *Repository) promptPreviewAttachmentSetTx(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	snapshot *entity.PromptMaterializationSnapshot, set sealedAttachmentSet, historical bool,
) error {
	rows, err := tx.Query(ctx, queryPromptPreviewAttachmentItems, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "attachment_set_id": set.ID})
	if err != nil {
		return errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || int64(len(refs)) != set.ItemCount {
		return errs.ErrConflict
	}
	if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			disablePromptFileScopes(snapshot, "PERMISSION_REQUIRED")
			return refreshPromptFileScopes(snapshot)
		}
		return err
	}
	items, err := repository.listAttachmentSetItemsTx(ctx, tx, set.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		item.AttachmentSetRef, item.AttachmentPurpose, item.Provenance = set.Ref, set.Purpose, "CURRENT_TURN"
		if historical {
			item.Scope, item.Provenance = runtimecontract.AttachmentScopeSession, "SESSION_HISTORY"
		}
		snapshot.Artifacts = append(snapshot.Artifacts, item.RunnerInputArtifact)
	}
	if len(snapshot.Artifacts) > 2048 {
		return errs.ErrConflict
	}
	return nil
}

// promptPreviewAgentTx forwards agent preview assembly in the existing transaction with identical
// viewer/execution scope. It preserves the specialized result and errors and does not add
// permission or transaction ownership.
// promptPreviewAgentTx передаёт сборку agent preview в существующей транзакции с одинаковым
// viewer/execution scope. Сохраняет специализированные результат и ошибки, не добавляя permission
// или владение транзакцией.
func (repository *Repository) promptPreviewAgentTx(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptPreviewAgentForActorTx(ctx, tx, current, current, ref)
}

// promptPreviewAgentForActorTx reads the current agent template, assistant core and
// runtime/environment pins, builds scalar/structured variables and loads knowledge under the
// execution actor. A distinct viewer must separately authorize the resulting artifact refs. It
// resolves actor/agent/connection capabilities and builds integration scope while marking future
// run/session/turn/node refs unavailable. The caller must already establish target read access;
// this assembler does not start execution or persist the returned context.
// promptPreviewAgentForActorTx читает текущие agent template, assistant core и runtime/environment
// pins, строит scalar/structured variables и загружает knowledge от execution actor. Отличающийся
// viewer обязан отдельно получить доступ к итоговым artifact refs. Разрешает
// actor/agent/connection capabilities и строит integration scope, отмечая будущие
// run/session/turn/node refs недоступными. Вызывающий обязан заранее установить target read
// access; сборщик не запускает выполнение и не сохраняет возвращаемый context.
func (repository *Repository) promptPreviewAgentForActorTx(ctx context.Context, tx pgx.Tx, current, execution scope, ref string) (entity.PromptMaterializationSnapshot, error) {
	snapshot := entity.PromptMaterializationSnapshot{ServiceTemplateRevision: promptservice.ServiceTemplateRevision,
		TargetKind: promptservice.TargetAgent, TargetRef: ref, Variables: map[string]string{}}
	snapshot.UnavailableVariables = map[string]string{"run.ref": "RUNTIME_CONTEXT_REQUIRED", "session.ref": "RUNTIME_CONTEXT_REQUIRED", "turn.ref": "RUNTIME_CONTEXT_REQUIRED", "node.ref": "RUNTIME_CONTEXT_REQUIRED"}
	var name, purpose, projectName, organizationName string
	err := tx.QueryRow(ctx, queryPromptPreviewAgent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": ref}).Scan(
		&snapshot.ContextPin.AgentRef, &name, &purpose, &snapshot.ContextPin.AgentVersion, &snapshot.ProjectRef, &projectName, &snapshot.Locale,
		&snapshot.TemplateRef, &snapshot.TemplateContent, &snapshot.TemplateDigest, &snapshot.AgentCapabilities, &organizationName)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	if err := repository.hydrateAssistantCoreTx(ctx, tx, current, &snapshot); err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if err := bindPlatformInstructions(&snapshot); err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, ref)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	snapshot.ContextPin.RuntimeConfigurationRef, snapshot.ContextPin.RuntimeConfigurationDigest = view.Configuration.Ref, view.Configuration.Digest
	snapshot.ContextPin.EnvironmentBindingRef, snapshot.ContextPin.EnvironmentBindingVersion = view.EnvironmentBinding.Ref, view.EnvironmentBinding.Version
	snapshot.ContextPin.EnvironmentVersionRef, snapshot.ContextPin.EnvironmentDigest = view.Environment.CurrentVersion.Ref, view.Environment.CurrentVersion.Digest
	snapshot.Variables["agent.ref"], snapshot.Variables["agent.name"], snapshot.Variables["project.ref"], snapshot.Variables["project.name"] = ref, name, snapshot.ProjectRef, projectName
	snapshot.Variables["organization.ref"], snapshot.Variables["user.ref"], snapshot.Variables["task"] = current.organizationRef, execution.actorRef, purpose
	snapshot.Variables["organization.name"] = organizationName
	snapshot.Variables["user.name"] = execution.actorName
	snapshot.Variables["environment.ref"] = view.Environment.CurrentVersion.Ref
	tools := make([]runtimecontract.RuntimeEnvironmentTool, 0, len(view.Environment.CurrentVersion.Tools))
	for _, tool := range view.Environment.CurrentVersion.Tools {
		tools = append(tools, runtimecontract.RuntimeEnvironmentTool{Name: tool.Name, Description: tool.Description, Command: tool.Command})
	}
	image := runtimecontract.RuntimeEnvironmentImage{Reference: view.Environment.CurrentVersion.Image.Reference, Digest: view.Environment.CurrentVersion.Image.Digest}
	snapshot.StructuredVariables, err = promptStructuredVariables(nil, tools, image, view.Environment.CurrentVersion.Ref, "", "")
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	if err := repository.promptPreviewKnowledgeTx(ctx, tx, execution, &snapshot); err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if execution.actorID != current.actorID {
		refs := make([]string, 0, len(snapshot.Artifacts))
		for _, item := range snapshot.Artifacts {
			refs = append(refs, item.Ref)
		}
		if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
	}
	var grants []map[string]string
	snapshot.UserCapabilities, grants, err = repository.agentCapabilityAuthority(ctx, tx, execution, snapshot.ProjectRef, ref, snapshot.AgentCapabilities)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	for _, grant := range grants {
		if key := grant["capabilityKey"]; key != "" {
			snapshot.ConnectionCapabilities = append(snapshot.ConnectionCapabilities, key)
		}
	}
	snapshot.StructuredVariables["integrations"] = promptIntegrationScope(grants, promptservice.Intersection(snapshot.UserCapabilities, promptservice.Union(snapshot.AgentCapabilities, snapshot.ConnectionCapabilities)))
	return snapshot, nil
}
