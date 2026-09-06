package grpc

import (
	"context"
	"encoding/hex"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ValidatePromptTemplate(ctx context.Context, request *controlplanev1.ValidatePromptTemplateRequest) (*controlplanev1.ValidatePromptTemplateResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ValidatePromptTemplate_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ValidatePromptTemplateWithContext(ctx, p, request.GetTemplate(), request.GetTargetKind(), request.GetTargetRef(), castPromptPreviewContext(request.GetContext()), request.GetExpectedContextDigest())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ValidatePromptTemplateResponse{Valid: result.Complete, ContextPin: castPromptContextPin(result.ContextPin)}
	for _, diagnostic := range result.Diagnostics {
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
	previewContext := castPromptPreviewContext(request.GetContext())
	result, err := server.service.PreviewPromptTemplateWithContext(ctx, p, request.GetTemplate(), request.GetTargetKind(),
		request.GetTargetRef(), request.GetIncludeFullMaterialization(), previewContext, request.GetExpectedContextDigest())
	if err != nil {
		return nil, transportError(err)
	}
	return castPromptMaterialization(result, request.GetIncludeFullMaterialization())
}

func castPromptMaterialization(result promptservice.Materialization, full bool) (*controlplanev1.PreviewPromptTemplateResponse, error) {
	var err error
	response := &controlplanev1.PreviewPromptTemplateResponse{SafePreview: promptservice.SafePreview(result.SafePrompt),
		Complete:    result.Complete,
		TemplateRef: result.TemplateRef, TemplateDigest: result.TemplateDigest,
		MaterializationDigest: result.Digest, EffectiveCapabilities: result.EffectiveCapabilities}
	if full {
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
	if full {
		sections = result.FullSections
	}
	for _, section := range sections {
		mapped, err := castPromptSection(section, result.TemplateRef, result.TemplateDigest)
		if err != nil {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Sections = append(response.Sections, mapped)
	}
	response.ContextPin = castPromptContextPin(result.ContextPin)
	if result.RuntimeDiff != nil {
		response.RuntimeDiff, err = castPromptRuntimeDiff(*result.RuntimeDiff)
		if err != nil {
			return nil, transportError(errs.ErrUnavailable)
		}
	}
	for _, diagnostic := range result.Diagnostics {
		response.Diagnostics = append(response.Diagnostics, castPromptDiagnostic(diagnostic))
	}
	return response, nil
}

func castPromptPreviewContext(input *controlplanev1.PromptPreviewContext) query.PromptPreviewContext {
	result := query.PromptPreviewContext{AgentRef: input.GetAgentRef(), WorkflowRevisionRef: input.GetWorkflowRevisionRef(),
		WorkflowStageKey: input.GetWorkflowStageKey(), ExpectedAgentVersion: input.GetExpectedAgentVersion(), ExpectedWorkflowVersion: input.GetExpectedWorkflowVersion(), AttachmentSetRef: input.GetAttachmentSetRef(), Task: input.GetTask()}
	if input.GetInput() != nil {
		result.Input = input.GetInput().AsMap()
	}
	return result
}

func castPromptContextPin(pin entity.PromptContextPin) *controlplanev1.PromptContextPin {
	if pin.Digest == "" {
		return nil
	}
	return &controlplanev1.PromptContextPin{Digest: pin.Digest, AgentRef: pin.AgentRef, AgentVersion: pin.AgentVersion,
		WorkflowRef: pin.WorkflowRef, WorkflowVersion: pin.WorkflowVersion, WorkflowRevisionRef: pin.WorkflowRevisionRef, WorkflowStageKey: pin.WorkflowStageKey,
		RuntimeConfigurationRef: pin.RuntimeConfigurationRef, RuntimeConfigurationDigest: pin.RuntimeConfigurationDigest,
		EnvironmentBindingRef: pin.EnvironmentBindingRef, EnvironmentBindingVersion: pin.EnvironmentBindingVersion, EnvironmentVersionRef: pin.EnvironmentVersionRef,
		EnvironmentDigest: pin.EnvironmentDigest, AttachmentSetRef: pin.AttachmentSetRef, AttachmentManifestDigest: pin.AttachmentManifestDigest, PreviousRuntimeRevisionRef: pin.PreviousRuntimeRevisionRef}
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

func castPromptSection(section promptservice.Section, baseRef, baseDigest string) (*controlplanev1.PromptPreviewSection, error) {
	slot, slotOK := castPromptSlot(string(section.Slot), section.Source == "USER_TEMPLATE")
	source, sourceOK := castPromptSource(section.Source)
	if !slotOK || !sourceOK || section.Source == "USER_TEMPLATE" && section.Slot != "" {
		return nil, errs.ErrUnavailable
	}
	result := &controlplanev1.PromptPreviewSection{Source: source, Slot: slot, Content: section.Content}
	if section.Source == "PLATFORM" {
		if section.UserKind != "" || section.TemplateRef != "" || section.TemplateDigest != "" {
			return nil, errs.ErrUnavailable
		}
		return result, nil
	}
	kind := section.UserKind
	if kind == "" {
		if section.TemplateRef != "" || section.TemplateDigest != "" {
			return nil, errs.ErrUnavailable
		}
		kind, result.TemplateRef, result.TemplateDigest = "BASE_TEMPLATE", baseRef, baseDigest
	} else {
		if kind != "WORKFLOW_CONTEXT" && kind != "AUTOMATION_TASK" {
			return nil, errs.ErrUnavailable
		}
		decoded, err := hex.DecodeString(section.TemplateDigest)
		if err != nil || len(decoded) != 32 || strings.ToLower(section.TemplateDigest) != section.TemplateDigest || section.TemplateRef == "" || len(section.TemplateRef) > 128 {
			return nil, errs.ErrUnavailable
		}
		result.TemplateRef, result.TemplateDigest = section.TemplateRef, section.TemplateDigest
	}
	result.UserKind = controlplanev1.PromptUserSectionKind(controlplanev1.PromptUserSectionKind_value["PROMPT_USER_SECTION_KIND_"+kind])
	return result, nil
}

func castPromptDiagnostic(value promptservice.Diagnostic) *controlplanev1.PromptTemplateDiagnostic {
	return &controlplanev1.PromptTemplateDiagnostic{Severity: value.Severity, Code: value.Code,
		Message: value.Message, Line: value.Line, Column: value.Column, VariableName: value.VariableName}
}

func castPromptRuntimeDiff(value promptservice.RuntimeDiff) (*controlplanev1.PromptRuntimeDiff, error) {
	if err := promptservice.ValidateRuntimeDiff(value); err != nil {
		return nil, err
	}
	result := &controlplanev1.PromptRuntimeDiff{PreviousRevisionRef: value.PreviousRevisionRef, CurrentRevisionRef: value.CurrentRevisionRef,
		SessionRef: value.SessionRef, TurnRef: value.TurnRef, Attempt: value.Attempt, Digest: value.Digest}
	for _, change := range value.Changes {
		component, ok := controlplanev1.PromptRuntimeComponent_value["PROMPT_RUNTIME_COMPONENT_"+change.Component]
		if !ok || component == 0 {
			return nil, errs.ErrUnavailable
		}
		item := &controlplanev1.PromptRuntimeChange{Component: controlplanev1.PromptRuntimeComponent(component), Action: controlplanev1.PromptRuntimeAction_PROMPT_RUNTIME_ACTION_USE_CURRENT_CONTEXT}
		for _, descriptor := range change.Previous {
			item.Previous = append(item.Previous, &controlplanev1.PromptRuntimeDescriptor{Ref: descriptor.Ref, Version: descriptor.Version, Digest: descriptor.Digest, Value: descriptor.Value})
		}
		for _, descriptor := range change.Current {
			item.Current = append(item.Current, &controlplanev1.PromptRuntimeDescriptor{Ref: descriptor.Ref, Version: descriptor.Version, Digest: descriptor.Digest, Value: descriptor.Value})
		}
		result.Changes = append(result.Changes, item)
	}
	return result, nil
}
