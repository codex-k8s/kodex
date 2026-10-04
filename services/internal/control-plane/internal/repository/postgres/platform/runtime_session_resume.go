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

//go:embed sql/runtime_session_resume_snapshot.sql
var queryRuntimeSessionResumeSnapshot string

// Возобновление использует только подтверждённый storage того же session и его
// immutable revision. Текущий task/history/lease не является конфигурацией.
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

// Новый owner health receipt не меняет семантику tools. Его свежесть проверяется
// перед claim; resume сохраняет exact package/config/credential/grant pins,
// но не сравнивает время и идентификатор повторного наблюдения той же системы.
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
