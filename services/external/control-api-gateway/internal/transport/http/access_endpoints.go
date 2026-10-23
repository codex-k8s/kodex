package httptransport

import (
	"net/http"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListPermissionRegistry reads the owner's closed permission catalog and public-projects it without granting any listed permission.
// ListPermissionRegistry читает закрытый каталог разрешений владельца и публично проецирует его без выдачи перечисленных прав.
func (server *Server) ListPermissionRegistry(writer http.ResponseWriter, request *http.Request) {
	response, err := server.control.Access.ListPermissionRegistry(request.Context(), &controlplanev1.ListPermissionRegistryRequest{})
	writeReadRPCResult(writer, response, err, "", "permissions")
}

// ListAccessSubjects forwards search/page and a mapped optional subject kind to owner eligibility, returning public subject metadata rather than selecting caller identity.
// ListAccessSubjects передаёт поиск, страницу и необязательный вид субъекта допуску владельца, возвращая metadata вместо назначения личности вызывающего.
func (server *Server) ListAccessSubjects(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessSubjectsParams) {
	response, err := server.control.Access.ListAccessSubjects(request.Context(), &controlplanev1.ListAccessSubjectsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), Query: stringValue(parameters.Query),
		Kind: protoSubjectKind(stringValue(parameters.Kind)),
	})
	writeReadRPCResult(writer, response, err, "", "subjects")
}

// ListOIDCGroups reads owner-visible IdP group metadata with pagination and search, without changing membership or trusting selected groups as caller claims.
// ListOIDCGroups читает видимую metadata групп IdP с пагинацией и поиском без изменения членства и принятия выбранных групп за claims вызывающего.
func (server *Server) ListOIDCGroups(writer http.ResponseWriter, request *http.Request, parameters generated.ListOIDCGroupsParams) {
	response, err := server.control.Access.ListOIDCGroups(request.Context(), &controlplanev1.ListOIDCGroupsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), Query: stringValue(parameters.Query),
	})
	writeReadRPCResult(writer, response, err, "", "groups")
}

// ListAccessRoles adds localized system-role search aliases to the exact query and forwards page/archive selection to the owner, returning metadata without activating a role.
// ListAccessRoles добавляет локализованные поисковые aliases системных ролей к точному запросу и передаёт страницу и выбор архива владельцу, возвращая metadata без активации роли.
func (server *Server) ListAccessRoles(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessRolesParams) {
	search := stringValue(parameters.Query)
	response, err := server.control.Access.ListAccessRoles(request.Context(), &controlplanev1.ListAccessRolesRequest{
		Page: page(parameters.PageSize, parameters.PageToken), IncludeArchived: boolValue(parameters.IncludeArchived),
		Query: search, QueryAliases: accessRoleQueryAliases(writer, search),
	})
	writeReadRPCResult(writer, response, err, "", "roles")
}

// accessRoleQueryAliases finds matches in ten fixed localized role name/description messages and returns only their i18n identifiers. Missing localizer or blank search returns nil; aliases aid search and never establish authorization.
// accessRoleQueryAliases ищет совпадения в десяти фиксированных переводах имён и описаний ролей и возвращает только их i18n-идентификаторы. Без localizer и при пустом поиске даёт nil; aliases помогают поиску, но не авторизации.
func accessRoleQueryAliases(writer http.ResponseWriter, search string) []string {
	localizer, ok := writer.(interface{ Localize(string) string })
	needle := strings.ToLower(strings.TrimSpace(search))
	if !ok || needle == "" {
		return nil
	}
	messageIDs := [...]string{
		"SYSTEM_ROLE_OWNER", "SYSTEM_ROLE_OWNER_DESCRIPTION",
		"SYSTEM_ROLE_ADMINISTRATOR", "SYSTEM_ROLE_ADMINISTRATOR_DESCRIPTION",
		"SYSTEM_ROLE_OPERATOR", "SYSTEM_ROLE_OPERATOR_DESCRIPTION",
		"SYSTEM_ROLE_MEMBER", "SYSTEM_ROLE_MEMBER_DESCRIPTION",
		"SYSTEM_ROLE_AUDITOR", "SYSTEM_ROLE_AUDITOR_DESCRIPTION",
	}
	aliases := make([]string, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		if strings.Contains(strings.ToLower(localizer.Localize(messageID)), needle) {
			aliases = append(aliases, "i18n:"+messageID)
		}
	}
	return aliases
}

