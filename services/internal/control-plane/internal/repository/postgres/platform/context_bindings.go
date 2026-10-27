package platform

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// populateContextBindings appends visible active memory/published skill binding metadata from
// owner SQL to the supplied view, rejecting unknown kinds or more than128 rows.
// Query/scan/iteration failures are Unavailable. It reads no file bytes and does not reset
// existing slices; callers discard partially populated views on failure.
// populateContextBindings добавляет из owner SQL метаданные видимых active memory и published
// skill bindings в переданный view, отклоняя неизвестные kinds и более128 строк. Ошибки
// query/scan/iteration дают Unavailable. Не читает байты файлов и не очищает прежние slices;
// caller отбрасывает частично заполненный view при ошибке.
func (repository *Repository) populateContextBindings(ctx context.Context, tx pgx.Tx, current scope, ref string, view *entity.AgentRuntimeConfigurationView) error {
	rows, err := tx.Query(ctx, queryContextBindingsList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "agent_ref": ref, "evaluated_at": time.Now().UTC()})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var kind string
		var binding entity.AgentContextBinding
		if rows.Scan(&kind, &binding.Ref, &binding.Version, &binding.AgentRef, &binding.ResourceRef, &binding.RevisionRef, &binding.Digest) != nil {
			return errs.ErrUnavailable
		}
		count++
		if count > 128 {
			return errs.ErrUnavailable
		}
		switch kind {
		case "SKILL":
			view.SkillBindings = append(view.SkillBindings, binding)
		case "MEMORY":
			view.MemoryBindings = append(view.MemoryBindings, binding)
		default:
			return errs.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// isMemoryBinding recognizes only typed memory bind/unbind command kinds; false selects the skill
// branch in already constrained callers and does not validate an arbitrary command.
// isMemoryBinding распознаёт только типизированные команды memory bind/unbind; false выбирает
// skill-ветку в уже ограниченных callers и не проверяет произвольную команду.
func isMemoryBinding(kind command.Kind) bool {
	return kind == command.BindAgentMemoryRecord || kind == command.UnbindAgentMemoryRecord
}

// authorizeContextResource rechecks memory or skill resource access and resolves the exact
// requested revision. Memory provenance with a source ref additionally requires run.view; each
// skill file requires artifact.view. Missing revisions are NotFound, malformed stored JSON is
// Unavailable; this path does not scan bytes or change bindings.
// authorizeContextResource повторно проверяет доступ к memory либо skill ресурсу и разрешает
// точную запрошенную ревизию. Memory provenance с source ref дополнительно требует run.view,
// каждый skill file — artifact.view. Отсутствующая ревизия даёт NotFound, повреждённый JSON —
// Unavailable; путь не сканирует байты и не меняет bindings.
func (repository *Repository) authorizeContextResource(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.AgentContextBindingInput) error {
	if isMemoryBinding(input.Kind) {
		record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, payload.ResourceRef))
		if err != nil {
			return err
		}
		if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(ctx, queryMemoryRevisionGet, current.organizationID, payload.ResourceRef, payload.RevisionRef).Scan(&raw); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		var revision entity.MemoryRecordRevision
		if json.Unmarshal(raw, &revision) != nil {
			return errs.ErrUnavailable
		}
		if revision.Provenance.SourceRef != "" {
			return repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: revision.Provenance.SourceRef})
		}
		return nil
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.ResourceRef))
	if err != nil {
		return err
	}
	if err := repository.skillBundleAccess(ctx, tx, current, bundle); err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryContextBindingSkillFiles, current.organizationID, payload.ResourceRef, payload.RevisionRef).Scan(&raw); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	var files []entity.SkillBundleFile
	if json.Unmarshal(raw, &files) != nil {
		return errs.ErrUnavailable
	}
	for _, file := range files {
		if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
			return err
		}
	}
	return nil
}

