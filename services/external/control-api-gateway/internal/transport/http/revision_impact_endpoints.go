package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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
	for _, value := range response.Items {
		item, valid := revisionImpactItemView(value, plan)
		if !valid || seen[item.Ref] {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

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

func revisionImpactItemView(v *cp.RevisionImpactItem, plan generated.RevisionImpactPlan) (generated.RevisionImpactItem, bool) {
	result := generated.RevisionImpactItem{}
	if v == nil || !validManagedVersion(v.ConsumerVersion) || !validManagedVersion(v.BindingVersion) {
		return result, false
	}
	kind := strings.TrimPrefix(v.ConsumerKind.String(), "REVISION_IMPACT_CONSUMER_KIND_")
	if kind != "AGENT" && (plan.Kind != "PROMPT_TEMPLATE" || kind != "AGENT_CONTINUATION" && kind != "WORKFLOW" && kind != "SCHEDULE") {
		return result, false
	}
	if !fileTargetRef(v.ProjectRef) && !(v.ProjectRef == "" && plan.Kind == "PROMPT_TEMPLATE" && (kind == "AGENT" || kind == "AGENT_CONTINUATION")) {
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
	result = generated.RevisionImpactItem{Ref: v.Ref, ProjectRef: v.ProjectRef, ConsumerKind: generated.RevisionImpactItemConsumerKind(kind), ConsumerRef: v.ConsumerRef, ConsumerVersion: v.ConsumerVersion, BindingRef: v.BindingRef, BindingVersion: v.BindingVersion, SourceRevisionRef: v.SourceRevisionRef, Outcome: generated.RevisionImpactItemOutcome(outcome), ResultRevisionRef: optionalManagedString(v.ResultRevisionRef), ResultBindingRef: optionalManagedString(v.ResultBindingRef)}
	if outcome == "APPLIED" {
		result.ResultBindingVersion = &v.ResultBindingVersion
		result.ResultConsumerVersion = &v.ResultConsumerVersion
	}
	return result, true
}
