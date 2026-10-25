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

// ListPermissionRegistry reads the permission catalog through the exact AccessService method principal and casts definitions; this registry describes available permissions rather than granting the caller or a listed subject any access. Domain failures retain transportError mapping.
// ListPermissionRegistry читает каталог полномочий с principal точного метода AccessService и преобразует определения; реестр описывает доступные полномочия, но не выдаёт доступ вызывающему или перечисленному субъекту. Доменные отказы сохраняют отображение transportError.
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
	response.Permissions = castResponseItems(items, castPermissionDefinition)
	return response, nil
}

// ListAccessSubjects resolves the verified method principal, forwards query/page and the requested subject kind, and casts only the returned subject page with its next token. Subject filters are query data and do not establish caller identity or membership.
// ListAccessSubjects разрешает проверенный principal метода, передаёт query/page и запрошенный вид субъекта и преобразует лишь возвращённую страницу с её следующим токеном. Фильтры являются данными запроса и не назначают идентичность или членство вызывающего.
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
	response.Subjects = castResponseItems(items, castAccessSubject)
	return response, nil
}

// ListOIDCGroups pages stored OIDC-group metadata through the authenticated AccessService operation and returns the repository cursor unchanged; casting group counts and synchronization timestamps neither queries the identity provider nor changes membership.
// ListOIDCGroups читает страницы сохранённых метаданных OIDC-групп через аутентифицированную операцию AccessService и возвращает курсор репозитория без изменения; преобразование счётчиков и времени синхронизации не запрашивает провайдера идентичности и не меняет членство.
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
	response.Groups = castResponseItems(items, castOIDCGroup)
	return response, nil
}

// ListAccessRoles forwards query aliases, paging and the explicit includeArchived flag under the exact method principal, then casts the returned role page. Including archived metadata does not reactivate a role or bypass repository eligibility.
// ListAccessRoles передаёт поисковые варианты, пагинацию и явный includeArchived с principal точного метода, затем преобразует страницу ролей. Включение архивных метаданных не активирует роль и не обходит проверку доступности репозитория.
func (server *Server) ListAccessRoles(ctx context.Context, request *controlplanev1.ListAccessRolesRequest) (*controlplanev1.ListAccessRolesResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessRoles_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAccessRoles(ctx, p, query.Filter{
		Query: request.GetQuery(), Aliases: request.GetQueryAliases(), Page: page(request.GetPage()),
	}, request.GetIncludeArchived())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessRolesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Roles = castResponseItems(items, castAccessRole)
	return response, nil
}

// ListAccessRoleVersions requests the exact role reference and version page with a verified method principal, returning the current role metadata together with stored versions and cursor. Reading a version neither publishes it nor attaches it to a binding.
// ListAccessRoleVersions запрашивает точную ссылку роли и страницу версий с проверенным principal метода, возвращая текущие метаданные роли, сохранённые версии и курсор. Чтение версии не публикует её и не назначает привязке.
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
	response.Versions = castResponseItems(items, castAccessRoleVersion)
	return response, nil
}

