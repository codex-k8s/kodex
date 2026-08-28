package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ListPermissionRegistry(ctx context.Context, _ *controlplanev1.ListPermissionRegistryRequest) (*controlplanev1.ListPermissionRegistryResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListPermissionRegistry_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListPermissionRegistry(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListPermissionRegistryResponse{}
	for _, item := range items {
		response.Permissions = append(response.Permissions, castPermissionDefinition(item))
	}
	return response, nil
}

func (server *Server) ListAccessSubjects(ctx context.Context, request *controlplanev1.ListAccessSubjectsRequest) (*controlplanev1.ListAccessSubjectsResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessSubjects_FullMethodName)
	if err != nil {
		return nil, err
	}
	kind := accessSubjectKind(request.GetKind())
	items, next, err := server.service.ListAccessSubjects(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())}, kind)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessSubjectsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Subjects = append(response.Subjects, castAccessSubject(item))
	}
	return response, nil
}

func (server *Server) ListOIDCGroups(ctx context.Context, request *controlplanev1.ListOIDCGroupsRequest) (*controlplanev1.ListOIDCGroupsResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListOIDCGroups_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListOIDCGroups(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListOIDCGroupsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Groups = append(response.Groups, castOIDCGroup(item))
	}
	return response, nil
}

func (server *Server) ListAccessRoles(ctx context.Context, request *controlplanev1.ListAccessRolesRequest) (*controlplanev1.ListAccessRolesResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessRoles_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAccessRoles(ctx, p, page(request.GetPage()), request.GetIncludeArchived())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessRolesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Roles = append(response.Roles, castAccessRole(item))
	}
	return response, nil
}

func (server *Server) ListAccessRoleVersions(ctx context.Context, request *controlplanev1.ListAccessRoleVersionsRequest) (*controlplanev1.ListAccessRoleVersionsResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessRoleVersions_FullMethodName)
	if err != nil {
		return nil, err
	}
	role, items, next, err := server.service.ListAccessRoleVersions(ctx, p, request.GetRoleRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessRoleVersionsResponse{Role: castAccessRole(role), Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Versions = append(response.Versions, castAccessRoleVersion(item))
	}
	return response, nil
}

