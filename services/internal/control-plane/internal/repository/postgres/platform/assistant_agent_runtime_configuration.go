package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// Context и agent.view проверены до чтения; штатный target resolver сохраняет
// точную project authority. Этот query не создаёт grants, audit или events.
func (repository *Repository) assistantAgentRuntimeConfigurationTx(ctx context.Context, tx pgx.Tx, current scope, agent entity.Agent) (entity.AssistantAgentConfiguration, error) {
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agent.Ref)
	if err != nil {
		return entity.AssistantAgentConfiguration{}, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return entity.AssistantAgentConfiguration{}, errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return entity.AssistantAgentConfiguration{}, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, agent.Ref)
	if err != nil {
		return entity.AssistantAgentConfiguration{}, err
	}
	if view.AgentVersion != agent.Version || view.Configuration.AgentRef != agent.Ref || view.Environment.ProjectRef != agent.ProjectRef || view.Environment.OrganizationRef != current.organizationRef || view.Environment.ScopeKind != "PROJECT" {
		return entity.AssistantAgentConfiguration{}, errs.ErrNotFound
	}
	image := view.Environment.CurrentVersion.Image
	artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, image.ArtifactRef))
	if err != nil || artifact.AdmissionVerdict != "ACCEPTED" || artifact.PromotionState != "PROMOTED" || artifact.ScopeKind != "PROJECT" || artifact.ProjectRef != agent.ProjectRef || artifact.OrganizationRef != current.organizationRef || artifact.ManifestDigest != image.Digest || artifact.PromotedReference != image.Reference || artifact.RecipeRef != image.RecipeRef || image.RecipeGeneration < 1 || artifact.RecipeGeneration != uint64(image.RecipeGeneration) {
		return entity.AssistantAgentConfiguration{}, errs.ErrUnavailable
	}
	raw, err := assistantAgentRuntimeConfigurationJSON(agent, view, artifact.ToolInventory, artifact.ToolInventorySHA256)
	if err != nil {
		return entity.AssistantAgentConfiguration{}, err
	}
	digest := sha256.Sum256(raw)
	return entity.AssistantAgentConfiguration{AgentRef: agent.Ref, ProjectRef: agent.ProjectRef, Version: agent.Version, ConfigurationJSON: raw, ConfigurationSHA256: hex.EncodeToString(digest[:])}, nil
}

// inventorySourceDigest — trusted receipt exact исходных bytes: scanner уже
// проверил raw SHA и artifact/provenance binding. Проекция имеет отдельный SHA
// canonical whitelist bytes и не сравнивает их с порядком полей source JSON.
func assistantAgentRuntimeConfigurationJSON(agent entity.Agent, view entity.AgentRuntimeConfigurationView, inventory *runtimecontract.ImageToolInventory, inventorySourceDigest string) ([]byte, error) {
	binding, environment := view.EnvironmentBinding, view.Environment
	image := environment.CurrentVersion.Image
	if binding.AgentRef != agent.Ref || binding.EnvironmentRef != environment.Ref || binding.VersionRef != environment.CurrentVersion.Ref || binding.Version < 1 || environment.Version < 1 || environment.CurrentVersion.Revision < 1 || inventory == nil || inventory.Validate() != nil || inventory.ImageDigest != image.Digest {
		return nil, errs.ErrUnavailable
	}
	if !exactSHA256(inventorySourceDigest) {
		return nil, errs.ErrUnavailable
	}
	// Настроенные инструменты ENV и подтверждённые бинарники image — разные наборы.
	tools := make([]string, 0, len(environment.CurrentVersion.Tools))
	for _, tool := range environment.CurrentVersion.Tools {
		tools = append(tools, tool.Name)
	}
	snapshot := map[string]any{"agent_ref": agent.Ref, "project_ref": agent.ProjectRef, "version": agent.Version,
		"binding_ref": binding.Ref, "binding_version": binding.Version, "binding_digest": binding.Digest,
		"environment_ref": environment.Ref, "environment_name": environment.Name, "environment_version": environment.Version,
		"published_version_ref": environment.CurrentVersion.Ref, "published_revision": environment.CurrentVersion.Revision, "published_digest": environment.CurrentVersion.Digest,
		"image": image, "configured_tools": tools, "verified_tool_inventory": inventory}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 1<<20 {
		return nil, errs.ErrUnavailable
	}
	// Канонические bytes используют одинаковый порядок ключей на всех глубинах.
	var canonical map[string]any
	if json.Unmarshal(raw, &canonical) != nil {
		return nil, errs.ErrUnavailable
	}
	inventoryRaw, err := json.Marshal(canonical["verified_tool_inventory"])
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	canonical["verified_tool_inventory_sha256"] = runtimecontract.ImageInventorySHA256(inventoryRaw)
	return json.Marshal(canonical)
}