// ListAccessBindings builds the typed binding filter from page, subject/role/project selectors, revoked-state inclusion and query aliases, then casts the authorized result page. Payload references select resources; authority still comes from the verified method principal and service.
// ListAccessBindings строит типизированный фильтр привязок из страницы, селекторов субъекта/роли/проекта, включения отозванных записей и поисковых вариантов, затем преобразует разрешённую страницу. Ссылки payload выбирают ресурсы; полномочия по-прежнему определяют проверенный principal метода и сервис.
func (server *Server) ListAccessBindings(ctx context.Context, request *controlplanev1.ListAccessBindingsRequest) (*controlplanev1.ListAccessBindingsResponse, error) {
	p, err := principal(ctx, controlplanev1.AccessService_ListAccessBindings_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAccessBindings(ctx, p, query.AccessBindingFilter{
		Page: page(request.GetPage()), SubjectKind: accessSubjectKind(request.GetSubjectKind()),
		SubjectRef: request.GetSubjectRef(), RoleRef: request.GetRoleRef(), ProjectRef: request.GetProjectRef(),
		IncludeRevoked: request.GetIncludeRevoked(), Query: request.GetQuery(), Aliases: request.GetQueryAliases(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAccessBindingsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Bindings = castResponseItems(items, castAccessBinding)
	return response, nil
}

// QueryEffectiveAccess evaluates the requested subject, target and permission keys through the authorized service, supplying zero time so the owner chooses the evaluation instant. Returns that instant and each explanation without treating the queried subject as the caller.
// QueryEffectiveAccess вычисляет доступ запрошенного субъекта к цели и ключам полномочий через авторизованный сервис, передавая нулевое время для выбора момента владельцем данных. Возвращает этот момент и все объяснения, не считая запрошенного субъекта вызывающим.
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

// ExplainAccess queries exactly one permission under the verified method principal and requires exactly one returned decision before casting its explanation. An unexpected decision cardinality maps to Invalid rather than selecting an arbitrary result.
// ExplainAccess запрашивает ровно одно полномочие с проверенным principal метода и требует единственное возвращённое решение перед преобразованием объяснения. Неожиданное количество решений отображается в Invalid вместо выбора произвольного результата.
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

// SimulateAccess validates the optional evaluation timestamp and constructs a hypothetical role/binding with a fixed simulation version reference, copied permissions and mapped scope/conditions. Returns current and simulated decisions through the service; it does not persist the hypothetical binding.
// SimulateAccess проверяет необязательный момент оценки и строит гипотетические роль/привязку с фиксированной ссылкой версии simulation, копией полномочий и преобразованными scope/conditions. Возвращает текущее и моделируемое решения через сервис; гипотетическую привязку не сохраняет.
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

// CreateAccessRole maps role fields and mutation metadata into the specialized CreateAccessRole command using the exact authenticated RPC path, then casts the owner result. The request supplies role content rather than caller identity or a preassigned role reference.
// CreateAccessRole преобразует поля роли и mutation metadata в специализированную команду CreateAccessRole через точный аутентифицированный RPC-путь, затем преобразует результат владельца. Запрос задаёт содержимое роли, а не идентичность вызывающего или заранее назначенную ссылку роли.
func (server *Server) CreateAccessRole(ctx context.Context, request *controlplanev1.CreateAccessRoleRequest) (*controlplanev1.CreateAccessRoleResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_CreateAccessRole_FullMethodName, command.CreateAccessRole, request.GetMutation(), accessRoleInput("", request.GetName(), request.GetDescription(), request.GetPermissionKeys(), request.GetAllowedScopes(), request.GetChangeComment()), func(result command.Result) *controlplanev1.CreateAccessRoleResponse {
		return &controlplanev1.CreateAccessRoleResponse{Role: castAccessRole(*result.AccessRole)}
	})
}

// CreateAccessRoleVersion passes the existing role reference, complete proposed content and mutation metadata to the specialized version command. Shared execute keeps principal/error handling; the domain owner controls authorization, expected version and the resulting role snapshot.
// CreateAccessRoleVersion передаёт ссылку существующей роли, полное предлагаемое содержимое и mutation metadata специализированной команде версии. Общий execute сохраняет обработку principal/ошибок; доменный владелец проверяет полномочия, ожидаемую версию и формирует снимок роли.
func (server *Server) CreateAccessRoleVersion(ctx context.Context, request *controlplanev1.CreateAccessRoleVersionRequest) (*controlplanev1.CreateAccessRoleVersionResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_CreateAccessRoleVersion_FullMethodName, command.CreateAccessRoleVersion, request.GetMutation(), accessRoleInput(request.GetRoleRef(), request.GetName(), request.GetDescription(), request.GetPermissionKeys(), request.GetAllowedScopes(), request.GetChangeComment()), func(result command.Result) *controlplanev1.CreateAccessRoleVersionResponse {
		return &controlplanev1.CreateAccessRoleVersionResponse{Role: castAccessRole(*result.AccessRole)}
	})
}

// ArchiveAccessRole invokes only the specialized archive command with the requested role reference and mutation metadata, returning the resulting role. This transport adapter neither performs a generic delete nor manufactures an archive state on command failure.
// ArchiveAccessRole вызывает только специализированную команду архивирования со ссылкой роли и mutation metadata, возвращая итоговую роль. Этот transport adapter не выполняет универсальное удаление и не назначает архивное состояние при отказе команды.
func (server *Server) ArchiveAccessRole(ctx context.Context, request *controlplanev1.ArchiveAccessRoleRequest) (*controlplanev1.ArchiveAccessRoleResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_ArchiveAccessRole_FullMethodName, command.ArchiveAccessRole, request.GetMutation(), command.AccessRoleInput{RoleRef: request.GetRoleRef()}, func(result command.Result) *controlplanev1.ArchiveAccessRoleResponse {
		return &controlplanev1.ArchiveAccessRoleResponse{Role: castAccessRole(*result.AccessRole)}
	})
}

// CreateAccessBinding maps the requested subject, exact role-version reference, scope and conditions into the specialized binding command. Mutation and verified RPC principal pass through execute; actual eligibility, ownership and persistence remain with the domain owner.
// CreateAccessBinding преобразует запрошенного субъекта, точную ссылку версии роли, область и условия в специализированную команду привязки. Mutation и проверенный RPC principal проходят через execute; реальную доступность, владение и сохранение проверяет доменный владелец.
func (server *Server) CreateAccessBinding(ctx context.Context, request *controlplanev1.CreateAccessBindingRequest) (*controlplanev1.CreateAccessBindingResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_CreateAccessBinding_FullMethodName, command.CreateAccessBinding, request.GetMutation(), command.AccessBindingInput{SubjectKind: accessSubjectKind(request.GetSubjectKind()), SubjectRef: request.GetSubjectRef(), RoleVersionRef: request.GetRoleVersionRef(), Scope: domainAccessScope(request.GetScope()), Conditions: domainAccessConditions(request.GetConditions())}, func(result command.Result) *controlplanev1.CreateAccessBindingResponse {
		return &controlplanev1.CreateAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}
	})
}

