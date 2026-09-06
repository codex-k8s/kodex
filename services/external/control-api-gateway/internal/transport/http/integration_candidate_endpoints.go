package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func candidateString[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}
func candidateRecipient(v string) cp.IntegrationGrantRecipientKind {
	return cp.IntegrationGrantRecipientKind(cp.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+v])
}

func (s *Server) ListIntegrationGrantConnectionCandidates(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationGrantConnectionCandidatesParams) {
	c := &cp.IntegrationGrantCandidateContext{ProjectRef: stringValue(p.ProjectRef), RecipientKind: candidateRecipient(candidateString(p.RecipientKind)), RecipientRef: stringValue(p.RecipientRef), CapabilityKey: stringValue(p.CapabilityKey), WorkflowRef: stringValue(p.WorkflowRef), StepKey: stringValue(p.StepKey)}
	purpose := string(p.Purpose)
	if !candidateRequestValid(r, 0, purpose, c, p.Query, p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	v, err := s.control.Query.ListIntegrationGrantConnectionCandidates(r.Context(), &cp.ListIntegrationGrantConnectionCandidatesRequest{Purpose: cp.IntegrationCandidatePurpose(cp.IntegrationCandidatePurpose_value["INTEGRATION_CANDIDATE_PURPOSE_"+purpose]), Context: c, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeCandidatePage(w, v, c, purpose, 0, int(page(p.PageSize, p.PageToken).PageSize))
}
func (s *Server) ListIntegrationGrantProjectCandidates(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationGrantProjectCandidatesParams) {
	c := &cp.IntegrationGrantCandidateContext{ConnectionRef: p.ConnectionRef}
	if !candidateRequestValid(r, 1, "GRANT", c, p.Query, p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	v, err := s.control.Query.ListIntegrationGrantProjectCandidates(r.Context(), &cp.ListIntegrationGrantProjectCandidatesRequest{Context: c, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeCandidatePage(w, v, c, "GRANT", 1, int(page(p.PageSize, p.PageToken).PageSize))
}
func (s *Server) ListIntegrationGrantRecipientCandidates(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationGrantRecipientCandidatesParams) {
	c := &cp.IntegrationGrantCandidateContext{ConnectionRef: p.ConnectionRef, ProjectRef: p.ProjectRef, RecipientKind: candidateRecipient(string(p.RecipientKind))}
	if !candidateRequestValid(r, 2, "GRANT", c, p.Query, p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	v, err := s.control.Query.ListIntegrationGrantRecipientCandidates(r.Context(), &cp.ListIntegrationGrantRecipientCandidatesRequest{Context: c, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeCandidatePage(w, v, c, "GRANT", 2, int(page(p.PageSize, p.PageToken).PageSize))
}
func (s *Server) ListIntegrationGrantCapabilityCandidates(w http.ResponseWriter, r *http.Request, p generated.ListIntegrationGrantCapabilityCandidatesParams) {
	c := &cp.IntegrationGrantCandidateContext{ConnectionRef: p.ConnectionRef, ProjectRef: p.ProjectRef, RecipientKind: candidateRecipient(string(p.RecipientKind)), RecipientRef: p.RecipientRef}
	if !candidateRequestValid(r, 3, "GRANT", c, p.Query, p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	v, err := s.control.Query.ListIntegrationGrantCapabilityCandidates(r.Context(), &cp.ListIntegrationGrantCapabilityCandidatesRequest{Context: c, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeCandidatePage(w, v, c, "GRANT", 3, int(page(p.PageSize, p.PageToken).PageSize))
}

// Каждый шаг допускает только уже выбранный префикс. Неизвестные query-поля не теряются молча.
func candidateRequestValid(r *http.Request, stage int, purpose string, c *cp.IntegrationGrantCandidateContext, q *string, size *int, token *string) bool {
	allowed := map[string]bool{"query": true, "pageSize": true, "pageToken": true}
	fields := []string{}
	switch stage {
	case 0:
		fields = []string{"purpose", "projectRef", "recipientKind", "recipientRef", "capabilityKey", "workflowRef", "stepKey"}
	case 1:
		fields = []string{"connectionRef"}
	case 2:
		fields = []string{"connectionRef", "projectRef", "recipientKind"}
	case 3:
		fields = []string{"connectionRef", "projectRef", "recipientKind", "recipientRef"}
	}
	for _, f := range fields {
		allowed[f] = true
	}
	for k, vs := range r.URL.Query() {
		if !allowed[k] || len(vs) != 1 || (k != "query" && k != "pageToken" && vs[0] == "") {
			return false
		}
	}
	if !validSearchText(stringValue(q), 0, 200) || !fileTargetPage(size, token) {
		return false
	}
	for _, ref := range []string{c.ConnectionRef, c.ProjectRef, c.RecipientRef, c.WorkflowRef} {
		if ref != "" && !fileTargetRef(ref) {
			return false
		}
	}
	if stage == 0 {
		if purpose == "GRANT" {
			for _, key := range []string{"projectRef", "recipientKind", "recipientRef", "capabilityKey", "workflowRef", "stepKey"} {
				if r.URL.Query().Has(key) {
					return false
				}
			}
			return proto.Equal(c, &cp.IntegrationGrantCandidateContext{})
		}
		return purpose == "USE" && c.ProjectRef != "" && c.RecipientKind == cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_AGENT && c.RecipientRef != "" && validSearchText(c.CapabilityKey, 1, 120) && ((c.WorkflowRef == "" && c.StepKey == "") || (c.WorkflowRef != "" && validSearchText(c.StepKey, 1, 120)))
	}
	if purpose != "GRANT" || c.ConnectionRef == "" {
		return false
	}
	if stage >= 2 && c.ProjectRef == "" {
		return false
	}
	if stage >= 2 && c.RecipientKind != cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_AGENT && c.RecipientKind != cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_WORKFLOW {
		return false
	}
	return stage < 3 || c.RecipientRef != ""
}

type candidatePage interface {
	proto.Message
	GetPage() *cp.PageInfo
	GetTotal() int64
	GetContextDigest() string
	GetContext() *cp.IntegrationGrantCandidateContext
	GetPins() *cp.IntegrationGrantCandidatePins
}

func candidatePins(p *cp.IntegrationGrantCandidatePins, c *cp.IntegrationGrantCandidateContext) (map[string]any, bool) {
	if p == nil || !validManagedDigest(p.ContextDigest) {
		return nil, false
	}
	v := map[string]any{"contextDigest": p.ContextDigest}
	for _, x := range []struct {
		k        string
		n        int64
		required bool
	}{{"connectionVersion", p.ConnectionVersion, c.ConnectionRef != ""}, {"projectVersion", p.ProjectVersion, c.ProjectRef != ""}, {"recipientVersion", p.RecipientVersion, c.RecipientRef != ""}} {
		if x.required {
			if !validManagedVersion(x.n) {
				return nil, false
			}
			v[x.k] = x.n
		} else if x.n != 0 {
			return nil, false
		}
	}
	if c.ConnectionRef != "" {
		if !validSearchText(p.DefinitionVersion, 1, 128) || !validManagedDigest(p.DefinitionDigest) {
			return nil, false
		}
		v["definitionVersion"] = p.DefinitionVersion
		v["definitionDigest"] = p.DefinitionDigest
	} else if p.DefinitionVersion != "" || p.DefinitionDigest != "" {
		return nil, false
	}
	if c.WorkflowRef != "" {
		if !fileTargetRef(p.WorkflowRevisionRef) {
			return nil, false
		}
		v["workflowRevisionRef"] = p.WorkflowRevisionRef
	} else if p.WorkflowRevisionRef != "" {
		return nil, false
	}
	return v, true
}
func candidateContext(c *cp.IntegrationGrantCandidateContext) map[string]any {
	v := map[string]any{}
	for k, s := range map[string]string{"connectionRef": c.ConnectionRef, "projectRef": c.ProjectRef, "recipientRef": c.RecipientRef, "capabilityKey": c.CapabilityKey, "workflowRef": c.WorkflowRef, "stepKey": c.StepKey} {
		if s != "" {
			v[k] = s
		}
	}
	if c.RecipientKind != 0 {
		v["recipientKind"] = strings.TrimPrefix(c.RecipientKind.String(), "INTEGRATION_GRANT_RECIPIENT_KIND_")
	}
	return v
}
func candidateReason(reason cp.IntegrationCandidateReason, grantable, usable bool, purpose string) (string, bool) {
	if reason < cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY || reason > cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_WORKFLOW_EXCLUDED {
		return "", false
	}
	ready := reason == cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY
	if purpose == "USE" {
		if !ready || !usable || grantable {
			return "", false
		}
	} else if usable || grantable != ready {
		return "", false
	}
	return strings.TrimPrefix(reason.String(), "INTEGRATION_CANDIDATE_REASON_"), true
}

func writeCandidatePage(w http.ResponseWriter, v candidatePage, c *cp.IntegrationGrantCandidateContext, purpose string, stage, limit int) {
	result, ok := candidatePageView(v, c, purpose, stage, limit)
	if !ok {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, 200, result)
}
func candidatePageView(v candidatePage, c *cp.IntegrationGrantCandidateContext, purpose string, stage, limit int) (map[string]any, bool) {
	if v == nil || !v.ProtoReflect().IsValid() || v.GetContext() == nil || !proto.Equal(v.GetContext(), c) || !validManagedDigest(v.GetContextDigest()) || v.GetTotal() < 0 || v.GetTotal() > maximumSafeJSONInteger {
		return nil, false
	}
	pins, ok := candidatePins(v.GetPins(), c)
	if !ok || v.GetPins().ContextDigest != v.GetContextDigest() {
		return nil, false
	}
	next := v.GetPage().GetNextPageToken()
	if !fileTargetPage(nil, &next) {
		return nil, false
	}
	items := []any{}
	seen := map[string]bool{}
	add := func(key string, item map[string]any, itemContext *cp.IntegrationGrantCandidateContext, p *cp.IntegrationGrantCandidatePins, reason cp.IntegrationCandidateReason, grantable, usable bool) bool {
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
		pin, valid := candidatePins(p, itemContext)
		if !valid {
			return false
		}
		base := v.GetPins()
		if base.ConnectionVersion != 0 && (p.ConnectionVersion != base.ConnectionVersion || p.DefinitionVersion != base.DefinitionVersion || p.DefinitionDigest != base.DefinitionDigest) || base.ProjectVersion != 0 && p.ProjectVersion != base.ProjectVersion || base.RecipientVersion != 0 && p.RecipientVersion != base.RecipientVersion || base.WorkflowRevisionRef != "" && p.WorkflowRevisionRef != base.WorkflowRevisionRef {
			return false
		}
		why, valid := candidateReason(reason, grantable, usable, purpose)
		if !valid {
			return false
		}
		item["pins"], item["reason"], item["grantable"] = pin, why, grantable
		items = append(items, item)
		return true
	}
	switch x := v.(type) {
	case *cp.ListIntegrationGrantConnectionCandidatesResponse:
		if stage != 0 {
			return nil, false
		}
		for _, i := range x.Items {
			if i == nil || !fileTargetRef(i.ConnectionRef) || !validSearchText(i.Name, 1, 500) || !validSearchText(i.DefinitionKey, 1, 500) || !validSearchText(i.ProviderName, 0, 500) || (i.CredentialKind != "" && i.CredentialKind != "TOKEN" && i.CredentialKind != "PASSWORD") || (i.ProjectRef != "" && !fileTargetRef(i.ProjectRef)) || len(i.ResourceScope) > 8 {
				return nil, false
			}
			if purpose == "GRANT" && len(i.ResourceScope) != 0 {
				return nil, false
			}
			for k, value := range i.ResourceScope {
				if !validSearchText(k, 1, 120) || !validSearchText(value, 0, 349528) {
					return nil, false
				}
			}
			item := map[string]any{"connectionRef": i.ConnectionRef, "name": i.Name, "definitionKey": i.DefinitionKey, "providerName": i.ProviderName, "resourceScope": i.ResourceScope, "usable": i.Usable}
			if i.ResourceScope == nil {
				item["resourceScope"] = map[string]string{}
			}
			if i.CredentialKind != "" {
				item["credentialKind"] = i.CredentialKind
			}
			if i.ProjectRef != "" {
				item["projectRef"] = i.ProjectRef
			}
			ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
			ic.ConnectionRef = i.ConnectionRef
			if !add(i.ConnectionRef, item, ic, i.Pins, i.Reason, i.Grantable, i.Usable) {
				return nil, false
			}
		}
	case *cp.ListIntegrationGrantProjectCandidatesResponse:
		if stage != 1 {
			return nil, false
		}
		for _, i := range x.Items {
			if i == nil || !fileTargetRef(i.ProjectRef) || !validSearchText(i.Name, 1, 500) {
				return nil, false
			}
			ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
			ic.ProjectRef = i.ProjectRef
			if !add(i.ProjectRef, map[string]any{"projectRef": i.ProjectRef, "name": i.Name}, ic, i.Pins, i.Reason, i.Grantable, false) {
				return nil, false
			}
		}
	case *cp.ListIntegrationGrantRecipientCandidatesResponse:
		if stage != 2 {
			return nil, false
		}
		for _, i := range x.Items {
			if i == nil || !fileTargetRef(i.RecipientRef) || i.ProjectRef != c.ProjectRef || !validSearchText(i.Name, 1, 500) || i.RecipientKind != c.RecipientKind || (i.RecipientKind != cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_AGENT && i.RecipientKind != cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_WORKFLOW) {
				return nil, false
			}
			ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
			ic.RecipientRef = i.RecipientRef
			ic.RecipientKind = i.RecipientKind
			if !add(i.RecipientKind.String()+i.RecipientRef, map[string]any{"recipientRef": i.RecipientRef, "name": i.Name, "recipientKind": strings.TrimPrefix(i.RecipientKind.String(), "INTEGRATION_GRANT_RECIPIENT_KIND_"), "projectRef": i.ProjectRef}, ic, i.Pins, i.Reason, i.Grantable, false) {
				return nil, false
			}
		}
	case *cp.ListIntegrationGrantCapabilityCandidatesResponse:
		if stage != 3 {
			return nil, false
		}
		for _, i := range x.Items {
			if i == nil || i.Capability == nil || !validSearchText(i.Capability.Key, 1, 100) || (i.CurrentGrantRef == "" && i.CurrentGrantVersion != 0) || (i.CurrentGrantRef != "" && (!fileTargetRef(i.CurrentGrantRef) || !validManagedVersion(i.CurrentGrantVersion))) {
				return nil, false
			}
			capability, valid := candidateCapabilityView(i.Capability)
			if !valid {
				return nil, false
			}
			item := map[string]any{"capability": capability}
			if i.CurrentGrantRef != "" {
				item["currentGrantRef"] = i.CurrentGrantRef
				item["currentGrantVersion"] = i.CurrentGrantVersion
			}
			ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
			ic.CapabilityKey = i.Capability.Key
			if !add(i.Capability.Key, item, ic, i.Pins, i.Reason, i.Grantable, false) {
				return nil, false
			}
		}
	default:
		return nil, false
	}
	if len(items) > limit || int64(len(items)) > v.GetTotal() {
		return nil, false
	}
	result := map[string]any{"items": items, "total": v.GetTotal(), "contextDigest": v.GetContextDigest(), "context": candidateContext(c), "pins": pins}
	if next != "" {
		result["nextPageToken"] = next
	}
	return result, true
}

func candidateCapabilityView(c *cp.IntegrationCapability) (map[string]any, bool) {
	if c == nil || !validSearchText(c.Key, 1, 100) || !validSearchText(c.Name, 1, 160) || !validSearchText(c.Description, 0, 500) || !validSearchText(c.Operation, 1, 120) || c.ApprovalPolicy == 0 || c.ResourceKind == 0 || len(c.InputFields) > 16 {
		return nil, false
	}
	v, err := messageMap(c)
	if err != nil || v["risk"] == nil || v["approvalPolicy"] == nil || v["resourceKind"] == nil {
		return nil, false
	}
	v["description"] = c.Description
	if len(c.InputFields) == 0 {
		v["inputFields"] = []any{}
	}
	fields, _ := v["inputFields"].([]any)
	for index, f := range c.InputFields {
		if f == nil || !validSearchText(f.Key, 1, 64) || !validSearchText(f.Label, 0, 160) || !validSearchText(f.Help, 0, 500) || !validSearchText(f.Placeholder, 0, 300) || !validSearchText(f.Format, 0, 80) || len(f.AllowedValues) > 100 || f.MaximumLength < 0 || f.MaximumLength > 1048576 || f.Minimum < -maximumSafeJSONInteger || f.Minimum > maximumSafeJSONInteger || f.Maximum < -maximumSafeJSONInteger || f.Maximum > maximumSafeJSONInteger {
			return nil, false
		}
		switch f.ValueType {
		case "TEXT", "URL", "STRING_LIST", "INTEGER", "BOOLEAN":
		default:
			return nil, false
		}
		for _, allowed := range f.AllowedValues {
			if !validSearchText(allowed, 0, 500) {
				return nil, false
			}
		}
		if index >= len(fields) {
			return nil, false
		}
		field, ok := fields[index].(map[string]any)
		if !ok {
			return nil, false
		}
		field["label"], field["help"] = f.Label, f.Help
	}
	return v, true
}
