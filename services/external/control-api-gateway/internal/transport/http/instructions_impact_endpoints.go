package httptransport

import (
	"fmt"
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// PrepareInstructionsImpact delegates exact agent-instruction impact preparation under mutation
// headers and accepts only a PREPARED AGENT_INSTRUCTIONS plan bound to the requested agent and
// expected source version.
// PrepareInstructionsImpact поручает подготовку impact точных инструкций агента с mutation
// заголовками и принимает только PREPARED AGENT_INSTRUCTIONS plan с запрошенным агентом и
// ожидаемой source version.
func (s *Server) PrepareInstructionsImpact(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.PrepareInstructionsImpactParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if !fileTargetRef(ref) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.PrepareInstructionsImpact(r.Context(), &cp.PrepareInstructionsImpactRequest{Mutation: m, AgentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(response.GetPlan())
	if !ok || plan.Kind != "AGENT_INSTRUCTIONS" || stringValue(plan.SourceRef) != ref || plan.SourceVersion != m.GetExpectedVersion() || plan.State != "PREPARED" {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// PreparePromptTemplateImpact prepares impact for an exact configuration revision and requires the
// PREPARED PROMPT_TEMPLATE plan to match configuration, expected source version and selected
// revision. No publication or rebinding occurs here.
// PreparePromptTemplateImpact готовит impact точной revision конфигурации и требует совпадения
// PREPARED PROMPT_TEMPLATE plan с configuration, ожидаемой source version и выбранной revision.
// Публикация и rebind здесь не выполняются.
func (s *Server) PreparePromptTemplateImpact(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.PreparePromptTemplateImpactParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if !fileTargetRef(ref) || !fileTargetRef(revision) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.PreparePromptTemplateImpact(r.Context(), &cp.PreparePromptTemplateImpactRequest{Mutation: m, ConfigurationRef: ref, RevisionRef: revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(response.GetPlan())
	if !ok || plan.Kind != "PROMPT_TEMPLATE" || stringValue(plan.SourceRef) != ref || plan.SourceVersion != m.GetExpectedVersion() || plan.DraftRef != revision || plan.State != "PREPARED" {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// validRevisionImpactSelection requires a valid plan ref and an explicitly present selection of at
// most 1000 unique valid item refs. An empty selection is allowed, while nil is rejected to
// preserve intentional selection semantics.
// validRevisionImpactSelection требует корректный plan ref и явно присутствующий выбор не более
// 1000 уникальных корректных item refs. Пустой выбор разрешён, а nil отклоняется для сохранения
// явного намерения выбора.
func validRevisionImpactSelection(ref string, items []string) bool {
	if !fileTargetRef(ref) || items == nil || len(items) > 1000 {
		return false
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !fileTargetRef(item) || seen[item] {
			return false
		}
		seen[item] = true
	}
	return true
}

// publishInstructionsWithImpact requires explicit plan/selected items without a caller-assigned
// published instruction ref and delegates the specialized owner publication. It validates an
// APPLIED version-two plan and the exact advanced agent version before emitting agent metadata and
// plan with ETag.
// publishInstructionsWithImpact требует явные plan/selected items без назначенного caller
// published instruction ref и поручает специализированную публикацию владельцу. До metadata агента
// и plan с ETag проверяются APPLIED plan версии два и точная возросшая версия агента.
func (s *Server) publishInstructionsWithImpact(w http.ResponseWriter, r *http.Request, ref string, mutation *cp.MutationContext, body generated.InstructionCommand) {
	if body.PlanRef == nil || body.SelectedItemRefs == nil || body.PublishedInstructionRef != nil || !validRevisionImpactSelection(*body.PlanRef, *body.SelectedItemRefs) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.PublishInstructionDraft(r.Context(), &cp.PublishInstructionDraftRequest{Mutation: mutation, AgentRef: ref, PlanRef: *body.PlanRef, SelectedItemRefs: *body.SelectedItemRefs})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(response.GetPlan())
	agent := response.GetAgent()
	if !ok || plan.Kind != "AGENT_INSTRUCTIONS" || plan.Ref != *body.PlanRef || stringValue(plan.SourceRef) != ref || plan.SourceVersion != mutation.GetExpectedVersion() || plan.State != "APPLIED" || plan.Version != 2 || int64(len(*body.SelectedItemRefs)) > plan.Total || agent == nil || agent.Ref != ref || !fileTargetRef(agent.ProjectRef) || !validManagedVersion(agent.Version) || agent.Version != plan.SourceVersion+1 {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", agent.Version))
	writeJSON(w, http.StatusOK, map[string]any{"agent": map[string]any{"ref": agent.Ref, "projectRef": agent.ProjectRef, "version": agent.Version}, "plan": plan})
}