// ChangeAccessBinding submits only the existing binding reference, replacement role version, scope and conditions with mutation metadata to the specialized change command. It does not accept a replacement subject identity and returns the owner-produced binding snapshot.
// ChangeAccessBinding передаёт лишь ссылку существующей привязки, новую версию роли, область и условия с mutation metadata специализированной команде изменения. Не принимает замену идентичности субъекта и возвращает сформированный владельцем снимок привязки.
func (server *Server) ChangeAccessBinding(ctx context.Context, request *controlplanev1.ChangeAccessBindingRequest) (*controlplanev1.ChangeAccessBindingResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_ChangeAccessBinding_FullMethodName, command.ChangeAccessBinding, request.GetMutation(), command.AccessBindingInput{BindingRef: request.GetBindingRef(), RoleVersionRef: request.GetRoleVersionRef(), Scope: domainAccessScope(request.GetScope()), Conditions: domainAccessConditions(request.GetConditions())}, func(result command.Result) *controlplanev1.ChangeAccessBindingResponse {
		return &controlplanev1.ChangeAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}
	})
}

// RevokeAccessBinding routes the exact binding reference and mutation metadata through the authenticated revoke command, then casts its result. Revocation authority and lifecycle are checked by the command path, not inferred from possession of a binding reference.
// RevokeAccessBinding направляет точную ссылку привязки и mutation metadata через аутентифицированную команду отзыва, затем преобразует результат. Полномочия отзыва и жизненный цикл проверяет командный путь, а не факт наличия ссылки привязки.
func (server *Server) RevokeAccessBinding(ctx context.Context, request *controlplanev1.RevokeAccessBindingRequest) (*controlplanev1.RevokeAccessBindingResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.AccessService_RevokeAccessBinding_FullMethodName, command.RevokeAccessBinding, request.GetMutation(), command.AccessBindingInput{BindingRef: request.GetBindingRef()}, func(result command.Result) *controlplanev1.RevokeAccessBindingResponse {
		return &controlplanev1.RevokeAccessBindingResponse{Binding: castAccessBinding(*result.AccessBinding)}
	})
}

// accessRoleInput builds domain role content from transport fields, copying permission keys and converting scope kinds while preserving name, description and change comment. Empty roleRef denotes creation; this value constructor does not authorize a role mutation.
// accessRoleInput строит доменное содержимое роли из transport-полей, копируя ключи полномочий и преобразуя виды областей с сохранением имени, описания и комментария изменения. Пустой roleRef означает создание; этот конструктор значения не разрешает изменение роли.
func accessRoleInput(roleRef, name, description string, permissions []string, scopes []controlplanev1.AccessScopeKind, comment string) command.AccessRoleInput {
	return command.AccessRoleInput{RoleRef: roleRef, Name: name, Description: description, PermissionKeys: append([]string(nil), permissions...), AllowedScopes: accessScopeKinds(scopes), ChangeComment: comment}
}

