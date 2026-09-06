package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (s *Server) PrepareRuntimeSecretDraftImpact(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.PrepareRuntimeSecretDraftImpactParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.PrepareRuntimeSecretDraftImpact(r.Context(), &cp.PrepareRuntimeSecretDraftImpactRequest{Mutation: mutation, DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := secretDraftImpactPlanView(response.GetPlan())
	if !ok || plan.DraftRef != ref || plan.DraftVersion != mutation.GetExpectedVersion() {
		invalidSecretDraft(w)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (s *Server) GetRuntimeSecretDraftImpact(w http.ResponseWriter, r *http.Request, ref generated.OpaqueRef, p generated.GetRuntimeSecretDraftImpactParams) {
	setRuntimeSecretHeaders(w)
	if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.GetRuntimeSecretDraftImpact(r.Context(), &cp.GetRuntimeSecretDraftImpactRequest{PlanRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := secretDraftImpactPlanView(response.GetPlan())
	if !ok || plan.Ref != ref || len(response.GetItems()) > 100 || response.GetTotal() < int64(len(response.GetItems())) || response.GetTotal() > plan.Total || len(response.GetPage().GetNextPageToken()) > 512 {
		invalidSecretDraft(w)
		return
	}
	result := generated.RuntimeSecretDraftImpactPage{Plan: plan, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken(), Items: []generated.RuntimeSecretDraftImpactItem{}}
	seen := map[string]bool{}
	for _, input := range response.GetItems() {
		item, valid := secretDraftImpactItemView(input, plan.State)
		if !valid || seen[item.Ref] {
			invalidSecretDraft(w)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}

func secretDraftImpactPlanView(v *cp.RuntimeSecretDraftImpactPlan) (generated.RuntimeSecretDraftImpactPlan, bool) {
	result := generated.RuntimeSecretDraftImpactPlan{}
	if v == nil || !validManagedVersion(v.GetDraftVersion()) || !validManagedVersion(v.GetSecretVersion()) || v.GetSourceRevision() < 0 || v.GetSourceRevision() > maximumSafeJSONInteger || !validManagedDigest(v.GetDigest()) || v.GetTotal() < 0 || v.GetTotal() > 1000 || v.GetExpiresAt() == nil || v.GetExpiresAt().CheckValid() != nil {
		return result, false
	}
	for _, ref := range []string{v.GetRef(), v.GetDraftRef(), v.GetSecretRef()} {
		if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 {
			return result, false
		}
	}
	state := generated.RuntimeSecretDraftImpactPlanState(strings.TrimPrefix(v.GetState().String(), "RUNTIME_SECRET_DRAFT_IMPACT_STATE_"))
	if !state.Valid() {
		return result, false
	}
	return generated.RuntimeSecretDraftImpactPlan{Ref: v.GetRef(), DraftRef: v.GetDraftRef(), DraftVersion: v.GetDraftVersion(), SecretRef: v.GetSecretRef(), SecretVersion: v.GetSecretVersion(), SourceRevision: v.GetSourceRevision(), Digest: v.GetDigest(), Total: v.GetTotal(), ExpiresAt: v.GetExpiresAt().AsTime(), State: state}, true
}

func secretDraftImpactItemView(v *cp.RuntimeSecretDraftImpactItem, state generated.RuntimeSecretDraftImpactPlanState) (generated.RuntimeSecretDraftImpactItem, bool) {
	result := generated.RuntimeSecretDraftImpactItem{}
	if v == nil || !opaqueHTTPReference.MatchString(v.GetRef()) || len(v.GetRef()) > 96 {
		return result, false
	}
	consumer, ok := secretImpactConsumerView(v.GetConsumer(), 0)
	outcome := generated.RuntimeSecretDraftImpactItemOutcome(strings.TrimPrefix(v.GetOutcome().String(), "RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_"))
	if !ok || !outcome.Valid() || state == "APPLIED" && outcome == "PENDING" || state != "APPLIED" && outcome != "PENDING" && outcome != "NOT_SELECTED" {
		return result, false
	}
	result = generated.RuntimeSecretDraftImpactItem{Ref: v.GetRef(), Consumer: consumer, Outcome: outcome}
	if outcome != "APPLIED" {
		return result, v.GetResultEnvironmentVersionRef() == "" && v.GetResultBindingRef() == "" && v.GetResultBindingVersion() == 0
	}
	if !opaqueHTTPReference.MatchString(v.GetResultEnvironmentVersionRef()) || len(v.GetResultEnvironmentVersionRef()) > 96 || v.GetResultEnvironmentVersionRef() == consumer.EnvironmentVersionRef {
		return result, false
	}
	result.ResultEnvironmentVersionRef = optionalManagedString(v.GetResultEnvironmentVersionRef())
	if consumer.Consumer == nil {
		return result, v.GetResultBindingRef() == "" && v.GetResultBindingVersion() == 0
	}
	if v.GetResultBindingRef() != consumer.Consumer.BindingRef || !validManagedVersion(v.GetResultBindingVersion()) || v.GetResultBindingVersion() <= consumer.Consumer.BindingVersion {
		return result, false
	}
	result.ResultBindingRef = optionalManagedString(v.GetResultBindingRef())
	version := v.GetResultBindingVersion()
	result.ResultBindingVersion = &version
	return result, true
}
