package platform

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantResolvedAgentRuntimeConfigurationTx validates a metadata-only AGENT locator without
// query/pagination/account/profile selectors, reads ordinary view-eligible agent state and
// enforces a project source boundary. It rejects system agents and delegates exact runtime
// projection without requiring the instructions screen context.
// assistantResolvedAgentRuntimeConfigurationTx проверяет AGENT locator только для metadata без
// query/pagination/account/profile selectors, читает обычное view-eligible состояние agent и
// соблюдает project source boundary. Отклоняет system agents и передаёт точную runtime projection
// без требования контекста экрана инструкций.
func (repository *Repository) assistantResolvedAgentRuntimeConfigurationTx(ctx context.Context, tx pgx.Tx, current scope, sourceScope, sourceProjectRef string, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: "PROJECT", OrganizationRef: current.organizationRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	if input.EntityKind != "AGENT" || input.EntityRef == "" || input.Query != "" || input.Offset != 0 || input.AccountRef != "" || input.RuntimeProfileRef != "" {
		return result, errs.ErrInvalid
	}
	agent, err := repository.readAssistantAgentConfigurationSnapshot(ctx, tx, current, input.EntityRef)
	if err != nil {
		return result, err
	}
	if agent.System || agent.ProjectRef == "" || sourceScope == "PROJECT" && (sourceProjectRef == "" || agent.ProjectRef != sourceProjectRef) {
		return result, errs.ErrNotFound
	}
	configuration, err := repository.assistantAgentRuntimeConfigurationTx(ctx, tx, current, agent)
	if err != nil {
		return result, err
	}
	result.ProjectRef, result.AgentRuntimeConfiguration = agent.ProjectRef, &configuration
	return result, nil
}

// assistantAgentRuntimeConfigurationTx rechecks agent.view and authorityProject against the
// runtime target, agent/environment versions and exact admitted/promoted project artifact pins
// including recipe generation. It returns a bounded public projection/hash only after these
// checks, without grants, audit, events or provider execution.
// assistantAgentRuntimeConfigurationTx повторно сверяет agent.view и authorityProject с runtime
// target, agent/environment versions и точными admitted/promoted project artifact pins, включая
// recipe generation. Возвращает ограниченную публичную projection/hash после проверок, без grants,
// audit, events или исполнения провайдера.
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
	return entity.AssistantAgentConfiguration{AgentRef: agent.Ref, ProjectRef: agent.ProjectRef, Version: agent.Version, ConfigurationJSON: raw, ConfigurationSHA256: assistantConfigurationSHA256(raw)}, nil
}

// assistantAgentRuntimeConfigurationJSON validates binding/version/image inventory and
// model/resource metadata, then emits a public eighteen-field whitelist capped before canonical
// map re-encoding. Configured tool names differ from verified inventory. Trusted source inventory
// digest syntax is checked separately; delivered canonical inventory receives its own hash,
// excluding values, secret descriptors and effective config.
// assistantAgentRuntimeConfigurationJSON проверяет binding/version/image inventory и
// model/resource metadata, затем выдаёт публичный список восемнадцати разрешённых полей с
// ограничением размера до канонического map re-encoding. Настроенные имена tools отличаются от
// verified inventory. Синтаксис trusted source inventory digest проверяется отдельно; переданный
// canonical inventory получает собственный hash, исключая values, secret descriptors и effective
// config.
func assistantAgentRuntimeConfigurationJSON(agent entity.Agent, view entity.AgentRuntimeConfigurationView, inventory *runtimecontract.ImageToolInventory, inventorySourceDigest string) ([]byte, error) {
	binding, environment := view.EnvironmentBinding, view.Environment
	image := environment.CurrentVersion.Image
	if binding.AgentRef != agent.Ref || binding.EnvironmentRef != environment.Ref || binding.VersionRef != environment.CurrentVersion.Ref || binding.Version < 1 || environment.Version < 1 || environment.CurrentVersion.Revision < 1 || inventory == nil || inventory.Validate() != nil || inventory.ImageDigest != image.Digest {
		return nil, errs.ErrUnavailable
	}
	if !exactSHA256(inventorySourceDigest) {
		return nil, errs.ErrUnavailable
	}
	if view.Configuration.AgentRef != agent.Ref || view.Configuration.Ref == "" || view.Configuration.Version < 1 || !exactSHA256(view.Configuration.Digest) ||
		strings.TrimSpace(view.Configuration.Provider) == "" || strings.TrimSpace(view.Configuration.Model) == "" {
		return nil, errs.ErrUnavailable
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicyWithoutDigests()
	policy.Resources = environment.CurrentVersion.Policy.Resources
	if _, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy); err != nil {
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
		"image": image, "configured_tools": tools, "verified_tool_inventory": inventory,
		"model_configuration": map[string]any{"ref": view.Configuration.Ref, "version": view.Configuration.Version,
			"digest": view.Configuration.Digest, "provider": view.Configuration.Provider, "model": view.Configuration.Model},
		"resources": runtimecontract.RuntimeResourcePolicy{
			CPURequestMilli:            environment.CurrentVersion.Policy.Resources.CPURequestMilli,
			CPULimitMilli:              environment.CurrentVersion.Policy.Resources.CPULimitMilli,
			MemoryRequestMiB:           environment.CurrentVersion.Policy.Resources.MemoryRequestMiB,
			MemoryLimitMiB:             environment.CurrentVersion.Policy.Resources.MemoryLimitMiB,
			EphemeralStorageRequestMiB: environment.CurrentVersion.Policy.Resources.EphemeralStorageRequestMiB,
			EphemeralStorageLimitMiB:   environment.CurrentVersion.Policy.Resources.EphemeralStorageLimitMiB,
		}}
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
