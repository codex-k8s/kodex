package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"strings"
)

func integrationCandidateInput(c *controlplanev1.IntegrationGrantCandidateContext, search string, pagination *controlplanev1.PageRequest) query.IntegrationCandidates {
	kind := ""
	if c.GetRecipientKind() != controlplanev1.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_UNSPECIFIED {
		kind = strings.TrimPrefix(c.GetRecipientKind().String(), "INTEGRATION_GRANT_RECIPIENT_KIND_")
	}
	return query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{
		ConnectionRef: c.GetConnectionRef(), ProjectRef: c.GetProjectRef(), RecipientKind: kind, RecipientRef: c.GetRecipientRef(),
		CapabilityKey: c.GetCapabilityKey(), WorkflowRef: c.GetWorkflowRef(), StepKey: c.GetStepKey(),
	}, Filter: query.Filter{Query: search, Page: page(pagination)}}
}
func castIntegrationCandidateContext(c entity.IntegrationCandidateContext) *controlplanev1.IntegrationGrantCandidateContext {
	return &controlplanev1.IntegrationGrantCandidateContext{ConnectionRef: c.ConnectionRef, ProjectRef: c.ProjectRef,
		RecipientKind: controlplanev1.IntegrationGrantRecipientKind(controlplanev1.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+c.RecipientKind]),
		RecipientRef:  c.RecipientRef, CapabilityKey: c.CapabilityKey, WorkflowRef: c.WorkflowRef, StepKey: c.StepKey}
}
func castIntegrationCandidatePins(p entity.IntegrationCandidatePins) *controlplanev1.IntegrationGrantCandidatePins {
	return &controlplanev1.IntegrationGrantCandidatePins{ContextDigest: p.ContextDigest, ConnectionVersion: p.ConnectionVersion,
		DefinitionVersion: p.DefinitionVersion, DefinitionDigest: p.DefinitionDigest, ProjectVersion: p.ProjectVersion,
		RecipientVersion: p.RecipientVersion, WorkflowRevisionRef: p.WorkflowRevisionRef}
}
func integrationCandidateReason(reason string) (controlplanev1.IntegrationCandidateReason, error) {
	v, ok := controlplanev1.IntegrationCandidateReason_value["INTEGRATION_CANDIDATE_REASON_"+reason]
	if !ok || v == 0 {
		return 0, errs.ErrUnavailable
	}
	return controlplanev1.IntegrationCandidateReason(v), nil
}

func (server *Server) ListIntegrationGrantConnectionCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantConnectionCandidatesRequest) (*controlplanev1.ListIntegrationGrantConnectionCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantConnectionCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())
	input.Purpose = strings.TrimPrefix(request.GetPurpose().String(), "INTEGRATION_CANDIDATE_PURPOSE_")
	result, err := server.service.ListIntegrationGrantConnectionCandidates(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationGrantConnectionCandidatesResponse{
		Items: []*controlplanev1.IntegrationGrantConnectionCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
		Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
	}
	for _, item := range result.Items {
		reason, err := integrationCandidateReason(item.Reason)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, &controlplanev1.IntegrationGrantConnectionCandidate{ConnectionRef: item.ConnectionRef, Name: item.Name, DefinitionKey: item.DefinitionKey, ProviderName: item.ProviderName, CredentialKind: item.CredentialKind, ProjectRef: item.ProjectRef, ResourceScope: item.ResourceScope, Grantable: item.Grantable, Usable: item.Usable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
	}
	return response, nil
}

func (server *Server) ListIntegrationGrantProjectCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantProjectCandidatesRequest) (*controlplanev1.ListIntegrationGrantProjectCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantProjectCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

	result, err := server.service.ListIntegrationGrantProjectCandidates(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationGrantProjectCandidatesResponse{
		Items: []*controlplanev1.IntegrationGrantProjectCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
		Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
	}
	for _, item := range result.Items {
		reason, err := integrationCandidateReason(item.Reason)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, &controlplanev1.IntegrationGrantProjectCandidate{ProjectRef: item.ProjectRef, Name: item.Name, Grantable: item.Grantable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
	}
	return response, nil
}

func (server *Server) ListIntegrationGrantRecipientCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantRecipientCandidatesRequest) (*controlplanev1.ListIntegrationGrantRecipientCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantRecipientCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

	result, err := server.service.ListIntegrationGrantRecipientCandidates(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationGrantRecipientCandidatesResponse{
		Items: []*controlplanev1.IntegrationGrantRecipientCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
		Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
	}
	for _, item := range result.Items {
		reason, err := integrationCandidateReason(item.Reason)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, &controlplanev1.IntegrationGrantRecipientCandidate{RecipientRef: item.RecipientRef, Name: item.Name, RecipientKind: controlplanev1.IntegrationGrantRecipientKind(controlplanev1.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+item.RecipientKind]), ProjectRef: item.ProjectRef, Grantable: item.Grantable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
	}
	return response, nil
}

func (server *Server) ListIntegrationGrantCapabilityCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantCapabilityCandidatesRequest) (*controlplanev1.ListIntegrationGrantCapabilityCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantCapabilityCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

	result, err := server.service.ListIntegrationGrantCapabilityCandidates(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListIntegrationGrantCapabilityCandidatesResponse{
		Items: []*controlplanev1.IntegrationGrantCapabilityCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
		Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
	}
	for _, item := range result.Items {
		reason, err := integrationCandidateReason(item.Reason)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, &controlplanev1.IntegrationGrantCapabilityCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
	}
	return response, nil
}
