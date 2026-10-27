package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"strings"
)

// integrationCandidateInput copies nullable candidate context and page, defaults purpose to GRANT
// and strips the recipient-kind prefix without enum validation.
// integrationCandidateInput копирует nullable-контекст кандидатов и страницу, выбирает цель GRANT
// и удаляет префикс вида получателя без проверки enum.
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

// castIntegrationCandidateContext projects owner-resolved candidate context locators and recipient
// kind without treating payload locators as authority.
// castIntegrationCandidateContext преобразует разрешённые владельцем ссылки контекста кандидатов и
// вид получателя без назначения полномочий ссылками payload.
func castIntegrationCandidateContext(c entity.IntegrationCandidateContext) *controlplanev1.IntegrationGrantCandidateContext {
	return &controlplanev1.IntegrationGrantCandidateContext{ConnectionRef: c.ConnectionRef, ProjectRef: c.ProjectRef,
		RecipientKind: controlplanev1.IntegrationGrantRecipientKind(controlplanev1.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+c.RecipientKind]),
		RecipientRef:  c.RecipientRef, CapabilityKey: c.CapabilityKey, WorkflowRef: c.WorkflowRef, StepKey: c.StepKey}
}

// castIntegrationCandidatePins copies exact context, connection, definition, project, recipient
// and workflow revision pins from the domain result.
// castIntegrationCandidatePins копирует точные привязки контекста, подключения, определения,
// проекта, получателя и ревизии процесса из доменного результата.
func castIntegrationCandidatePins(p entity.IntegrationCandidatePins) *controlplanev1.IntegrationGrantCandidatePins {
	return &controlplanev1.IntegrationGrantCandidatePins{ContextDigest: p.ContextDigest, ConnectionVersion: p.ConnectionVersion,
		DefinitionVersion: p.DefinitionVersion, DefinitionDigest: p.DefinitionDigest, ProjectVersion: p.ProjectVersion,
		RecipientVersion: p.RecipientVersion, WorkflowRevisionRef: p.WorkflowRevisionRef}
}

// integrationCandidateReason requires a known nonzero reason enum and returns unavailable when
// owner output cannot be represented.
// integrationCandidateReason требует известный ненулевой enum причины и возвращает unavailable,
// если результат владельца нельзя представить.
func integrationCandidateReason(reason string) (controlplanev1.IntegrationCandidateReason, error) {
	v, ok := controlplanev1.IntegrationCandidateReason_value["INTEGRATION_CANDIDATE_REASON_"+reason]
	if !ok || v == 0 {
		return 0, errs.ErrUnavailable
	}
	return controlplanev1.IntegrationCandidateReason(v), nil
}

// ListIntegrationGrantConnectionCandidates passes explicit purpose and context to the authorized
// connection-candidate read and requires representable reasons before returning pins and
// pagination.
// ListIntegrationGrantConnectionCandidates передаёт явную цель и контекст авторизованному чтению
// кандидатов подключений и требует представимые причины до возврата привязок и пагинации.
func (server *Server) ListIntegrationGrantConnectionCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantConnectionCandidatesRequest) (*controlplanev1.ListIntegrationGrantConnectionCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantConnectionCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationGrantConnectionCandidatesResponse, error) {
		input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())
		input.Purpose = strings.TrimPrefix(request.GetPurpose().String(), "INTEGRATION_CANDIDATE_PURPOSE_")
		result, err := server.service.ListIntegrationGrantConnectionCandidates(ctx, p, input)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationGrantConnectionCandidatesResponse{
			Items: []*controlplanev1.IntegrationGrantConnectionCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
			Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
		}
		for _, item := range result.Items {
			reason, err := integrationCandidateReason(item.Reason)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, &controlplanev1.IntegrationGrantConnectionCandidate{ConnectionRef: item.ConnectionRef, Name: item.Name, DefinitionKey: item.DefinitionKey, ProviderName: item.ProviderName, CredentialKind: item.CredentialKind, ProjectRef: item.ProjectRef, ResourceScope: item.ResourceScope, Grantable: item.Grantable, Usable: item.Usable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
		}
		return response, nil

	})
}