// ListAccessRoleVersions reads the selected role's owner-filtered historical page without publishing or activating a version.
// ListAccessRoleVersions читает историю выбранной роли у владельца без публикации и активации версии.
func (server *Server) ListAccessRoleVersions(writer http.ResponseWriter, request *http.Request, roleRef generated.AccessRoleRef, parameters generated.ListAccessRoleVersionsParams) {
	response, err := server.control.Access.ListAccessRoleVersions(request.Context(), &controlplanev1.ListAccessRoleVersionsRequest{
		RoleRef: roleRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	writeReadRPCResult(writer, response, err, "", "versions")
}

// ListAccessBindings forwards exact subject/role/project selectors, search aliases and revoked visibility to the owner, exposing public binding metadata without inferring access from those requested selectors.
// ListAccessBindings передаёт точные subject/role/project, поисковые aliases и видимость revoked владельцу, выдавая metadata без вывода доступа из запрошенных селекторов.
func (server *Server) ListAccessBindings(writer http.ResponseWriter, request *http.Request, parameters generated.ListAccessBindingsParams) {
	search := stringValue(parameters.Query)
	response, err := server.control.Access.ListAccessBindings(request.Context(), &controlplanev1.ListAccessBindingsRequest{
		Page: page(parameters.PageSize, parameters.PageToken), SubjectKind: protoSubjectKind(stringValue(parameters.SubjectKind)),
		SubjectRef: stringValue(parameters.SubjectRef), RoleRef: stringValue(parameters.RoleRef), ProjectRef: stringValue(parameters.ProjectRef),
		IncludeRevoked: boolValue(parameters.IncludeRevoked), Query: search, QueryAliases: accessRoleQueryAliases(writer, search),
	})
	writeReadRPCResult(writer, response, err, "", "bindings")
}

// QueryEffectiveAccess decodes a read-only query, copies requested permission keys and maps the target for owner evaluation. Requested subject and target describe the evaluated case, not authenticated caller authority.
// QueryEffectiveAccess разбирает запрос чтения, копирует permission keys и преобразует target для оценки владельцем. Запрошенные subject и target описывают проверяемый случай, а не полномочия вызывающего.
func (server *Server) QueryEffectiveAccess(writer http.ResponseWriter, request *http.Request) {
	body, ok := decodeJSON[generated.EffectiveAccessQuery](writer, request)
	if !ok {
		return
	}
	response, err := server.control.Access.QueryEffectiveAccess(request.Context(), &controlplanev1.QueryEffectiveAccessRequest{
		SubjectRef: stringValue(body.SubjectRef), Target: protoAccessScope(body.Target), PermissionKeys: append([]string(nil), body.PermissionKeys...),
	})
	writeReadRPCResult(writer, response, err, "", "decisions")
}

// ExplainAccess forwards optional subject, exact permission and mapped target to the owner explanation read without mutating policy or granting the explained permission.
// ExplainAccess передаёт необязательного субъекта, точное разрешение и target чтению объяснения владельца без изменения политики и выдачи разрешения.
func (server *Server) ExplainAccess(writer http.ResponseWriter, request *http.Request) {
	body, ok := decodeJSON[generated.ExplainAccessInput](writer, request)
	if !ok {
		return
	}
	response, err := server.control.Access.ExplainAccess(request.Context(), &controlplanev1.ExplainAccessRequest{
		SubjectRef: stringValue(body.SubjectRef), PermissionKey: body.PermissionKey, Target: protoAccessScope(body.Target),
	})
	writeReadRPCResult(writer, response, err, "", "")
}

// SimulateAccess maps hypothetical role/binding/conditions and optional UTC evaluation time into a read-only owner simulation. It copies permission keys but never persists or authorizes the proposed role or binding.
// SimulateAccess преобразует гипотетические роль, binding, conditions и необязательное UTC-время в симуляцию чтения владельца. Копирует permission keys, но предложенные роль и binding не сохраняет и не авторизует.
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
	writeReadRPCResult(writer, response, err, "", "")
}

// CreateAccessRole copies requested permission keys/scopes with editable metadata and create idempotency context into the dedicated owner command, returning HTTP201 without treating requested privileges as self-issued authority.
// CreateAccessRole копирует permission keys и scopes с metadata и идемпотентным create в отдельную команду владельца и выдаёт HTTP201 без принятия запрошенных прав за самовыданные полномочия.
func (server *Server) CreateAccessRole(writer http.ResponseWriter, request *http.Request, parameters generated.CreateAccessRoleParams) {
	body, mutation, ok := decodeMutationBody[generated.AccessRoleInput](writer, request, parameters.IdempotencyKey, "")
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

// CreateAccessRoleVersion sends full proposed role metadata and mutation/version pins to the owner version command; it does not overwrite historical versions locally.
// CreateAccessRoleVersion передаёт полную предлагаемую metadata роли и mutation/version pins команде версий владельца без локального перезаписывания истории.
func (server *Server) CreateAccessRoleVersion(writer http.ResponseWriter, request *http.Request, roleRef generated.AccessRoleRef, parameters generated.CreateAccessRoleVersionParams) {
	body, mutation, ok := decodeMutationBody[generated.AccessRoleInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
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

// ArchiveAccessRole delegates exact role archival with mutation/version pins and public-projects the result; dependency and binding effects remain owner-managed.
// ArchiveAccessRole поручает архивирование точной роли с mutation/version pins и проецирует результат; зависимости и эффекты bindings остаются у владельца.
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

// CreateAccessBinding maps subject, exact role-version selector, scope and conditions under create mutation context for owner authorization. Payload subject/scope never assigns the caller's own identity or permissions.
// CreateAccessBinding преобразует subject, точную role-version, scope и conditions с create mutation для авторизации владельцем. Subject и scope payload не назначают личность и права вызывающего.
func (server *Server) CreateAccessBinding(writer http.ResponseWriter, request *http.Request, parameters generated.CreateAccessBindingParams) {
	body, mutation, ok := decodeMutationBody[generated.AccessBindingInput](writer, request, parameters.IdempotencyKey, "")
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

// ChangeAccessBinding sends exact binding, role version, scope/conditions and mutation pins to the specialized owner update, leaving old/new scope eligibility to owner policy.
// ChangeAccessBinding передаёт точные binding, role version, scope/conditions и mutation pins отдельному update владельца, оставляя допуск прежней и новой области его политике.
func (server *Server) ChangeAccessBinding(writer http.ResponseWriter, request *http.Request, bindingRef generated.AccessBindingRef, parameters generated.ChangeAccessBindingParams) {
	body, mutation, ok := decodeMutationBody[generated.AccessBindingChangeInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
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

// RevokeAccessBinding invokes explicit owner revocation with exact binding/version/idempotency and returns public state without directly editing IdP groups or session credentials.
// RevokeAccessBinding вызывает явный отзыв владельца с точными binding, версией и идемпотентностью и выдаёт состояние без прямого изменения групп IdP и credentials сессий.
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

// protoAccessScope maps public kind/project/resource selectors into a fresh protobuf scope without validating their consistency or making them authoritative actor claims.
// protoAccessScope преобразует публичные kind/project/resource в новый protobuf scope без проверки согласованности и превращения их в авторитетные claims actor.
func protoAccessScope(value generated.AccessScope) *controlplanev1.AccessScope {
	return &controlplanev1.AccessScope{Kind: protoScopeKind(string(value.Kind)), ProjectRef: stringValue(value.ProjectRef), ResourceKind: protoResourceKind(stringValue(value.ResourceKind)), ResourceRef: stringValue(value.ResourceRef)}
}

// protoAccessConditions copies optional validity times and RequireOwner into protobuf; it does not check time ordering or prove owner status.
// protoAccessConditions копирует необязательные сроки и RequireOwner в protobuf без проверки порядка времени и доказательства статуса владельца.
func protoAccessConditions(value generated.AccessConditions) *controlplanev1.AccessConditions {
	return &controlplanev1.AccessConditions{ValidFrom: protoOptionalTime(value.ValidFrom), ValidUntil: protoOptionalTime(value.ValidUntil), RequireOwner: value.RequireOwner}
}

// protoOptionalTime preserves absent time as nil and converts present values to UTC protobuf timestamps without calling CheckValid or changing the original time.
// protoOptionalTime сохраняет отсутствие как nil и преобразует заданное время в UTC protobuf timestamp без CheckValid и изменения исходного времени.
func protoOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}

// protoScopeKinds returns a fresh typed scope-kind slice in original order, including zero results for unknown names and duplicates rather than silently validating them.
// protoScopeKinds возвращает новый типизированный список scope-kind в исходном порядке, включая нули неизвестных имён и дубликаты, а не неявно проверяя их.
func protoScopeKinds(values []generated.AccessScopeKind) []controlplanev1.AccessScopeKind {
	result := make([]controlplanev1.AccessScopeKind, 0, len(values))
	for _, value := range values {
		result = append(result, protoScopeKind(string(value)))
	}
	return result
}

// protoSubjectKind performs exact prefixed enum lookup, returning zero for an unknown subject name; it does not resolve an authenticated principal.
// protoSubjectKind точно ищет enum с префиксом, возвращая ноль неизвестному имени субъекта; аутентифицированного principal не определяет.
func protoSubjectKind(value string) controlplanev1.AccessSubjectKind {
	return controlplanev1.AccessSubjectKind(controlplanev1.AccessSubjectKind_value["ACCESS_SUBJECT_KIND_"+value])
}

// protoScopeKind maps a public scope name to its protobuf enum or zero, leaving validity and resource eligibility to their explicit validators.
// protoScopeKind преобразует публичное имя области в protobuf enum либо ноль, оставляя корректность и допуск явным validators.
func protoScopeKind(value string) controlplanev1.AccessScopeKind {
	return controlplanev1.AccessScopeKind(controlplanev1.AccessScopeKind_value["ACCESS_SCOPE_KIND_"+value])
}

// protoResourceKind maps the prefixed resource-kind name with zero fallback without resolving resource ownership or permissions.
// protoResourceKind преобразует имя вида ресурса с префиксом и нулевым запасным значением без определения владения и прав.
func protoResourceKind(value string) controlplanev1.AccessResourceKind {
	return controlplanev1.AccessResourceKind(controlplanev1.AccessResourceKind_value["ACCESS_RESOURCE_KIND_"+value])
}

// boolValue treats absence and an explicit false equally and returns true only for a nonnil true pointer, without preserving optional presence.
// boolValue одинаково трактует отсутствие и явный false, возвращая true только ненулевому true pointer без сохранения optional presence.
func boolValue(value *bool) bool { return value != nil && *value }