func (server *Server) ListAccessBindings(ctx context.Context, request *controlplanev1.ListAccessBindingsRequest) (*controlplanev1.ListAccessBindingsResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessBindings_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAccessBindings(ctx, p, query.AccessBindingFilter{
		Page: page(request.GetPage()), SubjectKind: accessSubjectKind(request.GetSubjectKind()),
		SubjectRef: request.GetSubjectRef(), RoleRef: request.GetRoleRef(), ProjectRef: request.GetProjectRef(),
		IncludeRevoked: request.GetIncludeRevoked(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessBindingsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Bindings = append(response.Bindings, castAccessBinding(item))
	}
	return response, nil
}

func (server *Server) QueryEffectiveAccess(ctx context.Context, request *controlplanev1.QueryEffectiveAccessRequest) (*controlplanev1.QueryEffectiveAccessResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_QueryEffectiveAccess_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.QueryEffectiveAccess(ctx, p, request.GetSubjectRef(), domainAccessScope(request.GetTarget()), request.GetPermissionKeys(), time.Time{})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.QueryEffectiveAccessResponse{Subject: castAccessSubject(result.Subject), EvaluatedAt: timestamppb.New(result.EvaluatedAt)}
	for _, item := range result.Decisions {
		response.Decisions = append(response.Decisions, castEffectiveDecision(item))
	}
	return response, nil
}

func (server *Server) ExplainAccess(ctx context.Context, request *controlplanev1.ExplainAccessRequest) (*controlplanev1.ExplainAccessResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ExplainAccess_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.QueryEffectiveAccess(ctx, p, request.GetSubjectRef(), domainAccessScope(request.GetTarget()), []string{request.GetPermissionKey()}, time.Time{})
	if err != nil {
		return nil, transportError(err)
	}
	if len(result.Decisions) != 1 {
		return nil, transportError(errs.ErrInvalid)
	}
	return &controlplanev1.ExplainAccessResponse{Subject: castAccessSubject(result.Subject), Result: castEffectiveDecision(result.Decisions[0]), EvaluatedAt: timestamppb.New(result.EvaluatedAt)}, nil
}

func (server *Server) SimulateAccess(ctx context.Context, request *controlplanev1.SimulateAccessRequest) (*controlplanev1.SimulateAccessResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_SimulateAccess_FullMethodName)
	if err != nil {
		return nil, err
	}
	evaluatedAt, err := optionalDomainTime(request.GetEvaluatedAt())
	if err != nil {
		return nil, transportError(err)
	}
	role := request.GetRole()
	binding := request.GetBinding()
	result, err := server.service.SimulateAccess(ctx, p, command.AccessSimulationInput{
		SubjectRef: request.GetSubjectRef(), PermissionKey: request.GetPermissionKey(), Target: domainAccessScope(request.GetTarget()),
		Role:        command.AccessRoleInput{PermissionKeys: append([]string(nil), role.GetPermissionKeys()...), AllowedScopes: accessScopeKinds(role.GetAllowedScopes())},
		Binding:     command.AccessBindingInput{SubjectKind: accessSubjectKind(binding.GetSubjectKind()), SubjectRef: binding.GetSubjectRef(), RoleVersionRef: "simulation", Scope: domainAccessScope(binding.GetScope()), Conditions: domainAccessConditions(binding.GetConditions())},
		EvaluatedAt: evaluatedAt,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.SimulateAccessResponse{Subject: castAccessSubject(result.Subject), Current: castEffectiveDecision(result.Current), Simulated: castEffectiveDecision(result.Simulated), EvaluatedAt: timestamppb.New(result.EvaluatedAt)}, nil
}

func (server *Server) CreateAccessRole(ctx context.Context, request *controlplanev1.CreateAccessRoleRequest) (*controlplanev1.CreateAccessRoleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_CreateAccessRole_FullMethodName, command.CreateAccessRole, request.GetMutation(), accessRoleInput("", request.GetName(), request.GetDescription(), request.GetPermissionKeys(), request.GetAllowedScopes(), request.GetChangeComment()))
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAccessRoleResponse{Role: castAccessRole(*result.AccessRole)}, nil
}

func (server *Server) CreateAccessRoleVersion(ctx context.Context, request *controlplanev1.CreateAccessRoleVersionRequest) (*controlplanev1.CreateAccessRoleVersionResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_CreateAccessRoleVersion_FullMethodName, command.CreateAccessRoleVersion, request.GetMutation(), accessRoleInput(request.GetRoleRef(), request.GetName(), request.GetDescription(), request.GetPermissionKeys(), request.GetAllowedScopes(), request.GetChangeComment()))
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAccessRoleVersionResponse{Role: castAccessRole(*result.AccessRole)}, nil
}

func (server *Server) ArchiveAccessRole(ctx context.Context, request *controlplanev1.ArchiveAccessRoleRequest) (*controlplanev1.ArchiveAccessRoleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_ArchiveAccessRole_FullMethodName, command.ArchiveAccessRole, request.GetMutation(), command.AccessRoleInput{RoleRef: request.GetRoleRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveAccessRoleResponse{Role: castAccessRole(*result.AccessRole)}, nil
}

func (server *Server) CreateAccessBinding(ctx context.Context, request *controlplanev1.CreateAccessBindingRequest) (*controlplanev1.CreateAccessBindingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_CreateAccessBinding_FullMethodName, command.CreateAccessBinding, request.GetMutation(), command.AccessBindingInput{
		SubjectKind: accessSubjectKind(request.GetSubjectKind()), SubjectRef: request.GetSubjectRef(), RoleVersionRef: request.GetRoleVersionRef(),
		Scope: domainAccessScope(request.GetScope()), Conditions: domainAccessConditions(request.GetConditions()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}, nil
}

func (server *Server) ChangeAccessBinding(ctx context.Context, request *controlplanev1.ChangeAccessBindingRequest) (*controlplanev1.ChangeAccessBindingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_ChangeAccessBinding_FullMethodName, command.ChangeAccessBinding, request.GetMutation(), command.AccessBindingInput{
		BindingRef: request.GetBindingRef(), RoleVersionRef: request.GetRoleVersionRef(), Scope: domainAccessScope(request.GetScope()), Conditions: domainAccessConditions(request.GetConditions()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}, nil
}

func (server *Server) RevokeAccessBinding(ctx context.Context, request *controlplanev1.RevokeAccessBindingRequest) (*controlplanev1.RevokeAccessBindingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.AccessService_RevokeAccessBinding_FullMethodName, command.RevokeAccessBinding, request.GetMutation(), command.AccessBindingInput{BindingRef: request.GetBindingRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RevokeAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}, nil
}

func accessRoleInput(roleRef, name, description string, permissions []string, scopes []controlplanev1.AccessScopeKind, comment string) command.AccessRoleInput {
	return command.AccessRoleInput{RoleRef: roleRef, Name: name, Description: description, PermissionKeys: append([]string(nil), permissions...), AllowedScopes: accessScopeKinds(scopes), ChangeComment: comment}
}

func domainAccessScope(input *controlplanev1.AccessScope) entity.AccessScope {
	if input == nil {
		return entity.AccessScope{}
	}
	return entity.AccessScope{Kind: accessScopeKind(input.GetKind()), ProjectRef: input.GetProjectRef(), ResourceKind: accessResourceKind(input.GetResourceKind()), ResourceRef: input.GetResourceRef()}
}

func domainAccessConditions(input *controlplanev1.AccessConditions) entity.AccessConditions {
	if input == nil {
		return entity.AccessConditions{}
	}
	result := entity.AccessConditions{RequireOwner: input.GetRequireOwner()}
	if value := input.GetValidFrom(); value != nil && value.IsValid() {
		timeValue := value.AsTime()
		result.ValidFrom = &timeValue
	}
	if value := input.GetValidUntil(); value != nil && value.IsValid() {
		timeValue := value.AsTime()
		result.ValidUntil = &timeValue
	}
	return result
}

func optionalDomainTime(input *timestamppb.Timestamp) (*time.Time, error) {
	if input == nil {
		return nil, nil
	}
	if !input.IsValid() {
		return nil, errs.ErrInvalid
	}
	value := input.AsTime()
	return &value, nil
}

func accessSubjectKind(value controlplanev1.AccessSubjectKind) string {
	if value == controlplanev1.AccessSubjectKind_ACCESS_SUBJECT_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_SUBJECT_KIND_")
}

func accessScopeKind(value controlplanev1.AccessScopeKind) string {
	if value == controlplanev1.AccessScopeKind_ACCESS_SCOPE_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_SCOPE_KIND_")
}

func accessScopeKinds(values []controlplanev1.AccessScopeKind) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := accessScopeKind(value); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func accessResourceKind(value controlplanev1.AccessResourceKind) string {
	if value == controlplanev1.AccessResourceKind_ACCESS_RESOURCE_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_RESOURCE_KIND_")
}

func castPermissionDefinition(value entity.PermissionDefinition) *controlplanev1.PermissionDefinition {
	result := &controlplanev1.PermissionDefinition{Key: value.Key, NameKey: value.NameKey, DescriptionKey: value.DescriptionKey, Risk: permissionRisk(value.Risk), OwnerConditionSupported: value.OwnerConditionSupported}
	for _, scope := range value.AllowedScopes {
		result.AllowedScopes = append(result.AllowedScopes, scopeKindEnum(scope))
	}
	for _, kind := range value.ResourceKinds {
		result.ResourceKinds = append(result.ResourceKinds, resourceKindEnum(kind))
	}
	return result
}

func castAccessSubject(value entity.AccessSubject) *controlplanev1.AccessSubject {
	return &controlplanev1.AccessSubject{Ref: value.Ref, Kind: subjectKindEnum(value.Kind), DisplayName: value.DisplayName, Active: value.Active, OidcGroupRefs: append([]string(nil), value.OIDCGroupRefs...)}
}

func castOIDCGroup(value entity.OIDCGroup) *controlplanev1.OIDCGroup {
	return &controlplanev1.OIDCGroup{Ref: value.Ref, DisplayName: value.DisplayName, State: oidcGroupState(value.State), MemberCount: value.MemberCount, BindingCount: value.BindingCount, LastSeenAt: timestamppb.New(value.LastSeenAt), SyncedAt: timestamppb.New(value.SynchronizedAt)}
}

func castAccessRole(value entity.AccessRole) *controlplanev1.AccessRole {
	return &controlplanev1.AccessRole{Ref: value.Ref, Version: value.Version, Kind: roleKindEnum(value.Kind), State: roleStateEnum(value.State), CurrentVersion: castAccessRoleVersion(value.CurrentVersion), BindingCount: value.BindingCount, UpdatedAt: timestamppb.New(value.UpdatedAt)}
}

func castAccessRoleVersion(value entity.AccessRoleVersion) *controlplanev1.AccessRoleVersion {
	result := &controlplanev1.AccessRoleVersion{Ref: value.Ref, RoleRef: value.RoleRef, Revision: value.Revision, Name: value.Name, Description: value.Description, PermissionKeys: append([]string(nil), value.PermissionKeys...), ChangeComment: value.ChangeComment, CreatedAt: timestamppb.New(value.CreatedAt), CreatedBy: castUser(value.CreatedBy)}
	for _, scope := range value.AllowedScopes {
		result.AllowedScopes = append(result.AllowedScopes, scopeKindEnum(scope))
	}
	return result
}

func castAccessScope(value entity.AccessScope) *controlplanev1.AccessScope {
	return &controlplanev1.AccessScope{Kind: scopeKindEnum(value.Kind), ProjectRef: value.ProjectRef, ResourceKind: resourceKindEnum(value.ResourceKind), ResourceRef: value.ResourceRef}
}

func castAccessBinding(value entity.AccessBinding) *controlplanev1.AccessBinding {
	conditions := &controlplanev1.AccessConditions{RequireOwner: value.Conditions.RequireOwner}
	if value.Conditions.ValidFrom != nil {
		conditions.ValidFrom = timestamppb.New(*value.Conditions.ValidFrom)
	}
	if value.Conditions.ValidUntil != nil {
		conditions.ValidUntil = timestamppb.New(*value.Conditions.ValidUntil)
	}
	return &controlplanev1.AccessBinding{Ref: value.Ref, Version: value.Version, State: bindingStateEnum(value.State), Subject: castAccessSubject(value.Subject), RoleVersion: castAccessRoleVersion(value.RoleVersion), Scope: castAccessScope(value.Scope), Conditions: conditions, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt)}
}

func castEffectiveDecision(value entity.EffectiveAccessDecision) *controlplanev1.EffectiveAccessDecision {
	decision := controlplanev1.AccessDecision_ACCESS_DECISION_DENIED
	if value.Allowed {
		decision = controlplanev1.AccessDecision_ACCESS_DECISION_ALLOWED
	}
	result := &controlplanev1.EffectiveAccessDecision{PermissionKey: value.PermissionKey, Decision: decision, Target: castAccessScope(value.Target)}
	for _, step := range value.Explanation {
		result.Explanation = append(result.Explanation, &controlplanev1.AccessExplanationStep{Code: step.Code, BindingRef: step.BindingRef, RoleRef: step.RoleRef, RoleVersionRef: step.RoleVersionRef, SourceKind: subjectKindEnum(step.SourceKind), SourceRef: step.SourceRef, Scope: castAccessScope(step.Scope)})
	}
	return result
}

func permissionRisk(value string) controlplanev1.PermissionRisk {
	return controlplanev1.PermissionRisk(controlplanev1.PermissionRisk_value["PERMISSION_RISK_"+value])
}
func subjectKindEnum(value string) controlplanev1.AccessSubjectKind {
	return controlplanev1.AccessSubjectKind(controlplanev1.AccessSubjectKind_value["ACCESS_SUBJECT_KIND_"+value])
}
func scopeKindEnum(value string) controlplanev1.AccessScopeKind {
	return controlplanev1.AccessScopeKind(controlplanev1.AccessScopeKind_value["ACCESS_SCOPE_KIND_"+value])
}
func resourceKindEnum(value string) controlplanev1.AccessResourceKind {
	return controlplanev1.AccessResourceKind(controlplanev1.AccessResourceKind_value["ACCESS_RESOURCE_KIND_"+value])
}
func roleKindEnum(value string) controlplanev1.AccessRoleKind {
	return controlplanev1.AccessRoleKind(controlplanev1.AccessRoleKind_value["ACCESS_ROLE_KIND_"+value])
}
func roleStateEnum(value string) controlplanev1.AccessRoleState {
	return controlplanev1.AccessRoleState(controlplanev1.AccessRoleState_value["ACCESS_ROLE_STATE_"+value])
}
func bindingStateEnum(value string) controlplanev1.AccessBindingState {
	return controlplanev1.AccessBindingState(controlplanev1.AccessBindingState_value["ACCESS_BINDING_STATE_"+value])
}
func oidcGroupState(value string) controlplanev1.OIDCGroupState {
	return controlplanev1.OIDCGroupState(controlplanev1.OIDCGroupState_value["OIDC_GROUP_STATE_"+value])
}
