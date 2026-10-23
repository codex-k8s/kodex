package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// PrepareRuntimeSecretDraftImpact requires a versioned mutation and asks the owner to prepare an immutable draft impact plan, then checks exact draft ref/version before a no-store 201. It returns selected metadata without publishing a secret, replacing environments or approving the plan itself.
// PrepareRuntimeSecretDraftImpact требует versioned mutation и запрашивает owner preparation неизменяемого draft impact plan, затем сверяет точные draft ref/version до no-store 201. Возвращает выбранные metadata без публикации секрета, замены окружений и самостоятельного подтверждения плана.
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

// GetRuntimeSecretDraftImpact validates plan/search/page syntax, forwards filtering to the owner and checks exact plan, bounded filtered totals, unique item refs and a single consumer owner tuple before full output. It does not filter away unauthorized items locally, compare cursor advancement or enforce current expiry by a local clock.
// GetRuntimeSecretDraftImpact проверяет plan/search/page syntax, передаёт фильтрацию владельцу и сверяет точный plan, bounded filtered totals, уникальные item refs и единый consumer owner tuple до полной выдачи. Не скрывает unauthorized items локальной фильтрацией, не сравнивает продвижение cursor и не применяет current expiry по локальным часам.
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
	owner := ""
	for _, input := range response.GetItems() {
		item, valid := secretDraftImpactItemView(input, plan.State)
		key := impactConsumerOwnerKey(item.Consumer.ScopeKind, item.Consumer.OrganizationRef, item.Consumer.ProjectRef)
		if !valid || seen[item.Ref] || owner != "" && key != owner {
			invalidSecretDraft(w)
			return
		}
		seen[item.Ref] = true
		owner = key
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}

// secretDraftImpactPlanView requires valid refs, safe positive draft/secret versions, bounded source revision/digest/total and a valid expiry timestamp with a known state. It maps an immutable owner snapshot, not evidence that the expiry is still in the future or permission to apply selected items.
// secretDraftImpactPlanView требует допустимые refs, safe positive draft/secret versions, bounded source revision/digest/total и допустимый expiry timestamp с известным state. Преобразует immutable owner snapshot, не доказывает будущее expiry и не разрешает применение выбранных items.
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

// secretDraftImpactItemView checks consumer lineage and state-compatible outcomes, forbids result pins for non-applied items and requires a new environment version plus advanced exact binding pins when an agent-bound item is applied. Environment-only items cannot invent binding results; the mapper does not perform the replacements.
// secretDraftImpactItemView проверяет consumer lineage и совместимые со state outcomes, запрещает result pins для неприменённых items, для APPLIED требует новую environment version и продвинутые exact binding pins при привязке сотрудника. Environment-only items не могут выдумать binding result; mapper замену не выполняет.
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