// domainAccessScope maps a nil wire scope to the zero domain value and otherwise preserves project/resource references with their converted kinds. It translates query shape only; existence, tenant ownership and permission eligibility require the owner service.
// domainAccessScope преобразует nil wire scope в нулевое доменное значение, иначе сохраняет ссылки проекта/ресурса вместе с преобразованными видами. Переводит лишь форму запроса; существование, tenant-владение и доступность проверяет сервис-владелец.
func domainAccessScope(input *controlplanev1.AccessScope) entity.AccessScope {
	if input == nil {
		return entity.AccessScope{}
	}
	return entity.AccessScope{Kind: accessScopeKind(input.GetKind()), ProjectRef: input.GetProjectRef(), ResourceKind: accessResourceKind(input.GetResourceKind()), ResourceRef: input.GetResourceRef()}
}

// domainAccessConditions copies RequireOwner and only present, valid protobuf timestamps into independent time pointers; nil input yields zero conditions. Existing malformed timestamps are omitted by this mapper rather than rejected, so it must not be described as a validating authorization boundary.
// domainAccessConditions копирует RequireOwner и лишь присутствующие корректные protobuf timestamps в отдельные указатели времени; nil даёт нулевые условия. Некорректные timestamps этот mapper по прежнему поведению пропускает, а не отклоняет, поэтому его нельзя считать проверяющей границей авторизации.
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

// optionalDomainTime distinguishes an omitted timestamp from a valid explicit evaluation instant and rejects an invalid protobuf timestamp with ErrInvalid. It converts the supplied instant without selecting a server clock value or mutating any access state.
// optionalDomainTime отличает отсутствующий timestamp от корректного явно заданного момента оценки и отклоняет некорректный protobuf timestamp через ErrInvalid. Преобразует переданный момент без выбора серверного времени или изменения состояния доступа.
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

// accessSubjectKind maps the unspecified subject-kind enum to an empty filter and strips the known prefix from other enum strings. Unknown numeric values are not validated here; downstream validation remains responsible for rejecting unsupported kinds.
// accessSubjectKind преобразует unspecified вид субъекта в пустой фильтр и удаляет известный префикс у остальных enum strings. Неизвестные числовые значения здесь не проверяются; неподдерживаемые виды должна отклонять последующая валидация.
func accessSubjectKind(value controlplanev1.AccessSubjectKind) string {
	if value == controlplanev1.AccessSubjectKind_ACCESS_SUBJECT_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_SUBJECT_KIND_")
}

// accessScopeKind converts an unspecified wire scope kind to the empty domain value and otherwise removes ACCESS_SCOPE_KIND_ from its string form. It does not infer project/resource authority from the selected scope kind.
// accessScopeKind преобразует unspecified wire вид области в пустое доменное значение, иначе удаляет ACCESS_SCOPE_KIND_ из строкового представления. Не выводит полномочия проекта/ресурса из выбранного вида области.
func accessScopeKind(value controlplanev1.AccessScopeKind) string {
	if value == controlplanev1.AccessScopeKind_ACCESS_SCOPE_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_SCOPE_KIND_")
}

// accessScopeKinds allocates an ordered domain scope-kind slice, omitting only entries converted to empty strings and preserving duplicates and other values. This conversion does not normalize or validate a proposed role permission policy.
// accessScopeKinds создаёт упорядоченный доменный список видов областей, пропуская лишь преобразованные в пустую строку записи и сохраняя дубликаты и прочие значения. Это преобразование не нормализует и не проверяет предлагаемую политику полномочий роли.
func accessScopeKinds(values []controlplanev1.AccessScopeKind) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := accessScopeKind(value); item != "" {
			result = append(result, item)
		}
	}
	return result
}

