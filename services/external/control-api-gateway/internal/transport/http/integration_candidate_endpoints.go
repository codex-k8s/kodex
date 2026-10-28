package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// candidateString returns an optional string-like query value unchanged, mapping nil to empty; it performs no enum, scope or UTF-8 validation.
// candidateString возвращает необязательное string-like query value без изменений, nil — пустую строку; enum, scope и UTF-8 не проверяет.
func candidateString[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

// candidateRecipient looks up the exact recipient enum suffix and maps unknown names to zero. Later stage validation, not this conversion, determines whether AGENT or WORKFLOW is allowed.
// candidateRecipient ищет точный суффикс recipient enum, неизвестное имя даёт zero. Допуск AGENT и WORKFLOW определяет последующая stage validation, не это преобразование.
func candidateRecipient(v string) cp.IntegrationGrantRecipientKind {
	return cp.IntegrationGrantRecipientKind(cp.IntegrationGrantRecipientKind_value["INTEGRATION_GRANT_RECIPIENT_KIND_"+v])
}

// ListIntegrationGrantConnectionCandidates builds the allowed stage-zero GRANT or USE context, validates query shape and reads connection candidates through the dedicated owner RPC. The returned page is checked against that exact context; candidate display does not create a grant or authorize integration use.
// ListIntegrationGrantConnectionCandidates строит допустимый stage-zero GRANT/USE context, проверяет query и читает кандидатов подключений отдельным owner RPC. Страница сверяется с точным context; показ кандидатов grant не создаёт и использование интеграции не разрешает.
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

// ListIntegrationGrantProjectCandidates accepts only the connection-selected GRANT stage, validates the query and reads project candidates through its specialized owner RPC. It forwards the page to contextual validation rather than treating the connection reference as authority.
// ListIntegrationGrantProjectCandidates принимает только GRANT stage с выбранным connection, проверяет query и читает проекты специализированным owner RPC. Передаёт страницу contextual validation, не считая connection reference полномочием.
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

// ListIntegrationGrantRecipientCandidates requires the connection/project/recipient-kind prefix for GRANT stage two, then reads and validates owner recipient candidates against it. The client selects a candidate kind, not a role or permission grant.
// ListIntegrationGrantRecipientCandidates требует connection/project/recipient-kind prefix для второго GRANT stage, затем читает и сверяет owner candidates. Клиент выбирает вид кандидата, а не выдаёт роль или permission grant.
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

// ListIntegrationGrantCapabilityCandidates requires the full connection/project/recipient prefix before the dedicated capability candidate query and public page validation. Current grant refs are display pins; this endpoint neither creates nor edits grants.
// ListIntegrationGrantCapabilityCandidates требует полный connection/project/recipient prefix до отдельного capability candidate query и проверки публичной страницы. Current grant refs — display pins; endpoint grants не создаёт и не меняет.
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

// candidateRequestValid allows only the selected stage's query prefix, rejecting unknown, duplicate and forbidden empty parameters. It checks bounded search/page/refs, empty-context GRANT discovery and complete agent USE context; these structural rules do not replace owner eligibility.
// candidateRequestValid допускает только query prefix выбранного stage, отклоняет неизвестные, повторные и запрещённые пустые параметры. Проверяет bounded search/page/refs, empty-context GRANT discovery и полный agent USE context; структура не заменяет owner eligibility.
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

// candidatePage describes common owner candidate-page metadata for validation while concrete reply types still determine the stage. It carries context/version pins for consistency, not an independently verified authorization grant.
// candidatePage описывает общие metadata owner candidate-page для проверки, тогда как stage определяется конкретным типом ответа. Context/version pins обеспечивают согласованность, а не самостоятельно проверенный authorization grant.
type candidatePage interface {
	proto.Message
	// GetPage reads optional owner pagination metadata; callers validate token shape before public projection.
	// GetPage читает необязательные owner pagination metadata; форму токена вызывающий код проверяет до публичной проекции.
	GetPage() *cp.PageInfo
	// GetTotal reads the owner total for safe-integer and actual-item count checks, not a locally recomputed count.
	// GetTotal читает owner total для safe-integer и actual-item count checks, а не локально пересчитанное количество.
	GetTotal() int64
	// GetContextDigest reads the context digest to compare page and pins; the getter does not recompute or authenticate it.
	// GetContextDigest читает context digest для сравнения страницы и pins; getter его не пересчитывает и не аутентифицирует.
	GetContextDigest() string
	// GetContext reads the echoed selection context for exact protobuf equality with the validated request.
	// GetContext читает возвращённый selection context для точного protobuf equality с проверенным запросом.
	GetContext() *cp.IntegrationGrantCandidateContext
	// GetPins reads owner version and definition pins; required and absent fields depend on the selected context prefix.
	// GetPins читает owner version и definition pins; обязательность и отсутствие полей зависят от выбранного context prefix.
	GetPins() *cp.IntegrationGrantCandidatePins
}

// candidatePins builds a fresh public pin map only when context digest and required safe versions/definition pins/workflow revision are present and absent-prefix pins stay zero or empty. It validates shape and correspondence, not live freshness or authority.
// candidatePins создаёт новую публичную map pins только при наличии context digest, требуемых safe versions/definition pins/workflow revision и нулевых либо пустых pins отсутствующего prefix. Проверяет форму и соответствие, не live freshness и полномочия.
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

// candidateContext projects only nonempty selection refs/keys and an optional recipient enum suffix into a fresh public map. It performs no resource lookup, grant creation or owner eligibility check.
// candidateContext проецирует только непустые selection refs/keys и необязательный recipient enum suffix в новую публичную map. Resource lookup, grant creation и owner eligibility check не выполняет.
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

// candidateReason accepts only the closed reason range and consistent GRANT/USE readiness flags; USE must be ready and usable without grantable, while GRANT's grantable bit follows READY. It returns a display suffix, not executable permission.
// candidateReason принимает только закрытый reason range и согласованные GRANT/USE readiness flags: USE требует ready и usable без grantable, у GRANT grantable совпадает с READY. Возвращает display suffix, не исполняемое разрешение.
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

// writeCandidatePage writes the validated public candidate map or a non-retryable 502 without partial items; all context/type/pin validation is delegated to candidatePageView.
// writeCandidatePage выдаёт проверенную публичную map кандидатов либо non-retryable 502 без partial items; context/type/pin validation делегирована candidatePageView.
func writeCandidatePage(w http.ResponseWriter, v candidatePage, c *cp.IntegrationGrantCandidateContext, purpose string, stage, limit int) {
	result, ok := candidatePageView(v, c, purpose, stage, limit)
	if !ok {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, 200, result)
}

// candidatePageView requires a valid concrete protobuf reply matching the selected stage and exact request context, then checks page/item pins, reason flags, unique identities and count bounds. It allocates ordered public items and forwards a shape-valid cursor; it does not recompute the digest, compare cursor advancement or issue a grant.
// candidatePageView требует допустимый конкретный protobuf reply нужного stage и точный request context, затем сверяет page/item pins, reason flags, уникальные identity и count bounds. Выделяет упорядоченные публичные items и передаёт cursor допустимой формы; digest не пересчитывает, продвижение cursor не сравнивает и grant не выдаёт.
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
			if i == nil || !fileTargetRef(i.ConnectionRef) || !validSearchText(i.Name, 1, 500) || !validSearchText(i.DefinitionKey, 1, 500) || !validSearchText(i.ProviderName, 0, 500) || (i.CredentialKind != "" && i.CredentialKind != "TOKEN" && i.CredentialKind != "PASSWORD") || (i.ProjectRef != "" && !fileTargetRef(i.ProjectRef)) || len(i.ResourceScope) > 9 || i.DefinitionKey != "ssh-compose" && len(i.ResourceScope) > 8 {
				return nil, false
			}
			if i.DefinitionKey == "ssh-compose" && len(i.ResourceScope) != 0 {
				if _, err := sshcompose.TargetFromResourceScope(i.ResourceScope); err != nil {
					return nil, false
				}
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

// candidateCapabilityView validates bounded capability metadata and a closed set of input-field types, then maps the protobuf while preserving literal description/labels/help and explicit empty fields. Risk/approval/resource enums must appear in the mapping; the result describes a capability and cannot execute it.
// candidateCapabilityView проверяет ограниченные capability metadata и закрытый набор input-field types, затем преобразует protobuf с сохранением literal description/labels/help и явно пустых fields. Risk/approval/resource enums должны присутствовать в mapping; результат описывает capability, но не исполняет её.
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
