package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func (server *Server) GetEmailMailboxConfiguration(ctx context.Context, request *cp.GetEmailMailboxConfigurationRequest) (*cp.GetEmailMailboxConfigurationResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetEmailMailboxConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetEmailMailboxConfiguration(ctx, p, request.GetConnectionRef(), request.GetConfigurationRef(), request.GetRevisionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetEmailMailboxConfigurationResponse{Configuration: castMailboxView(result)}, nil
}
func (server *Server) ListEmailMailboxConfigurations(ctx context.Context, request *cp.ListEmailMailboxConfigurationsRequest) (*cp.ListEmailMailboxConfigurationsResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_ListEmailMailboxConfigurations_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ListEmailMailboxConfigurations(ctx, p, request.GetConnectionRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListEmailMailboxConfigurationsResponse{Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}, NextActions: castMailboxActions(result.NextActions)}
	for _, item := range result.Items {
		response.Items = append(response.Items, castMailboxView(item))
	}
	return response, nil
}
func (server *Server) ListEmailMailboxCredentials(ctx context.Context, request *cp.ListEmailMailboxCredentialsRequest) (*cp.ListEmailMailboxCredentialsResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_ListEmailMailboxCredentials_FullMethodName)
	if err != nil {
		return nil, err
	}
	name, ok := cp.EmailMailboxCredentialKind_name[int32(request.GetKind())]
	if !ok {
		return nil, transportError(errs.ErrInvalid)
	}
	kind := strings.TrimPrefix(name, "EMAIL_MAILBOX_CREDENTIAL_KIND_")
	if request.GetKind() == 0 {
		kind = ""
	}
	items, total, next, err := server.service.ListEmailMailboxCredentials(ctx, p, request.GetConnectionRef(), kind, page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListEmailMailboxCredentialsResponse{Total: total, Page: &cp.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Items = append(response.Items, castEmailMailboxCredential(item))
	}
	return response, nil
}
func (server *Server) GetEmailMailboxCredentialReceipt(ctx context.Context, request *cp.GetEmailMailboxCredentialReceiptRequest) (*cp.GetEmailMailboxCredentialReceiptResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetEmailMailboxCredentialReceipt_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetEmailMailboxCredentialReceipt(ctx, p, request.GetConnectionRef(), request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetEmailMailboxCredentialReceiptResponse{Credential: castEmailMailboxCredential(result)}, nil
}
func (server *Server) PreviewEmailMailboxConfiguration(ctx context.Context, request *cp.PreviewEmailMailboxConfigurationRequest) (*cp.PreviewEmailMailboxConfigurationResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_PreviewEmailMailboxConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	format, content, err := mailboxContent(request.GetContent())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := server.service.PreviewEmailMailboxConfiguration(ctx, p, request.GetConnectionRef(), format, content)
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.PreviewEmailMailboxConfigurationResponse{CanonicalYaml: result.CanonicalYAML, Diagnostics: castMailboxDiagnostics(result.Diagnostics), Valid: result.Valid}
	if result.Specification != nil {
		response.Specification = castMailboxSpecification(*result.Specification)
	}
	return response, nil
}