// accessResourceKind converts unspecified resource kind to empty and strips ACCESS_RESOURCE_KIND_ for other enum strings. It keeps wire-to-domain representation separate from resolving the referenced resource or checking ownership.
// accessResourceKind преобразует unspecified вид ресурса в пустую строку, а у остальных enum strings удаляет ACCESS_RESOURCE_KIND_. Отделяет wire-to-domain представление от разрешения ссылки ресурса и проверки владения.
func accessResourceKind(value controlplanev1.AccessResourceKind) string {
	if value == controlplanev1.AccessResourceKind_ACCESS_RESOURCE_KIND_UNSPECIFIED {
		return ""
	}
	return enumSuffix(value, "ACCESS_RESOURCE_KIND_")
}

// castPermissionDefinition projects permission key, localized name/description keys, risk and owner-condition support, preserving ordered allowed scopes and resource kinds. Catalog metadata describes policy vocabulary rather than an effective access decision.
// castPermissionDefinition проецирует ключ полномочия, ключи локализованных имени/описания, риск и поддержку owner-condition, сохраняя порядок разрешённых областей и видов ресурсов. Метаданные каталога описывают словарь политики, а не решение о действующем доступе.
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

// castAccessSubject projects stored subject identity, activity and display name, copying its OIDC-group reference slice to avoid aliasing the domain value. Group references are presentation data and do not independently authenticate the returned subject.
// castAccessSubject проецирует сохранённые идентичность, активность и отображаемое имя субъекта, копируя список ссылок OIDC-групп без совместного владения доменным срезом. Ссылки групп являются данными представления и сами не аутентифицируют возвращённого субъекта.
func castAccessSubject(value entity.AccessSubject) *controlplanev1.AccessSubject {
	return &controlplanev1.AccessSubject{Ref: value.Ref, Kind: subjectKindEnum(value.Kind), DisplayName: value.DisplayName, Active: value.Active, OidcGroupRefs: append([]string(nil), value.OIDCGroupRefs...)}
}

// castOIDCGroup projects stored group state, membership/binding counts and last-seen/synchronization timestamps into the wire model. It performs no live OIDC query and does not interpret a count as authorization for the caller.
// castOIDCGroup проецирует сохранённое состояние группы, счётчики членства/привязок и время последнего наблюдения/синхронизации в wire-модель. Не делает live OIDC-запрос и не трактует счётчик как полномочие вызывающего.
func castOIDCGroup(value entity.OIDCGroup) *controlplanev1.OIDCGroup {
	return &controlplanev1.OIDCGroup{Ref: value.Ref, DisplayName: value.DisplayName, State: oidcGroupState(value.State), MemberCount: value.MemberCount, BindingCount: value.BindingCount, LastSeenAt: timestamppb.New(value.LastSeenAt), SyncedAt: timestamppb.New(value.SynchronizedAt)}
}

// castAccessRole combines role identity, aggregate version, kind/state, current immutable-version metadata, binding count and update timestamp. This projection exposes the owner snapshot without selecting or activating another role version.
// castAccessRole объединяет идентичность роли, версию агрегата, вид/состояние, метаданные текущей неизменяемой версии, счётчик привязок и время обновления. Проекция выдаёт снимок владельца без выбора или активации другой версии роли.
func castAccessRole(value entity.AccessRole) *controlplanev1.AccessRole {
	return &controlplanev1.AccessRole{Ref: value.Ref, Version: value.Version, Kind: roleKindEnum(value.Kind), State: roleStateEnum(value.State), CurrentVersion: castAccessRoleVersion(value.CurrentVersion), BindingCount: value.BindingCount, UpdatedAt: timestamppb.New(value.UpdatedAt)}
}

// castAccessRoleVersion copies permission keys and projects ordered allowed scopes, revision lineage, change comment and creator/time from a stored role version. It translates immutable metadata without changing the version or issuing its permissions.
// castAccessRoleVersion копирует ключи полномочий и проецирует упорядоченные виды областей, происхождение ревизии, комментарий изменения и автора/время сохранённой версии роли. Переводит неизменяемые метаданные без изменения версии или выдачи её полномочий.
func castAccessRoleVersion(value entity.AccessRoleVersion) *controlplanev1.AccessRoleVersion {
	result := &controlplanev1.AccessRoleVersion{Ref: value.Ref, RoleRef: value.RoleRef, Revision: value.Revision, Name: value.Name, Description: value.Description, PermissionKeys: append([]string(nil), value.PermissionKeys...), ChangeComment: value.ChangeComment, CreatedAt: timestamppb.New(value.CreatedAt), CreatedBy: castUser(value.CreatedBy)}
	for _, scope := range value.AllowedScopes {
		result.AllowedScopes = append(result.AllowedScopes, scopeKindEnum(scope))
	}
	return result
}

