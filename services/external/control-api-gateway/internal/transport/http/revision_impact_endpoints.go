package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// PrepareEnvironmentDraftImpact prepares an owner impact plan for the exact environment draft
// under mutation headers and requires matching draft/expected version, RUNTIME_ENVIRONMENT kind
// and PREPARED state before 201.
// PrepareEnvironmentDraftImpact готовит impact plan владельца для точного черновика среды с
// mutation заголовками и требует совпадения draft/expected version, вида RUNTIME_ENVIRONMENT и
// PREPARED до 201.
func (s *Server) PrepareEnvironmentDraftImpact(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef, p generated.PrepareEnvironmentDraftImpactParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if !fileTargetRef(ref) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.PrepareEnvironmentDraftImpact(r.Context(), &cp.PrepareEnvironmentDraftImpactRequest{Mutation: mutation, DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(response.GetPlan())
	if !ok || plan.DraftRef != ref || plan.DraftVersion != mutation.GetExpectedVersion() || plan.Kind != "RUNTIME_ENVIRONMENT" || plan.State != "PREPARED" {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

// GetRevisionImpactPlan reads a bounded filtered plan page, checks plan identity and page
// counts/cursor, and validates unique items sharing an organization. Owner-selected eligibility
// and outcome cardinality are preserved without filtering invalid items.
// GetRevisionImpactPlan читает ограниченную отфильтрованную страницу plan, проверяет identity
// плана и counts/cursor и уникальные элементы общей организации. Допуск владельца и cardinality
// исходов сохраняются без фильтрации неверных элементов.
func (s *Server) GetRevisionImpactPlan(w http.ResponseWriter, r *http.Request, ref generated.OpaqueRef, p generated.GetRevisionImpactPlanParams) {
	if !fileTargetRef(ref) || !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.GetRevisionImpactPlan(r.Context(), &cp.GetRevisionImpactPlanRequest{PlanRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(response.GetPlan())
	next := response.GetPage().GetNextPageToken()
	if !ok || plan.Ref != ref || response.Total < int64(len(response.Items)) || response.Total > plan.Total || len(response.Items) > int(page(p.PageSize, p.PageToken).PageSize) || len(next) > 512 || !utf8.ValidString(next) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RevisionImpactPage{Plan: plan, Items: []generated.RevisionImpactItem{}, Total: response.Total, NextPageToken: optionalManagedString(next)}
	seen := map[string]bool{}
	organizationRef := ""
	for _, value := range response.Items {
		item, valid := revisionImpactItemView(value, plan)
		if !valid || seen[item.Ref] || organizationRef != "" && organizationRef != item.OrganizationRef {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		organizationRef = item.OrganizationRef
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

// revisionImpactPlanView validates bounded plan identity/version/time/digests, kind-specific
// optional source pins and PREPARED/EXPIRED versus APPLIED publication fields. New environment
// plans may have no source; template/instruction plans retain their distinct revision rules.
// revisionImpactPlanView проверяет ограниченные identity/version/time/digests плана, optional
// source pins по виду и поля публикации PREPARED/EXPIRED против APPLIED. У новой среды может не
// быть источника; template/instruction планы сохраняют разные правила revision.
func revisionImpactPlanView(v *cp.RevisionImpactPlan) (generated.RevisionImpactPlan, bool) {
	result := generated.RevisionImpactPlan{}
	if v == nil || !validManagedVersion(v.Version) || !validManagedVersion(v.DraftVersion) || !fileTargetRef(v.Ref) || !fileTargetRef(v.DraftRef) || !validManagedDigest(v.TargetDigest) || !validManagedDigest(v.Digest) || v.Total < 0 || v.Total > 1000 || v.CreatedAt == nil || v.CreatedAt.CheckValid() != nil || v.ExpiresAt == nil || v.ExpiresAt.CheckValid() != nil || !v.ExpiresAt.AsTime().After(v.CreatedAt.AsTime()) {
		return result, false
	}
	kind := strings.TrimPrefix(v.Kind.String(), "REVISION_IMPACT_KIND_")
	if kind != "RUNTIME_ENVIRONMENT" && kind != "PROMPT_TEMPLATE" && kind != "AGENT_INSTRUCTIONS" {
		return result, false
	}
	if v.SourceRef == "" {
		if kind != "RUNTIME_ENVIRONMENT" || v.SourceVersion != 0 || v.SourceRevisionRef != "" {
			return result, false
		}
	} else if !fileTargetRef(v.SourceRef) || !validManagedVersion(v.SourceVersion) || (v.SourceRevisionRef != "" || kind != "PROMPT_TEMPLATE") && !fileTargetRef(v.SourceRevisionRef) {
		return result, false
	}
	state := strings.TrimPrefix(v.State.String(), "REVISION_IMPACT_STATE_")
	switch state {
	case "PREPARED", "EXPIRED":
		if v.PublishedRevisionRef != "" {
			return result, false
		}
	case "APPLIED":
		if !fileTargetRef(v.PublishedRevisionRef) || kind != "RUNTIME_ENVIRONMENT" && v.PublishedRevisionRef != v.DraftRef {
			return result, false
		}
	default:
		return result, false
	}
	result = generated.RevisionImpactPlan{Ref: v.Ref, Version: v.Version, Kind: generated.RevisionImpactPlanKind(kind), SourceRef: optionalManagedString(v.SourceRef), SourceVersion: v.SourceVersion, SourceRevisionRef: optionalManagedString(v.SourceRevisionRef), DraftRef: v.DraftRef, DraftVersion: v.DraftVersion, TargetDigest: v.TargetDigest, Digest: v.Digest, Total: v.Total, State: generated.RevisionImpactPlanState(state), CreatedAt: v.CreatedAt.AsTime(), ExpiresAt: v.ExpiresAt.AsTime(), PublishedRevisionRef: optionalManagedString(v.PublishedRevisionRef)}
	return result, true
}

// revisionImpactItemView validates consumer kind/scope/versions and the item outcome against its
// plan. APPLIED needs a different published result revision and advanced binding metadata; other
// outcomes must carry no result pins, preserving partial publication facts.
// revisionImpactItemView проверяет вид, область и версии consumer и исход элемента по плану.
// APPLIED требует другую опубликованную result revision и продвинутые metadata привязки; другие
// исходы не несут result pins, сохраняя факты частичной публикации.
func revisionImpactItemView(v *cp.RevisionImpactItem, plan generated.RevisionImpactPlan) (generated.RevisionImpactItem, bool) {
	result := generated.RevisionImpactItem{}
	if v == nil || !validManagedVersion(v.ConsumerVersion) || !validManagedVersion(v.BindingVersion) {
		return result, false
	}
	kind := strings.TrimPrefix(v.ConsumerKind.String(), "REVISION_IMPACT_CONSUMER_KIND_")
	if kind != "AGENT" && (plan.Kind != "PROMPT_TEMPLATE" || kind != "AGENT_CONTINUATION" && kind != "WORKFLOW" && kind != "SCHEDULE") {
		return result, false
	}
	if !validRuntimeResourceScope(runtimeResourceScopeKind(v.GetScopeKind().String()), v.GetOrganizationRef(), v.GetProjectRef()) ||
		v.GetScopeKind() == cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION && kind != "AGENT" && kind != "AGENT_CONTINUATION" {
		return result, false
	}
	for _, ref := range []string{v.Ref, v.ConsumerRef, v.BindingRef, v.SourceRevisionRef} {
		if !fileTargetRef(ref) {
			return result, false
		}
	}
	outcome := strings.TrimPrefix(v.Outcome.String(), "REVISION_IMPACT_OUTCOME_")
	switch outcome {
	case "PENDING", "APPLIED", "CONFLICT", "FORBIDDEN", "NOT_SELECTED":
	default:
		return result, false
	}
	if (plan.State == "APPLIED") == (outcome == "PENDING") {
		return result, false
	}
	if outcome == "APPLIED" {
		if !fileTargetRef(v.ResultRevisionRef) || v.ResultRevisionRef == v.SourceRevisionRef || v.ResultRevisionRef != stringValue(plan.PublishedRevisionRef) || v.ResultBindingRef != v.BindingRef || !validManagedVersion(v.ResultBindingVersion) || v.ResultBindingVersion <= v.BindingVersion || !validManagedVersion(v.ResultConsumerVersion) || v.ResultConsumerVersion < v.ConsumerVersion || plan.Kind != "PROMPT_TEMPLATE" && v.ResultConsumerVersion == v.ConsumerVersion {
			return result, false
		}
	} else if v.ResultRevisionRef != "" || v.ResultBindingRef != "" || v.ResultBindingVersion != 0 || v.ResultConsumerVersion != 0 {
		return result, false
	}
	result = generated.RevisionImpactItem{Ref: v.Ref, ProjectRef: v.ProjectRef, ScopeKind: runtimeResourceScopeKind(v.GetScopeKind().String()), OrganizationRef: v.GetOrganizationRef(), ConsumerKind: generated.RevisionImpactItemConsumerKind(kind), ConsumerRef: v.ConsumerRef, ConsumerVersion: v.ConsumerVersion, BindingRef: v.BindingRef, BindingVersion: v.BindingVersion, SourceRevisionRef: v.SourceRevisionRef, Outcome: generated.RevisionImpactItemOutcome(outcome), ResultRevisionRef: optionalManagedString(v.ResultRevisionRef), ResultBindingRef: optionalManagedString(v.ResultBindingRef)}
	if outcome == "APPLIED" {
		result.ResultBindingVersion = &v.ResultBindingVersion
		result.ResultConsumerVersion = &v.ResultConsumerVersion
	}
	return result, true
}
