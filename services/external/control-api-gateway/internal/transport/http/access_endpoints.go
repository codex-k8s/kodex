package httptransport

import (
	"net/http"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ListPermissionRegistry(writer http.ResponseWriter, request *http.Request) {
	response, err := server.control.Access.ListPermissionRegistry(request.Context(), &controlplanev1.ListPermissionRegistryRequest{})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "permissions")
}

func (server *Server) ListAccessSubjects(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessSubjectsParams) {
	response, err := server.control.Access.ListAccessSubjects(request.Context(), &controlplanev1.ListAccessSubjectsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), Query: stringValue(parameters.Query),
		Kind: protoSubjectKind(stringValue(parameters.Kind)),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "subjects")
}

func (server *Server) ListOIDCGroups(writer http.ResponseWriter, request *http.Request, parameters generated.ListOIDCGroupsParams) {
	response, err := server.control.Access.ListOIDCGroups(request.Context(), &controlplanev1.ListOIDCGroupsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), Query: stringValue(parameters.Query),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "groups")
}

func (server *Server) ListAccessRoles(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessRolesParams) {
	response, err := server.control.Access.ListAccessRoles(request.Context(), &controlplanev1.ListAccessRolesRequest{
		Page: page(parameters.PageSize, parameters.PageToken), IncludeArchived: boolValue(parameters.IncludeArchived),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "roles")
}

func (server *Server) ListAccessRoleVersions(writer http.ResponseWriter, request *http.Request, roleRef generated.AccessRoleRef, parameters generated.ListAccessRoleVersionsParams) {
	response, err := server.control.Access.ListAccessRoleVersions(request.Context(), &controlplanev1.ListAccessRoleVersionsRequest{
		RoleRef: roleRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "versions")
}

func (server *Server) ListAccessBindings(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessBindingsParams) {
	response, err := server.control.Access.ListAccessBindings(request.Context(), &controlplanev1.ListAccessBindingsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), SubjectKind: protoSubjectKind(stringValue(parameters.SubjectKind)),
		SubjectRef: stringValue(parameters.SubjectRef), RoleRef: stringValue(parameters.RoleRef), ProjectRef: stringValue(parameters.ProjectRef),
		IncludeRevoked: boolValue(parameters.IncludeRevoked),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "bindings")
}

func (server *Server) QueryEffectiveAccess(writer http.ResponseWriter, request *http.Request) {
	body, ok := decodeJSON[generated.EffectiveAccessQuery](writer, request)
	if !ok {
		return
	}
	response, err := server.control.Access.QueryEffectiveAccess(request.Context(), &controlplanev1.QueryEffectiveAccessRequest{
		SubjectRef: stringValue(body.SubjectRef), Target: protoAccessScope(body.Target), PermissionKeys: append([]string(nil), body.PermissionKeys...),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "decisions")
}

func (server *Server) ExplainAccess(writer http.ResponseWriter, request *http.Request) {
	body, ok := decodeJSON[generated.ExplainAccessInput](writer, request)
	if !ok {
		return
	}
	response, err := server.control.Access.ExplainAccess(request.Context(), &controlplanev1.ExplainAccessRequest{
		SubjectRef: stringValue(body.SubjectRef), PermissionKey: body.PermissionKey, Target: protoAccessScope(body.Target),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "")
}

func (server *Server) SimulateAccess(writer http.ResponseWriter, request *http.Request) {
	body, ok := decodeJSON[generated.SimulateAccessInput](writer, request)
	if !ok {
		return
	}
	response, err := server.control.Access.SimulateAccess(request.Context(), &controlplanev1.SimulateAccessRequest{
		SubjectRef: body.SubjectRef, PermissionKey: body.PermissionKey, Target: protoAccessScope(body.Target),
		Role:        &controlplanev1.AccessRoleDraft{PermissionKeys: append([]string(nil), body.Role.PermissionKeys...), AllowedScopes: protoScopeKinds(body.Role.AllowedScopes)},
		Binding:     &controlplanev1.AccessBindingDraft{SubjectKind: protoSubjectKind(string(body.Binding.SubjectKind)), SubjectRef: body.Binding.SubjectRef, Scope: protoAccessScope(body.Binding.Scope), Conditions: protoAccessConditions(body.Binding.Conditions)},
		EvaluatedAt: protoOptionalTime(body.EvaluatedAt),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "")
}

func (server *Server) CreateAccessRole(writer http.ResponseWriter, request *http.Request, parameters generated.CreateAccessRoleParams) {
	body, ok := decodeJSON[generated.AccessRoleInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Access.CreateAccessRole(request.Context(), &controlplanev1.CreateAccessRoleRequest{
		Mutation: mutation, Name: body.Name, Description: body.Description, PermissionKeys: append([]string(nil), body.PermissionKeys...), AllowedScopes: protoScopeKinds(body.AllowedScopes), ChangeComment: body.ChangeComment,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "role", "")
}

func (server *Server) CreateAccessRoleVersion(writer http.ResponseWriter, request *http.Request, roleRef generated.AccessRoleRef, parameters generated.CreateAccessRoleVersionParams) {
	body, ok := decodeJSON[generated.AccessRoleInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Access.CreateAccessRoleVersion(request.Context(), &controlplanev1.CreateAccessRoleVersionRequest{
		Mutation: mutation, RoleRef: roleRef, Name: body.Name, Description: body.Description, PermissionKeys: append([]string(nil), body.PermissionKeys...), AllowedScopes: protoScopeKinds(body.AllowedScopes), ChangeComment: body.ChangeComment,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "role", "")
}

func (server *Server) ArchiveAccessRole(writer http.ResponseWriter, request *http.Request, roleRef generated.AccessRoleRef, parameters generated.ArchiveAccessRoleParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Access.ArchiveAccessRole(request.Context(), &controlplanev1.ArchiveAccessRoleRequest{Mutation: mutation, RoleRef: roleRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "role", "")
}

func (server *Server) CreateAccessBinding(writer http.ResponseWriter, request *http.Request, parameters generated.CreateAccessBindingParams) {
	body, ok := decodeJSON[generated.AccessBindingInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Access.CreateAccessBinding(request.Context(), &controlplanev1.CreateAccessBindingRequest{
		Mutation: mutation, SubjectKind: protoSubjectKind(string(body.SubjectKind)), SubjectRef: body.SubjectRef,
		RoleVersionRef: body.RoleVersionRef, Scope: protoAccessScope(body.Scope), Conditions: protoAccessConditions(body.Conditions),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "binding", "")
}

func (server *Server) ChangeAccessBinding(writer http.ResponseWriter, request *http.Request, bindingRef generated.AccessBindingRef, parameters generated.ChangeAccessBindingParams) {
	body, ok := decodeJSON[generated.AccessBindingChangeInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Access.ChangeAccessBinding(request.Context(), &controlplanev1.ChangeAccessBindingRequest{
		Mutation: mutation, BindingRef: bindingRef, RoleVersionRef: body.RoleVersionRef, Scope: protoAccessScope(body.Scope), Conditions: protoAccessConditions(body.Conditions),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "binding", "")
}

func (server *Server) RevokeAccessBinding(writer http.ResponseWriter, request *http.Request, bindingRef generated.AccessBindingRef, parameters generated.RevokeAccessBindingParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Access.RevokeAccessBinding(request.Context(), &controlplanev1.RevokeAccessBindingRequest{Mutation: mutation, BindingRef: bindingRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "binding", "")
}

func protoAccessScope(value generated.AccessScope) *controlplanev1.AccessScope {
	return &controlplanev1.AccessScope{Kind: protoScopeKind(string(value.Kind)), ProjectRef: stringValue(value.ProjectRef), ResourceKind: protoResourceKind(stringValue(value.ResourceKind)), ResourceRef: stringValue(value.ResourceRef)}
}

func protoAccessConditions(value generated.AccessConditions) *controlplanev1.AccessConditions {
	return &controlplanev1.AccessConditions{ValidFrom: protoOptionalTime(value.ValidFrom), ValidUntil: protoOptionalTime(value.ValidUntil), RequireOwner: value.RequireOwner}
}

func protoOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}

func protoScopeKinds(values []generated.AccessScopeKind) []controlplanev1.AccessScopeKind {
	result := make([]controlplanev1.AccessScopeKind, 0, len(values))
	for _, value := range values {
		result = append(result, protoScopeKind(string(value)))
	}
	return result
}

func protoSubjectKind(value string) controlplanev1.AccessSubjectKind {
	return controlplanev1.AccessSubjectKind(controlplanev1.AccessSubjectKind_value["ACCESS_SUBJECT_KIND_"+value])
}

func protoScopeKind(value string) controlplanev1.AccessScopeKind {
	return controlplanev1.AccessScopeKind(controlplanev1.AccessScopeKind_value["ACCESS_SCOPE_KIND_"+value])
}

func protoResourceKind(value string) controlplanev1.AccessResourceKind {
	return controlplanev1.AccessResourceKind(controlplanev1.AccessResourceKind_value["ACCESS_RESOURCE_KIND_"+value])
}

func boolValue(value *bool) bool { return value != nil && *value }
