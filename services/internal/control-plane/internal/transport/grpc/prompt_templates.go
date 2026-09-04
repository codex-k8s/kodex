package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
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
	result, err := server.service.PreviewPromptTemplate(ctx, p, request.GetTemplate(), request.GetTargetKind(),
		request.GetTargetRef(), request.GetIncludeFullMaterialization())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.PreviewPromptTemplateResponse{SafePreview: promptservice.SafePreview(result.SafePrompt),
		TemplateRef: result.TemplateRef, TemplateDigest: result.TemplateDigest,
		MaterializationDigest: result.Digest, EffectiveCapabilities: result.EffectiveCapabilities}
	if request.GetIncludeFullMaterialization() {
		response.FullMaterializedPrompt = result.Prompt
	}
	for _, diagnostic := range result.Diagnostics {
		response.Diagnostics = append(response.Diagnostics, castPromptDiagnostic(diagnostic))
	}
	return response, nil
}

func castPromptDiagnostic(value promptservice.Diagnostic) *controlplanev1.PromptTemplateDiagnostic {
	return &controlplanev1.PromptTemplateDiagnostic{Severity: value.Severity, Code: value.Code,
		Message: value.Message, Line: value.Line, Column: value.Column}
}
