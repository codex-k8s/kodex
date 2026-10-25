package httptransport

import (
	"net/http"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// GetRuntimeEnvironmentImpact reads the exact environment/target revision impact, validates
// totals/digest and unique consumers sharing one owner-scope tuple, then emits the
// environment-version ETag. It does not rebind or grant access to listed consumers.
// GetRuntimeEnvironmentImpact читает impact точной environment/target revision, проверяет
// total/digest и уникальных consumers с одной owner-scope тройкой, затем выдаёт ETag версии среды.
// Перепривязка и выдача доступа consumers не выполняются.
func (server *Server) GetRuntimeEnvironmentImpact(w http.ResponseWriter, r *http.Request, environmentRef generated.RuntimeEnvironmentRef, versionRef generated.RuntimeEnvironmentVersionRef, p generated.GetRuntimeEnvironmentImpactParams) {
	if !opaqueHTTPReference.MatchString(environmentRef) || !opaqueHTTPReference.MatchString(versionRef) || !validFilteredHTTPPage(p.PageSize, p.PageToken, p.Query) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetRuntimeEnvironmentImpact(r.Context(), &controlplanev1.GetRuntimeEnvironmentImpactRequest{EnvironmentRef: environmentRef, VersionRef: versionRef, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || response.GetEnvironmentRef() != environmentRef || response.GetTargetVersionRef() != versionRef ||
		response.GetEnvironmentVersion() < 1 || response.GetEnvironmentVersion() > maximumSafeJSONInteger || !validManagedDigest(response.GetTargetDigest()) ||
		response.GetTotal() < int64(len(response.GetConsumers())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetConsumers()) > 100 || len(response.GetPage().GetNextPageToken()) > 512 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeEnvironmentImpact{EnvironmentRef: environmentRef, EnvironmentVersion: response.GetEnvironmentVersion(), TargetVersionRef: versionRef,
		TargetDigest: response.GetTargetDigest(), Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken(), Consumers: []generated.RuntimeEnvironmentConsumer{}}
	seen := make(map[string]bool)
	owner := ""
	for _, consumer := range response.GetConsumers() {
		item := environmentConsumerView(consumer)
		key := impactConsumerOwnerKey(item.ScopeKind, item.OrganizationRef, item.ProjectRef)
		if !validEnvironmentConsumer(item) || seen[item.AgentRef] || owner != "" && key != owner {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.AgentRef] = true
		owner = key
		result.Consumers = append(result.Consumers, item)
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.EnvironmentVersion, 10)+"\"")
	writeJSON(w, http.StatusOK, result)
}

// RebindRuntimeEnvironment validates one bounded unique consumer selection with a common scope
// tuple under versioned mutation context, then delegates rebind. Returned bindings must cover
// exactly the selected agents, retain binding identities and advance versions toward the requested
// target revision.
// RebindRuntimeEnvironment проверяет ограниченный уникальный выбор consumers с общей тройкой
// области и versioned mutation context, затем поручает rebind владельцу. Ответ покрывает ровно
// выбранных агентов, сохраняет identity привязок и повышает версии к запрошенной target revision.
func (server *Server) RebindRuntimeEnvironment(w http.ResponseWriter, r *http.Request, environmentRef generated.RuntimeEnvironmentRef, versionRef generated.RuntimeEnvironmentVersionRef, p generated.RebindRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentRebindInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(environmentRef) || !opaqueHTTPReference.MatchString(versionRef) || len(body.Consumers) < 1 || len(body.Consumers) > 100 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	input := &controlplanev1.RebindRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: environmentRef, VersionRef: versionRef}
	selected := make(map[string]generated.RuntimeEnvironmentConsumer, len(body.Consumers))
	owner := ""
	for _, item := range body.Consumers {
		_, duplicate := selected[item.AgentRef]
		key := impactConsumerOwnerKey(item.ScopeKind, item.OrganizationRef, item.ProjectRef)
		if !validEnvironmentConsumer(item) || duplicate || owner != "" && key != owner {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		selected[item.AgentRef] = item
		owner = key
		input.Consumers = append(input.Consumers, &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: item.AgentRef, AgentVersion: item.AgentVersion, BindingRef: item.BindingRef,
			BindingVersion: item.BindingVersion, VersionRef: item.VersionRef, ProjectRef: item.ProjectRef,
			ScopeKind: consumerScopeProto(item.ScopeKind), OrganizationRef: item.OrganizationRef})
	}
	response, err := server.control.Command.RebindRuntimeEnvironment(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetBindings()) != len(selected) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeEnvironmentRebindResult{Bindings: []generated.AgentRuntimeEnvironmentBinding{}}
	for _, binding := range response.GetBindings() {
		previous, exists := selected[binding.GetAgentRef()]
		if !exists || binding.GetRef() != previous.BindingRef || binding.GetEnvironmentRef() != environmentRef || binding.GetVersionRef() != versionRef ||
			binding.GetVersion() <= previous.BindingVersion || binding.GetVersion() > maximumSafeJSONInteger || !validManagedDigest(binding.GetDigest()) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		delete(selected, binding.GetAgentRef())
		result.Bindings = append(result.Bindings, generated.AgentRuntimeEnvironmentBinding{Ref: binding.GetRef(), Version: binding.GetVersion(), AgentRef: binding.GetAgentRef(),
			EnvironmentRef: environmentRef, VersionRef: &versionRef, Digest: binding.GetDigest()})
	}
	writeJSON(w, http.StatusOK, result)
}

// validEnvironmentConsumer checks the public scope tuple, agent/binding/version refs and safe
// positive agent/binding versions. Shape compatibility is not proof of tenant ownership or
// permission to rebind.
// validEnvironmentConsumer проверяет публичную тройку области, agent/binding/version refs и
// безопасные положительные версии агента и привязки. Совместимость формы не доказывает tenant
// владение и право rebind.
func validEnvironmentConsumer(item generated.RuntimeEnvironmentConsumer) bool {
	return validRuntimeResourceScope(item.ScopeKind, item.OrganizationRef, item.ProjectRef) && opaqueHTTPReference.MatchString(item.AgentRef) && opaqueHTTPReference.MatchString(item.BindingRef) && opaqueHTTPReference.MatchString(item.VersionRef) &&
		item.AgentVersion >= 1 && item.AgentVersion <= maximumSafeJSONInteger && item.BindingVersion >= 1 && item.BindingVersion <= maximumSafeJSONInteger
}

// environmentConsumerView copies consumer identity, scope and version metadata through nil-safe
// protobuf getters. Callers must validate the resulting public projection before using it.
// environmentConsumerView копирует identity, область и metadata версий consumer через nil-safe
// protobuf getters. Caller обязан проверить полученную публичную проекцию до использования.
func environmentConsumerView(item *controlplanev1.RuntimeEnvironmentConsumer) generated.RuntimeEnvironmentConsumer {
	return generated.RuntimeEnvironmentConsumer{AgentRef: item.GetAgentRef(), AgentVersion: item.GetAgentVersion(), BindingRef: item.GetBindingRef(),
		BindingVersion: item.GetBindingVersion(), VersionRef: item.GetVersionRef(), ProjectRef: item.GetProjectRef(),
		ScopeKind: runtimeResourceScopeKind(item.GetScopeKind().String()), OrganizationRef: item.GetOrganizationRef()}
}

// consumerScopeProto maps a public scope discriminator through the closed protobuf value table;
// unknown input becomes unspecified and requires rejection by the caller validator.
// consumerScopeProto преобразует публичный вид области через закрытую protobuf таблицу значений;
// неизвестный вход становится unspecified и требует отказа валидатором caller.
func consumerScopeProto(kind generated.RuntimeResourceScopeKind) controlplanev1.RuntimeResourceScopeKind {
	return controlplanev1.RuntimeResourceScopeKind(controlplanev1.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+string(kind)])
}

// impactConsumerOwnerKey joins the already validated scope kind, organization and project into a
// comparison key for page/selection consistency. It does not resolve ownership or authenticate
// those coordinates.
// impactConsumerOwnerKey соединяет уже проверенные вид области, организацию и проект в ключ
// сравнения согласованности страницы или выборки. Владение не разрешается и координаты не
// аутентифицируются.
func impactConsumerOwnerKey(kind generated.RuntimeResourceScopeKind, organizationRef, projectRef string) string {
	return string(kind) + "/" + organizationRef + "/" + projectRef
}
