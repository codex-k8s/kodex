package httptransport

import (
	"net/http"
	"regexp"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

var systemGrantDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Server) GetSystemAssistantIntegrationGrantCandidates(w http.ResponseWriter, r *http.Request, p generated.GetSystemAssistantIntegrationGrantCandidatesParams) {
	response, err := s.control.Query.GetSystemAssistantIntegrationGrantCandidates(r.Context(), &cp.GetSystemAssistantIntegrationGrantCandidatesRequest{
		ConnectionRef: p.ConnectionRef, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := systemAssistantGrantCandidatePage(response, p.ConnectionRef, int(page(p.PageSize, p.PageToken).PageSize))
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", true)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func systemAssistantGrantCandidatePage(v *cp.GetSystemAssistantIntegrationGrantCandidatesResponse, connectionRef string, limit int) (map[string]any, bool) {
	if v == nil || v.ScopeKind != cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION ||
		!fileTargetRef(v.OrganizationRef) || !fileTargetRef(v.AssistantRef) || v.AssistantVersion < 1 || v.AssistantVersion > maximumSafeJSONInteger ||
		v.ConnectionRef != connectionRef || !fileTargetRef(v.ConnectionRef) || v.ConnectionVersion < 1 || v.ConnectionVersion > maximumSafeJSONInteger ||
		v.DefinitionVersion == "" || !systemGrantDigestPattern.MatchString(v.DefinitionDigest) || len(v.Items) > limit ||
		v.Total < int64(len(v.Items)) || v.Total > maximumSafeJSONInteger || v.Page == nil {
		return nil, false
	}
	items := make([]map[string]any, 0, len(v.Items))
	seen := make(map[string]bool, len(v.Items))
	for _, item := range v.Items {
		if item == nil || item.Capability == nil || item.Capability.Key == "" || seen[item.Capability.Key] ||
			item.CurrentGrantVersion < 0 || item.CurrentGrantVersion > maximumSafeJSONInteger ||
			(item.CurrentGrantRef == "") != (item.CurrentGrantVersion == 0) ||
			item.CurrentGrantRef != "" && !fileTargetRef(item.CurrentGrantRef) || len(item.CurrentApprovalScopePaths) > 16 {
			return nil, false
		}
		seen[item.Capability.Key] = true
		reason, valid := candidateReason(item.Reason, item.Grantable, false, "GRANT")
		if !valid || len(item.Capability.AllowedApprovalPolicies) == 0 {
			return nil, false
		}
		capability, err := messageMap(item.Capability)
		if err != nil {
			return nil, false
		}
		policy := ""
		if item.CurrentGrantRef != "" {
			if item.CurrentApprovalPolicy < cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE || item.CurrentApprovalPolicy > cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_SCOPED {
				return nil, false
			}
			policy = strings.TrimPrefix(item.CurrentApprovalPolicy.String(), "INTEGRATION_APPROVAL_POLICY_")
		}
		paths := append([]string{}, item.CurrentApprovalScopePaths...)
		entry := map[string]any{"capability": capability, "grantable": item.Grantable, "reason": reason,
			"currentGrantVersion": item.CurrentGrantVersion, "currentGrantEnabled": item.CurrentGrantEnabled, "currentApprovalScopePaths": paths}
		if item.CurrentGrantRef != "" {
			entry["currentGrantRef"], entry["currentApprovalPolicy"] = item.CurrentGrantRef, policy
		}
		items = append(items, entry)
	}
	result := map[string]any{"scopeKind": "ORGANIZATION", "organizationRef": v.OrganizationRef, "assistantRef": v.AssistantRef,
		"assistantVersion": v.AssistantVersion, "connectionRef": v.ConnectionRef, "connectionVersion": v.ConnectionVersion,
		"definitionVersion": v.DefinitionVersion, "definitionDigest": v.DefinitionDigest, "items": items, "total": v.Total}
	if v.Page.NextPageToken != "" {
		result["nextPageToken"] = v.Page.NextPageToken
	}
	return result, true
}

func (s *Server) ChangeSystemAssistantIntegrationGrant(w http.ResponseWriter, r *http.Request, p generated.ChangeSystemAssistantIntegrationGrantParams) {
	body, ok := decodeJSON[generated.SystemAssistantIntegrationGrantInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var paths []string
	if body.ApprovalScopePaths != nil {
		paths = append(paths, (*body.ApprovalScopePaths)...)
	}
	response, err := s.control.Command.ChangeSystemAssistantIntegrationGrant(r.Context(), &cp.ChangeSystemAssistantIntegrationGrantRequest{
		Mutation: mutation, ConnectionRef: body.ConnectionRef, CapabilityKey: body.CapabilityKey, Enabled: body.Enabled,
		ApprovalPolicy: cp.IntegrationApprovalPolicy(cp.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+string(body.ApprovalPolicy)]), ApprovalScopePaths: paths,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}
