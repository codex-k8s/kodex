package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (s *Server) PrepareRoleImageImpactPlan(w http.ResponseWriter, r *http.Request, configuration generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.PrepareRoleImageImpactPlanParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if !fileTargetRef(configuration) || !fileTargetRef(revision) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.PrepareRoleImageImpactPlan(r.Context(), &cp.PrepareRoleImageImpactPlanRequest{Mutation: mutation, ConfigurationRef: configuration, RevisionRef: revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := roleImageImpactPlanView(response.GetPlan())
	if !ok || plan.ConfigurationRef != configuration || plan.RevisionRef != revision || plan.ConfigurationVersion != mutation.GetExpectedVersion() {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (s *Server) GetRoleImageImpactPlan(w http.ResponseWriter, r *http.Request, ref generated.OpaqueRef, p generated.GetRoleImageImpactPlanParams) {
	if !fileTargetRef(ref) || !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.GetRoleImageImpactPlan(r.Context(), &cp.GetRoleImageImpactPlanRequest{PlanRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := roleImageImpactPlanView(response.GetPlan())
	next := response.GetPage().GetNextPageToken()
	if !ok || plan.Ref != ref || response.GetTotal() < int64(len(response.GetItems())) || response.GetTotal() > plan.Total || len(response.GetItems()) > int(page(p.PageSize, p.PageToken).PageSize) || len(next) > 512 || !utf8.ValidString(next) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RoleImageImpactPage{Plan: plan, Items: []generated.RoleImageImpactItem{}, Total: response.Total, NextPageToken: optionalManagedString(next)}
	seen := map[string]bool{}
	for _, value := range response.Items {
		item, valid := roleImageImpactItemView(value, plan.State)
		if !valid || seen[item.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}

func roleImageImpactPlanView(v *cp.RoleImageImpactPlan) (generated.RoleImageImpactPlan, bool) {
	result := generated.RoleImageImpactPlan{}
	if v == nil || !validManagedVersion(v.Version) || !validManagedVersion(v.ConfigurationVersion) || !validManagedVersion(v.RecipeGeneration) || v.Total < 0 || v.Total > 1000 || v.CreatedAt == nil || v.CreatedAt.CheckValid() != nil || v.ExpiresAt == nil || v.ExpiresAt.CheckValid() != nil || !v.ExpiresAt.AsTime().After(v.CreatedAt.AsTime()) || !strings.HasPrefix(v.ArtifactDigest, "sha256:") || !validManagedDigest(strings.TrimPrefix(v.ArtifactDigest, "sha256:")) {
		return result, false
	}
	for _, ref := range []string{v.Ref, v.ConfigurationRef, v.RevisionRef, v.RecipeRef, v.BuildRef, v.ArtifactRef} {
		if !fileTargetRef(ref) {
			return result, false
		}
	}
	for _, digest := range []string{v.RevisionDigest, v.AdmissionPolicyDigest, v.Digest} {
		if !validManagedDigest(digest) {
			return result, false
		}
	}
	state := strings.TrimPrefix(v.State.String(), "ROLE_IMAGE_IMPACT_PLAN_STATE_")
	switch state {
	case "PREPARED", "APPLIED", "EXPIRED":
	default:
		return result, false
	}
	result = generated.RoleImageImpactPlan{Ref: v.Ref, Version: v.Version, ConfigurationRef: v.ConfigurationRef, ConfigurationVersion: v.ConfigurationVersion, RevisionRef: v.RevisionRef, RevisionDigest: v.RevisionDigest, RecipeRef: v.RecipeRef, RecipeGeneration: v.RecipeGeneration, BuildRef: v.BuildRef, ArtifactRef: v.ArtifactRef, ArtifactDigest: v.ArtifactDigest, AdmissionPolicyDigest: v.AdmissionPolicyDigest, Digest: v.Digest, Total: v.Total, State: generated.RoleImageImpactPlanState(state), CreatedAt: v.CreatedAt.AsTime(), ExpiresAt: v.ExpiresAt.AsTime()}
	return result, true
}

func roleImageImpactItemView(v *cp.RoleImageImpactItem, state generated.RoleImageImpactPlanState) (generated.RoleImageImpactItem, bool) {
	result := generated.RoleImageImpactItem{}
	if v == nil || !fileTargetRef(v.Ref) || !fileTargetRef(v.EnvironmentRef) || !validManagedVersion(v.EnvironmentVersion) || !fileTargetRef(v.SourceVersionRef) || !validManagedDigest(v.SourceVersionDigest) || v.Consumer == nil || !fileTargetRef(v.Consumer.ProjectRef) || v.Consumer.VersionRef != v.SourceVersionRef {
		return result, false
	}
	outcome := strings.TrimPrefix(v.Outcome.String(), "ROLE_IMAGE_IMPACT_OUTCOME_")
	switch outcome {
	case "PENDING", "APPLIED", "CONFLICT", "FORBIDDEN", "NOT_SELECTED":
	default:
		return result, false
	}
	if state == "APPLIED" && outcome == "PENDING" || state != "APPLIED" && outcome != "PENDING" {
		return result, false
	}
	result = generated.RoleImageImpactItem{Ref: v.Ref, EnvironmentRef: v.EnvironmentRef, EnvironmentVersion: v.EnvironmentVersion, SourceVersionRef: v.SourceVersionRef, SourceVersionDigest: v.SourceVersionDigest, ProjectRef: v.Consumer.ProjectRef, Outcome: generated.RoleImageImpactItemOutcome(outcome)}
	c := v.Consumer
	if c.AgentRef != "" {
		consumer := generated.RuntimeEnvironmentConsumer{AgentRef: c.AgentRef, AgentVersion: c.AgentVersion, BindingRef: c.BindingRef, BindingVersion: c.BindingVersion, VersionRef: c.VersionRef, ProjectRef: c.ProjectRef}
		if !validEnvironmentConsumer(consumer) {
			return result, false
		}
		result.Consumer = &consumer
	} else if c.AgentVersion != 0 || c.BindingRef != "" || c.BindingVersion != 0 {
		return result, false
	}
	if outcome == "APPLIED" {
		if !fileTargetRef(v.ResultEnvironmentVersionRef) || v.ResultEnvironmentVersionRef == v.SourceVersionRef {
			return result, false
		}
		result.ResultEnvironmentVersionRef = optionalManagedString(v.ResultEnvironmentVersionRef)
		if c.AgentRef != "" {
			if v.ResultBindingRef != c.BindingRef || !validManagedVersion(v.ResultBindingVersion) || v.ResultBindingVersion <= c.BindingVersion {
				return result, false
			}
			result.ResultBindingRef = optionalManagedString(v.ResultBindingRef)
			result.ResultBindingVersion = &v.ResultBindingVersion
		} else if v.ResultBindingRef != "" || v.ResultBindingVersion != 0 {
			return result, false
		}
	} else if v.ResultEnvironmentVersionRef != "" || v.ResultBindingRef != "" || v.ResultBindingVersion != 0 {
		return result, false
	}
	return result, true
}