// castAccessScope preserves domain scope kind and exact project/resource references in the protobuf target shape. This is a representation adapter, not an existence lookup or proof that a caller may access the target.
// castAccessScope сохраняет вид доменной области и точные ссылки проекта/ресурса в protobuf-форме цели. Это adapter представления, а не поиск существования или доказательство доступа вызывающего к цели.
func castAccessScope(value entity.AccessScope) *controlplanev1.AccessScope {
	return &controlplanev1.AccessScope{Kind: scopeKindEnum(value.Kind), ProjectRef: value.ProjectRef, ResourceKind: resourceKindEnum(value.ResourceKind), ResourceRef: value.ResourceRef}
}

// castAccessBinding projects the complete stored binding with subject, pinned role version, target scope, optional condition times and lifecycle timestamps. Management-kind translation distinguishes direct and platform/project membership presentation without issuing new access.
// castAccessBinding проецирует полную сохранённую привязку с субъектом, закреплённой версией роли, целевой областью, необязательными временами условий и lifecycle timestamps. Преобразование management-kind различает прямое назначение и представление членства платформы/проекта без выдачи нового доступа.
func castAccessBinding(value entity.AccessBinding) *controlplanev1.AccessBinding {
	conditions := &controlplanev1.AccessConditions{RequireOwner: value.Conditions.RequireOwner}
	if value.Conditions.ValidFrom != nil {
		conditions.ValidFrom = timestamppb.New(*value.Conditions.ValidFrom)
	}
	if value.Conditions.ValidUntil != nil {
		conditions.ValidUntil = timestamppb.New(*value.Conditions.ValidUntil)
	}
	return &controlplanev1.AccessBinding{Ref: value.Ref, Version: value.Version, State: bindingStateEnum(value.State), Subject: castAccessSubject(value.Subject), RoleVersion: castAccessRoleVersion(value.RoleVersion), Scope: castAccessScope(value.Scope), Conditions: conditions, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), ManagementKind: bindingManagementKindEnum(value.PresentationKind)}
}

// castEffectiveDecision maps the owner Allowed boolean to the explicit allowed/denied wire enum and preserves the permission target plus ordered explanation provenance. Explanation references describe evaluated evidence; they do not become new executable bindings.
// castEffectiveDecision преобразует owner Allowed в явный wire enum allowed/denied и сохраняет цель полномочия и упорядоченное происхождение объяснения. Ссылки объяснения описывают оценённые основания, но не становятся новыми исполняемыми привязками.
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

// permissionRisk looks up the prefixed permission-risk wire enum; an unknown domain string maps to protobuf zero/unspecified through the generated map. It does not invent a severity or alter the permission policy.
// permissionRisk ищет wire enum риска полномочия с префиксом; неизвестная доменная строка через generated map даёт protobuf zero/unspecified. Не назначает выдуманную серьёзность и не меняет политику полномочия.
func permissionRisk(value string) controlplanev1.PermissionRisk {
	return controlplanev1.PermissionRisk(controlplanev1.PermissionRisk_value["PERMISSION_RISK_"+value])
}

// subjectKindEnum maps a domain subject kind through the generated ACCESS_SUBJECT_KIND_ table, yielding unspecified for an unknown key. This output conversion does not resolve or authenticate a subject.
// subjectKindEnum преобразует доменный вид субъекта через generated таблицу ACCESS_SUBJECT_KIND_, давая unspecified для неизвестного ключа. Это выходное преобразование не разрешает и не аутентифицирует субъекта.
func subjectKindEnum(value string) controlplanev1.AccessSubjectKind {
	return controlplanev1.AccessSubjectKind(controlplanev1.AccessSubjectKind_value["ACCESS_SUBJECT_KIND_"+value])
}

// scopeKindEnum maps a domain scope kind through the generated ACCESS_SCOPE_KIND_ table; missing keys retain the protobuf unspecified value. Resource references and authorization are handled separately from this enum projection.
// scopeKindEnum преобразует доменный вид области через generated таблицу ACCESS_SCOPE_KIND_; отсутствующие ключи сохраняют protobuf unspecified. Ссылки ресурсов и полномочия обрабатываются отдельно от этой enum-проекции.
func scopeKindEnum(value string) controlplanev1.AccessScopeKind {
	return controlplanev1.AccessScopeKind(controlplanev1.AccessScopeKind_value["ACCESS_SCOPE_KIND_"+value])
}

