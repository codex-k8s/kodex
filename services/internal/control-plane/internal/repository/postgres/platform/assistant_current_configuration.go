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

//go:embed sql/assistant_current_configuration__owner_instructions.sql
var queryAssistantCurrentConfigurationOwnerInstructions string

// Lease и собственный target уже разрешены каталогом; view eligibility не расширяется.
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
