package grpc

import (
	"context"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	accessservice "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	grpccasters "github.com/codex-k8s/kodex/services/internal/access-manager/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

type accessService interface {
	BootstrapUserFromIdentity(context.Context, accessservice.BootstrapUserFromIdentityInput) (accessservice.BootstrapUserFromIdentityResult, error)
	CreateOrganization(context.Context, accessservice.CreateOrganizationInput) (entity.Organization, error)
	CreateGroup(context.Context, accessservice.CreateGroupInput) (entity.Group, error)
	SetMembership(context.Context, accessservice.SetMembershipInput) (entity.Membership, error)
	PutAllowlistEntry(context.Context, accessservice.PutAllowlistEntryInput) (entity.AllowlistEntry, error)
	PutExternalProvider(context.Context, accessservice.PutExternalProviderInput) (entity.ExternalProvider, error)
	RegisterExternalAccount(context.Context, accessservice.RegisterExternalAccountInput) (entity.ExternalAccount, error)
	BindExternalAccount(context.Context, accessservice.BindExternalAccountInput) (entity.ExternalAccountBinding, error)
	PutAccessAction(context.Context, accessservice.PutAccessActionInput) (entity.AccessAction, error)
	PutAccessRule(context.Context, accessservice.PutAccessRuleInput) (entity.AccessRule, error)
	CheckAccess(context.Context, accessservice.CheckAccessInput) (accessservice.CheckAccessResult, error)
	ExplainAccess(context.Context, accessservice.ExplainAccessInput) (accessservice.ExplainAccessResult, error)
	ResolveExternalAccountUsage(context.Context, accessservice.ResolveExternalAccountUsageInput) (accessservice.ResolveExternalAccountUsageResult, error)
}

// Server implements the generated AccessManagerServiceServer contract.
type Server struct {
	accessaccountsv1.UnimplementedAccessManagerServiceServer
	service accessService
}

// NewServer creates an access-manager gRPC transport around domain use cases.
func NewServer(service accessService) *Server {
	if service == nil {
		panic("access-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterAccessManagerService registers access-manager gRPC handlers.
func RegisterAccessManagerService(registrar grpcruntime.ServiceRegistrar, service accessService) {
	accessaccountsv1.RegisterAccessManagerServiceServer(registrar, NewServer(service))
}

// BootstrapUserFromIdentity creates or resolves a platform user after SSO/OIDC.
func (s *Server) BootstrapUserFromIdentity(ctx context.Context, request *accessaccountsv1.BootstrapUserFromIdentityRequest) (*accessaccountsv1.BootstrapUserFromIdentityResponse, error) {
	return handleUnary(ctx, request, grpccasters.BootstrapUserFromIdentityInput, s.service.BootstrapUserFromIdentity, grpccasters.BootstrapUserFromIdentityResponse)
}

// CreateOrganization creates an owner, client, contractor or SaaS organization.
func (s *Server) CreateOrganization(ctx context.Context, request *accessaccountsv1.CreateOrganizationRequest) (*accessaccountsv1.OrganizationResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreateOrganizationInput, s.service.CreateOrganization, grpccasters.OrganizationResponse)
}

// CreateGroup creates a global or organization-scoped group.
func (s *Server) CreateGroup(ctx context.Context, request *accessaccountsv1.CreateGroupRequest) (*accessaccountsv1.GroupResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreateGroupInput, s.service.CreateGroup, grpccasters.GroupResponse)
}

// SetMembership creates, updates or disables subject membership.
func (s *Server) SetMembership(ctx context.Context, request *accessaccountsv1.SetMembershipRequest) (*accessaccountsv1.MembershipResponse, error) {
	return handleUnary(ctx, request, grpccasters.SetMembershipInput, s.service.SetMembership, grpccasters.MembershipResponse)
}

// PutAllowlistEntry creates or updates an allowlist entry.
func (s *Server) PutAllowlistEntry(ctx context.Context, request *accessaccountsv1.PutAllowlistEntryRequest) (*accessaccountsv1.AllowlistEntryResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutAllowlistEntryInput, s.service.PutAllowlistEntry, grpccasters.AllowlistEntryResponse)
}

// RegisterExternalProvider creates an external-account provider.
func (s *Server) RegisterExternalProvider(ctx context.Context, request *accessaccountsv1.RegisterExternalProviderRequest) (*accessaccountsv1.ExternalProviderResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutExternalProviderInput, s.service.PutExternalProvider, grpccasters.ExternalProviderResponse)
}

// RegisterExternalAccount creates an external account as a policy subject.
func (s *Server) RegisterExternalAccount(ctx context.Context, request *accessaccountsv1.RegisterExternalAccountRequest) (*accessaccountsv1.ExternalAccountResponse, error) {
	return handleUnary(ctx, request, grpccasters.RegisterExternalAccountInput, s.service.RegisterExternalAccount, grpccasters.ExternalAccountResponse)
}

// BindExternalAccount allows an external account to be used in a scope.
func (s *Server) BindExternalAccount(ctx context.Context, request *accessaccountsv1.BindExternalAccountRequest) (*accessaccountsv1.ExternalAccountBindingResponse, error) {
	return handleUnary(ctx, request, grpccasters.BindExternalAccountInput, s.service.BindExternalAccount, grpccasters.ExternalAccountBindingResponse)
}

// PutAccessAction creates or updates an action from the access-action catalog.
func (s *Server) PutAccessAction(ctx context.Context, request *accessaccountsv1.PutAccessActionRequest) (*accessaccountsv1.AccessActionResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutAccessActionInput, s.service.PutAccessAction, grpccasters.AccessActionResponse)
}

// PutAccessRule creates or updates an access rule.
func (s *Server) PutAccessRule(ctx context.Context, request *accessaccountsv1.PutAccessRuleRequest) (*accessaccountsv1.AccessRuleResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutAccessRuleInput, s.service.PutAccessRule, grpccasters.AccessRuleResponse)
}

// ResolveExternalAccountUsage checks whether an external account can be used.
func (s *Server) ResolveExternalAccountUsage(ctx context.Context, request *accessaccountsv1.ResolveExternalAccountUsageRequest) (*accessaccountsv1.ResolveExternalAccountUsageResponse, error) {
	return handleUnary(ctx, request, grpccasters.ResolveExternalAccountUsageInput, s.service.ResolveExternalAccountUsage, grpccasters.ResolveExternalAccountUsageResponse)
}

// CheckAccess resolves effective access for a subject, action and resource.
func (s *Server) CheckAccess(ctx context.Context, request *accessaccountsv1.CheckAccessRequest) (*accessaccountsv1.CheckAccessResponse, error) {
	return handleUnary(ctx, request, grpccasters.CheckAccessInput, s.service.CheckAccess, grpccasters.CheckAccessResponse)
}

// ExplainAccess returns a previously audited access decision explanation.
func (s *Server) ExplainAccess(ctx context.Context, request *accessaccountsv1.ExplainAccessRequest) (*accessaccountsv1.ExplainAccessResponse, error) {
	return handleUnary(ctx, request, grpccasters.ExplainAccessInput, s.service.ExplainAccess, grpccasters.ExplainAccessResponse)
}

func handleUnary[Request any, Input any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast func(*Request) (Input, error),
	call func(context.Context, Input) (Output, error),
	respond func(Output) *Response,
) (*Response, error) {
	input, err := cast(request)
	if err != nil {
		return nil, err
	}
	output, err := call(ctx, input)
	if err != nil {
		return nil, err
	}
	return respond(output), nil
}
