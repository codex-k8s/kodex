package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeSessionResumeSnapshot embeds the confirmed LIVE storage snapshot for an exact
// tenant/session/provider codex-session identity with nonempty source size/digest. The linked
// immutable RuntimeRevision, not current task/history/lease, supplies previous configuration under
// a shared storage lock.
// queryRuntimeSessionResumeSnapshot встраивает подтверждённый LIVE storage snapshot точных
// организации, сессии и provider codex-session identity с непустыми размером и дайджестом
// источника. Прежнюю конфигурацию под разделяемой блокировкой storage задаёт связанная
// неизменяемая RuntimeRevision, а не текущие task/history/lease.
//
//go:embed sql/runtime_session_resume_snapshot.sql
var queryRuntimeSessionResumeSnapshot string

// runtimeSessionResumeCompatible requires recognized assistant scope, OpenAI runtime and mandatory
// provenance in both snapshots, then JSON-normalizes and compares the closed
// configuration/dependency field set plus initiator and instruction dependencies. It excludes
// task/history/lease and normalizes MCP observation-only changes. Missing fields or
// parse/validation errors return false; compatibility does not grant access or prove storage
// bytes.
// runtimeSessionResumeCompatible требует известную область ассистента, OpenAI runtime и
// обязательное происхождение обоих снимков, затем JSON-нормализует и сравнивает закрытый набор
// конфигурации и зависимостей с инициатором и инструкциями. Исключает task/history/lease и
// нормализует только наблюдательные изменения MCP. Отсутствие полей и ошибки разбора либо проверки
// дают false; совместимость не выдаёт доступ и не доказывает storage bytes.
func runtimeSessionResumeCompatible(previous, current map[string]any) bool {
	for _, snapshot := range []map[string]any{previous, current} {
		switch stringMap(snapshot, "assistantScope") {
		case "SYSTEM":
			if stringMap(snapshot, "assistantProfileRef") != "" {
				return false
			}
		case "PROJECT":
			if stringMap(snapshot, "assistantProfileRef") == "" || stringMap(snapshot, "projectRef") == "" {
				return false
			}
		case "NONE":
			if stringMap(snapshot, "projectRef") == "" || stringMap(snapshot, "assistantProfileRef") != "" {
				return false
			}
		default:
			return false
		}
		if stringMap(snapshot, "runtimeProvider") != "openai" || stringMap(snapshot, "runtimeKey") == "" ||
			stringMap(snapshot, "runtimeRevision") == "" || runtimeRevisionMapInt64(snapshot, "roleRuntimeContractRevision") < 1 {
			return false
		}
		for _, field := range []string{"organizationRef", "sessionRef", "agentRef", "providerAccountRef", "runtimeModel", "promptTemplateRef", "promptTemplateDigest", "roleRuntimeContractSHA256", "imageManifestDigest", "runtimeEnvironmentDigest"} {
			if stringMap(snapshot, field) == "" {
				return false
			}
		}
	}
	fields := []string{
		"organizationRef", "projectRef", "sessionRef", "agentRef", "stableKey", "assistantScope", "assistantProfileRef",
		"runtimeKey", "runtimeRevision", "runtimeProvider", "runtimeModel", "reasoningMode", "effectiveReasoningEffort",
		"providerAccountRef", "providerCredentialRevisionRef", "providerCredentialRevisionNumber", "providerCredentialSHA256",
		"instructionRef", "promptTemplateRef", "promptTemplateDigest", "promptServiceTemplateRevision", "promptServiceTemplateDigest",
		"roleDefinitionRef", "roleImageRecipeRef", "roleImageArtifactRef", "roleImageRecipeGeneration", "imageReference", "imageManifestDigest",
		"roleRuntimeContractRevision", "roleRuntimeContractSHA256", "runtimeConfigRef", "runtimeConfigVersion", "runtimeConfigDigest",
		"providerPolicyRef", "providerPolicyVersion", "providerPolicyDigest", "providerPolicyMode", "configOverlayRef", "configOverlayVersion", "configOverlayDigest", "configOverlay",
		"runtimeEnvironmentRef", "runtimeEnvironmentVersion", "runtimeEnvironmentDigest", "environmentBindingRef", "environmentBindingVersion", "environmentBindingDigest",
		"environmentValues", "secretProjections", "environmentImage", "environmentTools", "environmentPolicy", "workspacePolicy",
		"capabilities", "integrationGrants", "managedMCPProfiles", "contextSnapshot", "promptAuthority", "knowledgeArtifactRefs", "delegationTargets",
	}
	projection := func(snapshot map[string]any) ([]byte, error) {
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		var normalized map[string]any
		if err := json.Unmarshal(encoded, &normalized); err != nil {
			return nil, err
		}
		selected := make(map[string]any, len(fields))
		for _, field := range fields {
			value, present := normalized[field]
			if !present {
				return nil, errs.ErrConflict
			}
			selected[field] = value
		}
		prompt, _ := normalized["promptSnapshot"].(map[string]any)
		variables, _ := prompt["variables"].(map[string]any)
		actor, _ := variables["user.ref"].(string)
		if actor == "" {
			return nil, errs.ErrConflict
		}
		selected["initiatorRef"] = actor
		selected["managedMCPProfiles"], err = runtimeSessionMCPDependencies(normalized)
		if err != nil {
			return nil, err
		}
		components, err := continuationComponents(normalized)
		if err != nil {
			return nil, err
		}
		selected["instructionDependencies"] = components["INSTRUCTIONS"]
		return json.Marshal(selected)
	}
	a, errA := projection(previous)
	b, errB := projection(current)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// runtimeSessionMCPDependencies validates MCP profiles against exact grants/agent/scope, then
// removes only profile digest and health test ref/generation/time from a fresh JSON projection.
// Empty profiles normalize to nil. Package/configuration/credential/grant pins remain; current
// health freshness is checked before claim rather than inferred from resume equality.
// runtimeSessionMCPDependencies проверяет MCP-профили по точным grants/agent/scope, затем удаляет
// только дайджест профиля и health test ref/generation/time из новой JSON-проекции. Пустые профили
// нормализуются в nil. Версии пакета, конфигурации, credential и grants сохраняются; текущее
// здоровье проверяется до claim, а не выводится из равенства resume.
func runtimeSessionMCPDependencies(snapshot map[string]any) (any, error) {
	raw, err := json.Marshal(snapshot["managedMCPProfiles"])
	if err != nil {
		return nil, errs.ErrConflict
	}
	var profiles []runtimecontract.ManagedMCPProfile
	if json.Unmarshal(raw, &profiles) != nil {
		return nil, errs.ErrConflict
	}
	grantJSON, err := json.Marshal(snapshot["integrationGrants"])
	if err != nil {
		return nil, errs.ErrConflict
	}
	var grants []map[string]string
	if json.Unmarshal(grantJSON, &grants) != nil {
		return nil, errs.ErrConflict
	}
	input := runtimecontract.RunnerInput{
		AgentRef: stringMap(snapshot, "agentRef"), ProjectRef: stringMap(snapshot, "projectRef"),
		AssistantScope:      runtimecontract.AssistantScope(stringMap(snapshot, "assistantScope")),
		AssistantProfileRef: stringMap(snapshot, "assistantProfileRef"),
		IntegrationGrants:   runtimeRevisionGrants(grants), ManagedMCPProfiles: profiles,
	}
	if runtimecontract.ValidateManagedMCPProfiles(input) != nil {
		return nil, errs.ErrConflict
	}
	if len(profiles) == 0 {
		return nil, nil
	}
	var selected []map[string]any
	if json.Unmarshal(raw, &selected) != nil {
		return nil, errs.ErrConflict
	}
	for _, profile := range selected {
		delete(profile, "digest")
		health, _ := profile["health"].(map[string]any)
		for _, observation := range []string{"test_ref", "generation", "checked_at"} {
			delete(health, observation)
		}
	}
	return selected, nil
}

// runtimeSessionResumeID returns no resume ID for an empty ID, missing confirmed storage,
// malformed snapshot or incompatible dependencies, preserving the cold path. Backend errors are
// Unavailable; a compatible owner snapshot returns the same ID without renewing a lease, changing
// storage or authorizing execution.
// runtimeSessionResumeID не возвращает resume ID при пустом ID, отсутствии подтверждённого
// storage, повреждённом снимке или несовместимых зависимостях, сохраняя cold path. Ошибка
// хранилища даёт Unavailable; совместимый owner snapshot возвращает тот же ID без продления
// аренды, изменения storage и разрешения исполнения.
func runtimeSessionResumeID(ctx context.Context, tx pgx.Tx, current scope, snapshot map[string]any, sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, queryRuntimeSessionResumeSnapshot, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "session_ref": stringMap(snapshot, "sessionRef"), "codex_session_id": sessionID,
	}).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	var previous map[string]any
	if json.Unmarshal(raw, &previous) != nil || !runtimeSessionResumeCompatible(previous, snapshot) {
		return "", nil
	}
	return sessionID, nil
}
