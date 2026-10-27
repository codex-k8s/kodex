package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5"
)

// queryAssistantCurrentConfigurationOwnerInstructions embeds exact enabled organization
// system-assistant core revision and owner-instruction metadata lookup without runtime secret
// values.
// queryAssistantCurrentConfigurationOwnerInstructions встраивает точное чтение core revision и
// owner-instruction metadata включённого organization system-assistant без runtime secret values.
//
//go:embed sql/assistant_current_configuration__owner_instructions.sql
var queryAssistantCurrentConfigurationOwnerInstructions string

// assistantCurrentConfigurationTx reads scope-only prompt context and the current runtime view
// under already resolved own-target eligibility, verifies available artifact owner/image admission
// pins and, for SYSTEM, exact core prompt revision. It adds only public own metadata and
// stage-coded failures, without new grants or side effects.
// assistantCurrentConfigurationTx читает scope-only prompt context и текущий runtime view под уже
// разрешённой own-target eligibility, сверяет имеющиеся artifact owner/image admission pins и для
// SYSTEM точную core prompt revision. Добавляет только публичные собственные metadata и
// stage-coded failures без новых grants или побочных эффектов.
func (repository *Repository) assistantCurrentConfigurationTx(ctx context.Context, tx pgx.Tx, current scope, ref, assistantScope string) (entity.AssistantCurrentConfiguration, error) {
	snapshot, err := repository.promptPreviewContextTx(ctx, tx, current, promptservice.TargetAgent, ref, query.PromptPreviewContext{ScopeOnly: true})
	if err != nil {
		return entity.AssistantCurrentConfiguration{}, errs.WithAssistantCurrentConfigurationStage(err, errs.AssistantCurrentPromptContext)
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, ref)
	if err != nil {
		return entity.AssistantCurrentConfiguration{}, errs.WithAssistantCurrentConfigurationStage(err, errs.AssistantCurrentConfigurationView)
	}
	result := projectAssistantCurrentConfiguration(view, snapshot)
	if view.Environment.CurrentVersion.Image.ArtifactRef != "" {
		artifact, artifactErr := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, view.Environment.CurrentVersion.Image.ArtifactRef))
		if artifactErr != nil || artifact.AdmissionVerdict != "ACCEPTED" || artifact.PromotionState != "PROMOTED" ||
			artifact.ScopeKind != view.Environment.ScopeKind || artifact.ProjectRef != view.Environment.ProjectRef || artifact.OrganizationRef != view.Environment.OrganizationRef ||
			artifact.ManifestDigest != result.Environment.Image.Digest || artifact.PromotedReference != result.Environment.Image.Reference {
			return entity.AssistantCurrentConfiguration{}, errs.ErrUnavailable
		}
		result.ImageToolInventory, result.ImageToolInventorySHA256 = artifact.ToolInventory, artifact.ToolInventorySHA256
	}
	if assistantScope == "SYSTEM" {
		if err := tx.QueryRow(ctx, queryAssistantCurrentConfigurationOwnerInstructions, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": ref}).Scan(&result.SystemCoreRevision, &result.OwnerInstructions, &result.OwnerInstructionsRevision); err != nil {
			return entity.AssistantCurrentConfiguration{}, errs.WithAssistantCurrentConfigurationStage(errs.ErrUnavailable, errs.AssistantCurrentOwnerCoreRead)
		}
		if result.SystemCoreRevision != systemassistant.CorePromptRevision {
			return entity.AssistantCurrentConfiguration{}, errs.WithAssistantCurrentConfigurationStage(errs.ErrUnavailable, errs.AssistantCurrentOwnerCoreVersion)
		}
		result.SystemCoreInstructions = systemassistant.CorePrompt()
	}
	return result, nil
}

// projectAssistantCurrentConfiguration copies current configuration/environment and full published
// template into the own-assistant response, replacing Kubernetes secret descriptors with
// name/ref/revision bindings. It derives variable availability from the materialization snapshot,
// suppresses absent file groups and preserves explicit unavailability reasons without mutating
// source descriptors.
// projectAssistantCurrentConfiguration копирует текущие configuration/environment и полный
// опубликованный шаблон в собственный assistant response, заменяя Kubernetes secret descriptors на
// name/ref/revision bindings. Выводит variable availability из materialization snapshot, скрывает
// отсутствующие file groups и сохраняет явные причины недоступности, не меняя source descriptors.
func projectAssistantCurrentConfiguration(view entity.AgentRuntimeConfigurationView, snapshot entity.PromptMaterializationSnapshot) entity.AssistantCurrentConfiguration {
	result := entity.AssistantCurrentConfiguration{AgentVersion: view.AgentVersion, Configuration: view.Configuration,
		PublishedOverlay: view.PublishedOverlay, EnvironmentBinding: view.EnvironmentBinding,
		EnvironmentRef: view.Environment.Ref, EnvironmentVersion: view.Environment.Version, Environment: view.Environment.CurrentVersion,
		InstructionTemplateRef: snapshot.TemplateRef, InstructionTemplateDigest: snapshot.TemplateDigest, PublishedInstructions: snapshot.TemplateContent,
		SecretBindings: []entity.RuntimeSecretBinding{}, TemplateVariables: templateVariableCatalog()}
	for _, descriptor := range view.Environment.CurrentVersion.SecretDescriptors {
		result.SecretBindings = append(result.SecretBindings, entity.RuntimeSecretBinding{Name: descriptor.Name, SecretRef: descriptor.SecretRef, Revision: descriptor.Revision})
	}
	// Служебные Kubernetes descriptors не должны пережить безопасную проекцию.
	result.Environment.SecretDescriptors = nil
	available := materializedVariableAvailability(snapshot)
	for _, prefix := range []string{"input", "project", "workflow", "run", "session", "gate"} {
		fields, _ := snapshot.StructuredVariables[prefix].(map[string]any)
		if continuationNumber(fields["files_count"]) <= 0 {
			for _, suffix := range []string{"files", "files_count", "files_dir", "manifest_path"} {
				available[prefix+"."+suffix] = false
			}
		}
	}
	for index := range result.TemplateVariables {
		item := &result.TemplateVariables[index]
		item.Available = available[item.Name]
		item.Reason = variableAvailabilityReason(*item, available, true)
		if reason := snapshot.UnavailableVariables[item.Name]; reason != "" {
			item.Available, item.Reason = false, reason
		}
	}
	return result
}
