package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ValidatePromptTemplate(ctx context.Context, request *controlplanev1.ValidatePromptTemplateRequest) (*controlplanev1.ValidatePromptTemplateResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ValidatePromptTemplate_FullMethodName)
	if err != nil {
		return nil, err
	}
	diagnostics, err := server.service.ValidatePromptTemplate(ctx, p, request.GetTemplate())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ValidatePromptTemplateResponse{Valid: true}
	for _, diagnostic := range diagnostics {
		response.Diagnostics = append(response.Diagnostics, castPromptDiagnostic(diagnostic))
		if diagnostic.Severity == "ERROR" {
			response.Valid = false
		}
	}
	return response, nil
}

func (server *Server) PreviewPromptTemplate(ctx context.Context, request *controlplanev1.PreviewPromptTemplateRequest) (*controlplanev1.PreviewPromptTemplateResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_PreviewPromptTemplate_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := request.GetContext()
	previewContext := query.PromptPreviewContext{AgentRef: input.GetAgentRef(), WorkflowRevisionRef: input.GetWorkflowRevisionRef(),
		WorkflowStageKey: input.GetWorkflowStageKey(), ExpectedAgentVersion: input.GetExpectedAgentVersion(), ExpectedWorkflowVersion: input.GetExpectedWorkflowVersion(), AttachmentSetRef: input.GetAttachmentSetRef()}
	if input.GetInput() != nil {
		previewContext.Input = input.GetInput().AsMap()
	}
	result, err := server.service.PreviewPromptTemplateWithContext(ctx, p, request.GetTemplate(), request.GetTargetKind(),
		request.GetTargetRef(), request.GetIncludeFullMaterialization(), previewContext, request.GetExpectedContextDigest())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.PreviewPromptTemplateResponse{SafePreview: promptservice.SafePreview(result.SafePrompt),
		Complete:    result.Complete,
		TemplateRef: result.TemplateRef, TemplateDigest: result.TemplateDigest,
		MaterializationDigest: result.Digest, EffectiveCapabilities: result.EffectiveCapabilities}
	if request.GetIncludeFullMaterialization() {
		response.FullMaterializedPrompt = result.Prompt
	}
	response.ServiceTemplateRevision, response.ServiceTemplateDigest, response.VariableSnapshotDigest, response.Locale = result.ServiceTemplateRevision, result.ServiceTemplateDigest, result.VariableSnapshotDigest, result.Locale
	for _, slot := range result.Slots {
		typedSlot, slotOK := castPromptSlot(string(slot.Slot), false)
		typedSource, sourceOK := castPromptSource(slot.Source)
		if !slotOK || !sourceOK || slot.Position < 1 {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Slots = append(response.Slots, &controlplanev1.PromptSlotProvenance{
			Slot: typedSlot, Source: typedSource, Position: slot.Position})
	}
	sections := result.Sections
	if request.GetIncludeFullMaterialization() {
		sections = result.FullSections
	}
	for _, section := range sections {
		typedSlot, slotOK := castPromptSlot(string(section.Slot), section.Source == "USER_TEMPLATE")
		typedSource, sourceOK := castPromptSource(section.Source)
		if !slotOK || !sourceOK || section.Source == "USER_TEMPLATE" && section.Slot != "" {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Sections = append(response.Sections, &controlplanev1.PromptPreviewSection{
			Source: typedSource, Slot: typedSlot, Content: section.Content})
	}
	pin := result.ContextPin
	if pin.Digest != "" {
		response.ContextPin = &controlplanev1.PromptContextPin{Digest: pin.Digest, AgentRef: pin.AgentRef, AgentVersion: pin.AgentVersion,
			WorkflowRef: pin.WorkflowRef, WorkflowVersion: pin.WorkflowVersion, WorkflowRevisionRef: pin.WorkflowRevisionRef, WorkflowStageKey: pin.WorkflowStageKey,
			RuntimeConfigurationRef: pin.RuntimeConfigurationRef, RuntimeConfigurationDigest: pin.RuntimeConfigurationDigest,
			EnvironmentBindingRef: pin.EnvironmentBindingRef, EnvironmentBindingVersion: pin.EnvironmentBindingVersion, EnvironmentVersionRef: pin.EnvironmentVersionRef,
			EnvironmentDigest: pin.EnvironmentDigest, AttachmentSetRef: pin.AttachmentSetRef, AttachmentManifestDigest: pin.AttachmentManifestDigest, PreviousRuntimeRevisionRef: pin.PreviousRuntimeRevisionRef}
	}
	for _, diagnostic := range result.Diagnostics {
		response.Diagnostics = append(response.Diagnostics, castPromptDiagnostic(diagnostic))
	}
	return response, nil
}

func castPromptSlot(value string, allowEmpty bool) (controlplanev1.PromptSemanticSlot, bool) {
	if value == "" && allowEmpty {
		return controlplanev1.PromptSemanticSlot_PROMPT_SEMANTIC_SLOT_UNSPECIFIED, true
	}
	number, ok := controlplanev1.PromptSemanticSlot_value["PROMPT_SEMANTIC_SLOT_"+value]
	return controlplanev1.PromptSemanticSlot(number), ok && number > 0
}

func castPromptSource(value string) (controlplanev1.PromptSectionSource, bool) {
	number, ok := controlplanev1.PromptSectionSource_value["PROMPT_SECTION_SOURCE_"+value]
	return controlplanev1.PromptSectionSource(number), ok && number > 0
}

func castPromptDiagnostic(value promptservice.Diagnostic) *controlplanev1.PromptTemplateDiagnostic {
	return &controlplanev1.PromptTemplateDiagnostic{Severity: value.Severity, Code: value.Code,
		Message: value.Message, Line: value.Line, Column: value.Column, VariableName: value.VariableName}
}
