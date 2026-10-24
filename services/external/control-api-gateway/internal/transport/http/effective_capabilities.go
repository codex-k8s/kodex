package httptransport

import (
	"net/http"
	"regexp"
	"strings"
	"unicode"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// effectiveCapabilityKey defines the lowercase capability-key grammar for public catalog items,
// with the separate length bound enforced by the page validator.
// effectiveCapabilityKey задаёт грамматику ключа capability нижнего регистра для публичных
// элементов каталога; длину отдельно ограничивает валидатор страницы.
var effectiveCapabilityKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]+$`)

// GetAgentEffectiveCapabilities validates the agent and paired workflow/step selectors, calls CP
// under the request context and rejects mismatched, oversized or repeating-cursor results before
// writing the public page. Requested selectors identify a query and do not establish access.
// GetAgentEffectiveCapabilities проверяет агента и парные workflow/step selectors, вызывает CP в
// контексте запроса и отклоняет несовпадающий, длинный результат либо повторный cursor до выдачи
// публичной страницы. Selectors задают запрос и не устанавливают доступ.
func (server *Server) GetAgentEffectiveCapabilities(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, p generated.GetAgentEffectiveCapabilitiesParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, nil)
	if !ok {
		return
	}
	workflowRef, stepKey := stringValue(p.WorkflowRef), stringValue(p.StepKey)
	if !effectiveCapabilityRef(agentRef) || !validSearchText(stringValue(p.Query), 0, 200) ||
		(p.WorkflowRef == nil) != (p.StepKey == nil) || p.WorkflowRef != nil && (!effectiveCapabilityRef(workflowRef) || !validSearchText(stepKey, 1, 96) || strings.IndexFunc(stepKey, unicode.IsControl) >= 0) ||
		p.PageToken != nil && !boundedModelText(*p.PageToken, 2048) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetAgentEffectiveCapabilities(r.Context(), &cp.GetAgentEffectiveCapabilitiesRequest{AgentRef: agentRef, WorkflowRef: workflowRef, StepKey: stepKey, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, valid := effectiveCapabilityPage(response, agentRef, workflowRef, stepKey)
	if !valid || result.NextPageToken != nil && *result.NextPageToken == stringValue(p.PageToken) || p.PageSize != nil && len(result.Items) > *p.PageSize {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// effectiveCapabilityRef requires the common opaque reference grammar and a maximum of 96 bytes,
// without interpreting prefixes or establishing resource ownership.
// effectiveCapabilityRef требует общей грамматики opaque ref и не более 96 байт без интерпретации
// префиксов и установления владения ресурсом.
func effectiveCapabilityRef(ref string) bool {
	return len(ref) <= 96 && opaqueHTTPReference.MatchString(ref)
}

// effectiveCapabilityPage projects only a page matching the requested agent/workflow/step and
// valid immutable pins, counts and timestamps. It enforces strict item ordering, effective/reason
// consistency and integration-only connection/grant pins; false makes any accumulated partial page
// unusable.
// effectiveCapabilityPage проецирует только страницу с запрошенными agent/workflow/step и
// корректными immutable pins, счётчиками и временем. Проверяются строгий порядок элементов,
// согласованность effective/reason и connection/grant pins только у интеграций; при false
// накопленная частичная страница непригодна.
func effectiveCapabilityPage(response *cp.GetAgentEffectiveCapabilitiesResponse, agentRef, workflowRef, stepKey string) (generated.AgentEffectiveCapabilityPage, bool) {
	result := generated.AgentEffectiveCapabilityPage{Items: []generated.AgentEffectiveCapability{}}
	if response == nil || response.GetAgentRef() != agentRef || !validManagedVersion(response.GetAgentVersion()) ||
		!effectiveCapabilityRef(response.GetRuntimeConfigurationRef()) || !validManagedVersion(response.GetRuntimeConfigurationVersion()) || !effectiveCapabilityRef(response.GetEnvironmentVersionRef()) ||
		!validManagedDigest(response.GetDigest()) || response.GetEvaluatedAt() == nil || response.GetEvaluatedAt().CheckValid() != nil ||
		response.GetProjectRef() != "" && !effectiveCapabilityRef(response.GetProjectRef()) ||
		response.GetWorkflowRef() != workflowRef || response.GetStepKey() != stepKey || (workflowRef != "") != (response.GetWorkflowVersionRef() != "") ||
		workflowRef != "" && !effectiveCapabilityRef(response.GetWorkflowVersionRef()) ||
		response.GetTotal() < int64(len(response.GetCapabilities())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetCapabilities()) > 100 {
		return result, false
	}
	next := response.GetPage().GetNextPageToken()
	if next != "" && (!boundedModelText(next, 2048) || len(response.GetCapabilities()) == 0) {
		return result, false
	}
	result.AgentRef, result.AgentVersion = agentRef, response.GetAgentVersion()
	result.RuntimeConfigurationRef, result.RuntimeConfigurationVersion = response.GetRuntimeConfigurationRef(), response.GetRuntimeConfigurationVersion()
	result.EnvironmentVersionRef, result.Digest, result.EvaluatedAt = response.GetEnvironmentVersionRef(), response.GetDigest(), response.GetEvaluatedAt().AsTime()
	result.RuntimeReady, result.Total = response.GetRuntimeReady(), response.GetTotal()
	result.ProjectRef, result.WorkflowRef, result.WorkflowVersionRef, result.StepKey, result.NextPageToken = optionalManagedString(response.GetProjectRef()), optionalManagedString(workflowRef), optionalManagedString(response.GetWorkflowVersionRef()), optionalManagedString(stepKey), optionalManagedString(next)
	previous := ""
	for _, item := range response.GetCapabilities() {
		if item == nil || !effectiveCapabilityKey.MatchString(item.GetKey()) || len(item.GetKey()) > 120 || !validSearchText(item.GetName(), 1, 240) || !validSearchText(item.GetDescription(), 0, 4000) ||
			!generated.AgentEffectiveCapabilitySource(item.GetSource()).Valid() || !generated.AgentEffectiveCapabilityReason(item.GetReason()).Valid() ||
			item.GetEffective() != (item.GetReason() == "AVAILABLE") || item.GetEffective() && (!item.GetRequested() || !response.GetRuntimeReady() || workflowRef != "" && !item.GetRequired()) {
			return result, false
		}
		key := item.GetKey() + ":" + item.GetConnectionRef() + ":" + item.GetGrantRef()
		if key <= previous {
			return result, false
		}
		previous = key
		value := generated.AgentEffectiveCapability{Key: item.GetKey(), Name: item.GetName(), Description: item.GetDescription(), Source: generated.AgentEffectiveCapabilitySource(item.GetSource()), Reason: generated.AgentEffectiveCapabilityReason(item.GetReason()), Requested: item.GetRequested(), Required: item.GetRequired(), Effective: item.GetEffective(), Grantable: item.GetGrantable()}
		if item.GetSource() == "INTEGRATION" {
			if !effectiveCapabilityRef(item.GetConnectionRef()) || !effectiveCapabilityRef(item.GetGrantRef()) || !validManagedVersion(item.GetConnectionVersion()) || !validManagedVersion(item.GetGrantVersion()) || !validManagedDigest(item.GetDefinitionDigest()) {
				return result, false
			}
			connectionVersion, grantVersion := item.GetConnectionVersion(), item.GetGrantVersion()
			value.ConnectionRef, value.GrantRef, value.DefinitionDigest = optionalManagedString(item.GetConnectionRef()), optionalManagedString(item.GetGrantRef()), optionalManagedString(item.GetDefinitionDigest())
			value.ConnectionVersion, value.GrantVersion = &connectionVersion, &grantVersion
		} else if item.GetConnectionRef() != "" || item.GetGrantRef() != "" || item.GetConnectionVersion() != 0 || item.GetGrantVersion() != 0 || item.GetDefinitionDigest() != "" {
			return result, false
		}
		result.Items = append(result.Items, value)
	}
	return result, true
}