// ListIntegrationGrantProjectCandidates reads project candidates with GRANT purpose and projects
// checked reasons, per-item pins and owner context/pagination.
// ListIntegrationGrantProjectCandidates читает кандидатов проектов с целью GRANT и преобразует
// проверенные причины, привязки элементов и контекст с пагинацией владельца.
func (server *Server) ListIntegrationGrantProjectCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantProjectCandidatesRequest) (*controlplanev1.ListIntegrationGrantProjectCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantProjectCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationGrantProjectCandidatesResponse, error) {
		input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

		result, err := server.service.ListIntegrationGrantProjectCandidates(ctx, p, input)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationGrantProjectCandidatesResponse{
			Items: []*controlplanev1.IntegrationGrantProjectCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
			Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
		}
		for _, item := range result.Items {
			reason, err := integrationCandidateReason(item.Reason)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, &controlplanev1.IntegrationGrantProjectCandidate{ProjectRef: item.ProjectRef, Name: item.Name, Grantable: item.Grantable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
		}
		return response, nil

	})
}

// ListIntegrationGrantRecipientCandidates reads recipient candidates with GRANT purpose and
// preserves typed recipient fields, checked reasons and exact result pins.
// ListIntegrationGrantRecipientCandidates читает кандидатов получателей с целью GRANT и сохраняет
// типизированные поля получателя, проверенные причины и точные привязки результата.
func (server *Server) ListIntegrationGrantRecipientCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantRecipientCandidatesRequest) (*controlplanev1.ListIntegrationGrantRecipientCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantRecipientCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationGrantRecipientCandidatesResponse, error) {
		input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

		result, err := server.service.ListIntegrationGrantRecipientCandidates(ctx, p, input)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationGrantRecipientCandidatesResponse{
			Items: []*controlplanev1.IntegrationGrantRecipientCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
			Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
		}
		for _, item := range result.Items {
			reason, err := integrationCandidateReason(item.Reason)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, &controlplanev1.IntegrationGrantRecipientCandidate{RecipientRef: item.RecipientRef, Name: item.Name, RecipientKind: controlplanev1.IntegrationGrantRecipientKind(controlplanev1.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+item.RecipientKind]), ProjectRef: item.ProjectRef, Grantable: item.Grantable, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
		}
		return response, nil

	})
}

// ListIntegrationGrantCapabilityCandidates reads capability candidates with GRANT purpose and
// returns checked reasons, grant version and typed capability projections.
// ListIntegrationGrantCapabilityCandidates читает кандидатов возможностей с целью GRANT и
// возвращает проверенные причины, версию grant и типизированные представления возможностей.
func (server *Server) ListIntegrationGrantCapabilityCandidates(ctx context.Context, request *controlplanev1.ListIntegrationGrantCapabilityCandidatesRequest) (*controlplanev1.ListIntegrationGrantCapabilityCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListIntegrationGrantCapabilityCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.ListIntegrationGrantCapabilityCandidatesResponse, error) {
		input := integrationCandidateInput(request.GetContext(), request.GetQuery(), request.GetPage())

		result, err := server.service.ListIntegrationGrantCapabilityCandidates(ctx, p, input)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListIntegrationGrantCapabilityCandidatesResponse{
			Items: []*controlplanev1.IntegrationGrantCapabilityCandidate{}, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
			Total: result.Total, ContextDigest: result.ContextDigest, Context: castIntegrationCandidateContext(result.Context), Pins: castIntegrationCandidatePins(result.Pins),
		}
		for _, item := range result.Items {
			reason, err := integrationCandidateReason(item.Reason)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, &controlplanev1.IntegrationGrantCapabilityCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, Reason: reason, Pins: castIntegrationCandidatePins(item.Pins)})
		}
		return response, nil

	})
}