// resourceKindEnum maps domain access-resource kinds through the generated ACCESS_RESOURCE_KIND_ names, returning zero for unsupported strings. It does not widen a target scope or substitute a different resource kind.
// resourceKindEnum преобразует доменные виды ресурсов доступа через generated имена ACCESS_RESOURCE_KIND_, возвращая ноль для неподдерживаемых строк. Не расширяет целевую область и не подставляет иной вид ресурса.
func resourceKindEnum(value string) controlplanev1.AccessResourceKind {
	return controlplanev1.AccessResourceKind(controlplanev1.AccessResourceKind_value["ACCESS_RESOURCE_KIND_"+value])
}

// roleKindEnum projects the stored role category using the generated ACCESS_ROLE_KIND_ lookup, with unspecified as the missing-key result. It does not promote a custom role into a platform-owned role.
// roleKindEnum проецирует сохранённую категорию роли через generated поиск ACCESS_ROLE_KIND_ с unspecified для отсутствующего ключа. Не превращает пользовательскую роль в роль владельца платформы.
func roleKindEnum(value string) controlplanev1.AccessRoleKind {
	return controlplanev1.AccessRoleKind(controlplanev1.AccessRoleKind_value["ACCESS_ROLE_KIND_"+value])
}

// roleStateEnum translates stored role lifecycle state through ACCESS_ROLE_STATE_; unknown strings stay unspecified rather than being treated as active. No archive or activation transition is performed here.
// roleStateEnum переводит сохранённое lifecycle-состояние роли через ACCESS_ROLE_STATE_; неизвестные строки остаются unspecified вместо трактовки как active. Переход архивирования или активации здесь не выполняется.
func roleStateEnum(value string) controlplanev1.AccessRoleState {
	return controlplanev1.AccessRoleState(controlplanev1.AccessRoleState_value["ACCESS_ROLE_STATE_"+value])
}

// bindingStateEnum translates a stored binding state through ACCESS_BINDING_STATE_, yielding unspecified for missing keys. It represents the owner state and does not reactivate a revoked binding.
// bindingStateEnum переводит сохранённое состояние привязки через ACCESS_BINDING_STATE_, давая unspecified для отсутствующего ключа. Представляет состояние владельца и не активирует отозванную привязку.
func bindingStateEnum(value string) controlplanev1.AccessBindingState {
	return controlplanev1.AccessBindingState(controlplanev1.AccessBindingState_value["ACCESS_BINDING_STATE_"+value])
}

// bindingManagementKindEnum maps the internal NONE presentation to the public DIRECT enum; other values use the generated management-kind table with unspecified fallback. This translation describes binding management and grants no membership or edit permission.
// bindingManagementKindEnum сопоставляет внутреннее представление NONE публичному DIRECT enum; прочие значения используют generated management-kind таблицу с unspecified fallback. Перевод описывает управление привязкой и не выдаёт членство или право изменения.
func bindingManagementKindEnum(value string) controlplanev1.AccessBindingManagementKind {
	if value == "NONE" {
		return controlplanev1.AccessBindingManagementKind_ACCESS_BINDING_MANAGEMENT_KIND_DIRECT
	}
	return controlplanev1.AccessBindingManagementKind(controlplanev1.AccessBindingManagementKind_value["ACCESS_BINDING_MANAGEMENT_KIND_"+value])
}

// oidcGroupState projects the stored OIDC-group state through the generated OIDC_GROUP_STATE_ table, returning unspecified for unknown strings. It does not refresh identity-provider membership or change stored group synchronization state.
// oidcGroupState проецирует сохранённое состояние OIDC-группы через generated таблицу OIDC_GROUP_STATE_, возвращая unspecified для неизвестных строк. Не обновляет членство у провайдера идентичности и не меняет сохранённое состояние синхронизации группы.
func oidcGroupState(value string) controlplanev1.OIDCGroupState {
	return controlplanev1.OIDCGroupState(controlplanev1.OIDCGroupState_value["OIDC_GROUP_STATE_"+value])
}
