package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantAgentConfigurationCatalogTx reads the exact AGENT screen context for
// CREATE_INSTRUCTION_DRAFT or UPDATE_AGENT, falling back only on no rows. It rechecks nonsystem
// agent/project/version and selects the canonical effective publication before returning full
// bounded instruction JSON and its hash in the caller transaction.
// assistantAgentConfigurationCatalogTx читает точный AGENT screen context для
// CREATE_INSTRUCTION_DRAFT либо UPDATE_AGENT, переходя ко второму варианту только при отсутствии
// строки. Перепроверяет nonsystem agent/project/version и выбирает каноническую действующую
// публикацию перед возвратом полного ограниченного instruction JSON и его hash в транзакции
// caller.
func (repository *Repository) assistantAgentConfigurationCatalogTx(ctx context.Context, tx pgx.Tx, current scope, leaseRef string, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: "PROJECT", OrganizationRef: current.organizationRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	var contextKind, contextRef, name string
	var contextVersion, projectVersion int64
	var err error
	// Оба намерения принадлежат agent.manage; immutable и fresh проекции должны
	// содержать одно и то же допустимое намерение. Любая ошибка SQL закрывает read.
	for _, operation := range []string{"CREATE_INSTRUCTION_DRAFT", "UPDATE_AGENT"} {
		err = tx.QueryRow(ctx, queryAssistantRecipientIntegrationCatalogContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "lease_ref": leaseRef, "required_operation": operation}).Scan(&contextKind, &contextRef, &name, &contextVersion, &result.ProjectRef, &projectVersion)
		if !errors.Is(err, pgx.ErrNoRows) {
			break
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if contextKind != "AGENT" || input.EntityKind != contextKind || input.EntityRef != contextRef {
		return result, errs.ErrNotFound
	}
	agent, err := repository.readAssistantAgentConfigurationSnapshot(ctx, tx, current, contextRef)
	if err != nil {
		return result, err
	}
	if agent.System || agent.ProjectRef != result.ProjectRef || agent.Version != contextVersion {
		return result, errs.ErrNotFound
	}
	// Та же canonical publication selection, что у GetEffectivePromptTemplate:
	// managed PROMPT_TEMPLATE имеет приоритет над native instruction binding.
	var effective entity.InstructionVersion
	err = tx.QueryRow(ctx, queryManagedConfigurationEffectivePrompt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": contextRef}).Scan(&effective.Ref, &effective.Content, &effective.Digest, &effective.VersionNumber, &effective.CreatedAt, &effective.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	effective.State = "PUBLISHED"
	raw, err := assistantAgentConfigurationJSON(agent, effective)
	if err != nil {
		return result, err
	}
	result.AgentConfiguration = &entity.AssistantAgentConfiguration{AgentRef: contextRef, ProjectRef: result.ProjectRef, Version: contextVersion, ConfigurationJSON: raw, ConfigurationSHA256: assistantConfigurationSHA256(raw)}
	return result, nil
}

// readAssistantAgentConfigurationSnapshot reuses the ordinary agent.view row and instruction
// attachment readers, maps missing rows to NotFound and locked-read failures to Unavailable/retry
// classification. It marks system identity from SystemKey; grants, secrets, materialized prompts
// and runtime environments are outside this snapshot.
// readAssistantAgentConfigurationSnapshot повторно использует обычное agent.view чтение строки и
// присоединение инструкций, отображает отсутствие строки в NotFound, locked-read ошибки в
// Unavailable/retry classification. Определяет system identity по SystemKey; grants, secrets,
// materialized prompts и runtime environments не входят в snapshot.
func (repository *Repository) readAssistantAgentConfigurationSnapshot(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.Agent, error) {
	var item entity.Agent
	var canManage, canLaunch bool
	err := tx.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey, current.organizationID, ref, current.role, current.actorID).Scan(
		&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errs.ErrNotFound
	}
	if err != nil {
		return item, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	item.System = item.SystemKey != ""
	if err := repository.attachInstructionsFrom(ctx, tx, current, &item); err != nil {
		return entity.Agent{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return item, nil
}

// assistantAgentConfigurationJSON projects fourteen explicit agent/instruction fields, requiring a
// binding and valid published version/content hashes. Native content is capped at64 KiB, effective
// content at256 KiB and JSON at1 MiB; effective binding must match the selected publication. It
// preserves full templates without runtime materialization or private fields.
// assistantAgentConfigurationJSON проецирует четырнадцать явных agent/instruction fields, требуя
// binding и корректные published version/content hashes. Native content ограничено64 KiB,
// effective content256 KiB, JSON1 MiB; effective binding должен совпасть с выбранной публикацией.
// Сохраняет полные шаблоны без runtime materialization или приватных полей.
func assistantAgentConfigurationJSON(agent entity.Agent, effective entity.InstructionVersion) ([]byte, error) {
	if agent.InstructionBinding == nil {
		return nil, errs.ErrUnavailable
	}
	instruction := func(item *entity.InstructionVersion, maximumBytes int) (any, error) {
		if item == nil {
			return nil, nil
		}
		digest := sha256.Sum256([]byte(item.Content))
		if item.State != "PUBLISHED" || item.VersionNumber < 1 || len(item.Content) > maximumBytes || hex.EncodeToString(digest[:]) != item.Digest {
			return nil, errs.ErrUnavailable
		}
		return map[string]any{"ref": item.Ref, "versionNumber": item.VersionNumber, "state": item.State, "content": item.Content, "digest": item.Digest}, nil
	}
	published, err := instruction(agent.PublishedInstructions, 65536)
	if err != nil {
		return nil, err
	}
	if effective.Ref == "" || agent.InstructionBinding.Effective && effective.Ref != agent.InstructionBinding.RevisionRef {
		return nil, errs.ErrUnavailable
	}
	bound, err := instruction(&effective, 256<<10)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{
		"agentRef": agent.Ref, "projectRef": agent.ProjectRef, "name": agent.Name, "purpose": agent.Purpose,
		"roleDefinitionRef": agent.RoleDefinitionRef, "roleDefinitionName": agent.RoleDefinitionName, "roleDescription": agent.RoleDescription,
		"avatarUrl": agent.AvatarURL, "state": agent.State, "enabled": agent.Enabled,
		"runtime":               map[string]any{"key": agent.RuntimeKey, "name": agent.RuntimeName, "provider": agent.Provider, "model": agent.Model, "revision": agent.RuntimeRevision},
		"publishedInstructions": published, "effectiveInstructions": bound,
		"instructionBinding": map[string]any{"ref": agent.InstructionBinding.Ref, "version": agent.InstructionBinding.Version, "revisionRef": agent.InstructionBinding.RevisionRef, "effective": agent.InstructionBinding.Effective},
	})
	if err != nil || len(raw) > 1<<20 {
		return nil, errs.ErrUnavailable
	}
	return raw, nil
}