// changeContextBinding locks a project agent and checks agent/binding OCC, same-project owner
// eligibility and exact resource revision. Bind requires current eligibility, skill byte
// validation and a maximum128 enabled bindings; unbind requires the currently bound revision. It
// writes binding plus agent version in the caller transaction and returns AGENT_CHANGED metadata;
// authorization and final audit/event/receipt commit stay with the command owner.
// changeContextBinding блокирует project agent и проверяет agent/binding OCC, допустимость
// владельца в том же проекте и точную resource revision. Bind требует текущей допустимости,
// проверки skill bytes и максимум128 enabled bindings; unbind требует текущую связанную ревизию.
// Записывает binding и agent version в переданной транзакции и возвращает AGENT_CHANGED metadata;
// авторизация и окончательный audit/event/receipt commit остаются у владельца команды.
func (repository *Repository) changeContextBinding(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentContextBindingInput)
	if !ok || payload.ExpectedBindingVersion < 0 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, current, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if agent.projectID == "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if agent.agentVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	memory := isMemoryBinding(input.Kind)
	bind := input.Kind == command.BindAgentMemoryRecord || input.Kind == command.BindAgentSkillBundle
	statement := queryContextBindingSkillTarget
	if memory {
		statement = queryContextBindingMemoryTarget
	}
	var resourceID, projectID, ownerAgentID, revisionID, revisionRef, digest string
	var eligible bool
	if err := tx.QueryRow(ctx, statement, current.organizationID, payload.ResourceRef, payload.RevisionRef).Scan(&resourceID, &projectID, &ownerAgentID, &revisionID, &revisionRef, &digest, &eligible); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if projectID != agent.projectID || ownerAgentID != "" && ownerAgentID != agent.id {
		return commandOutcome{}, errs.ErrForbidden
	}
	if bind && !eligible {
		return commandOutcome{}, errs.ErrConflict
	}
	if bind && !memory {
		var raw []byte
		if err := tx.QueryRow(ctx, queryContextBindingSkillFiles, current.organizationID, payload.ResourceRef, payload.RevisionRef).Scan(&raw); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var files []entity.SkillBundleFile
		if json.Unmarshal(raw, &files) != nil || len(files) == 0 {
			return commandOutcome{}, errs.ErrConflict
		}
		for _, file := range files {
			if _, err := repository.readSkillFile(ctx, tx, current, projectID, file); err != nil {
				return commandOutcome{}, err
			}
		}
	}
	memoryID, memoryRevisionID, skillID, skillRevisionID := "", "", "", ""
	if memory {
		memoryID, memoryRevisionID = resourceID, revisionID
	} else {
		skillID, skillRevisionID = resourceID, revisionID
	}
	var bindingRef, boundRevisionRef string
	var bindingVersion int64
	var enabled bool
	err = tx.QueryRow(ctx, queryContextBindingGet, current.organizationID, agent.id, memoryID, skillID).Scan(&bindingRef, &bindingVersion, &enabled, &boundRevisionRef)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if enabled && payload.ExpectedBindingVersion != bindingVersion || !enabled && payload.ExpectedBindingVersion != 0 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if !bind && (!enabled || boundRevisionRef != revisionRef) {
		return commandOutcome{}, errs.ErrConflict
	}
	if bind && !enabled {
		var count int64
		if err := tx.QueryRow(ctx, queryContextBindingCount, current.organizationID, agent.id).Scan(&count); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if count >= 128 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if exists {
		err = tx.QueryRow(ctx, queryContextBindingUpdate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "ref": bindingRef, "version": bindingVersion, "enabled": bind, "memory_revision_id": memoryRevisionID, "skill_revision_id": skillRevisionID}).Scan(&bindingVersion)
	} else {
		bindingRef, _ = newRef("ctxb")
		err = tx.QueryRow(ctx, queryContextBindingInsert, pgx.StrictNamedArgs{"organization_id": current.organizationID, "ref": bindingRef, "project_id": projectID, "agent_id": agent.id, "actor_id": current.actorID, "memory_id": memoryID, "memory_revision_id": memoryRevisionID, "skill_id": skillID, "skill_revision_id": skillRevisionID}).Scan(&bindingVersion)
	}
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt, agent.id); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{ContextBinding: &entity.AgentContextBinding{Ref: bindingRef, Version: bindingVersion, AgentRef: payload.AgentRef, ResourceRef: payload.ResourceRef, RevisionRef: revisionRef, Digest: digest}},
		resourceKind: "AGENT", resourceRef: payload.AgentRef, projectID: projectID, projectRef: agent.projectRef, summary: "i18n:AGENT_CONTEXT_BINDING_CHANGED", platformEvent: "AGENT_CHANGED"}, nil
}
