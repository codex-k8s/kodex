package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ListArtifactBindingTargets validates artifact/search/page controls, requests owner-filtered
// targets and emits only a matching valid target page. The artifact locator does not authorize
// binding or bypass owner eligibility.
// ListArtifactBindingTargets проверяет artifact/search/page параметры, запрашивает отфильтрованные
// владельцем цели и выдаёт только согласованную корректную страницу. Locator артефакта не
// разрешает привязку и не обходит допуск владельца.
func (server *Server) ListArtifactBindingTargets(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.ListArtifactBindingTargetsParams) {
	if !fileTargetRef(ref) || !validSearchText(stringValue(p.Query), 0, 200) || !fileTargetPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListArtifactBindingTargets(r.Context(), &cp.ListArtifactBindingTargetsRequest{ArtifactRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, ok := fileBindingTargetsView(response, ref, int(page(p.PageSize, p.PageToken).PageSize))
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// fileTargetPage allows omitted page controls or size 1..100 and a valid UTF-8 cursor of at most
// 2048 bytes. Cursor authority and continuation interpretation remain with the catalog owner.
// fileTargetPage допускает отсутствие параметров страницы либо размер 1..100 и UTF-8 cursor не
// более 2048 байт. Область cursor и толкование продолжения остаются у владельца каталога.
func fileTargetPage(size *int, token *string) bool {
	return (size == nil || *size >= 1 && *size <= 100) && (token == nil || len(*token) <= 2048 && utf8.ValidString(*token))
}

// fileTargetRef validates an 8..128-byte locator against the common opaque reference grammar,
// without inferring a resource type or checking ownership from its prefix.
// fileTargetRef проверяет locator размером 8..128 байт по общей грамматике opaque ref без вывода
// типа ресурса или владения из префикса.
func fileTargetRef(ref string) bool {
	return len(ref) >= 8 && len(ref) <= 128 && opaqueHTTPReference.MatchString(ref)
}

// fileBindingTargetsView binds the owner page to the requested artifact/version/project/digest and
// valid evaluation time, then checks unique agent targets and coherent bound/canBind/canUnbind
// reason pairs. Invalid partial pages are unusable.
// fileBindingTargetsView связывает страницу владельца с запрошенными
// artifact/version/project/digest и корректным временем оценки, затем проверяет уникальные
// цели-агенты и согласованные bound/canBind/canUnbind с причинами. Неверные частичные страницы
// непригодны.
func fileBindingTargetsView(v *cp.ListArtifactBindingTargetsResponse, ref string, limit int) (generated.ArtifactBindingTargetPage, bool) {
	result := generated.ArtifactBindingTargetPage{Items: []generated.ArtifactBindingTarget{}}
	next := v.GetPage().GetNextPageToken()
	if v == nil || v.ArtifactRef != ref || !validManagedVersion(v.ArtifactVersion) || !fileTargetRef(v.ProjectRef) || !validManagedDigest(v.Digest) || v.EvaluatedAt == nil || v.EvaluatedAt.CheckValid() != nil || v.Total < int64(len(v.Items)) || v.Total > maximumSafeJSONInteger || len(v.Items) > limit || !fileTargetPage(nil, &next) {
		return result, false
	}
	result.ArtifactRef, result.ArtifactVersion, result.ProjectRef, result.Digest, result.EvaluatedAt, result.Total = v.ArtifactRef, v.ArtifactVersion, v.ProjectRef, v.Digest, v.EvaluatedAt.AsTime(), v.Total
	result.NextPageToken = optionalManagedString(v.GetPage().GetNextPageToken())
	seen := map[string]bool{}
	for _, item := range v.Items {
		if item == nil || !fileTargetRef(item.AgentRef) || !validManagedVersion(item.AgentVersion) || !validSearchText(item.Name, 1, 200) || seen[item.AgentRef] {
			return result, false
		}
		seen[item.AgentRef] = true
		state := strings.TrimPrefix(item.State.String(), "AGENT_STATE_")
		switch state {
		case "DRAFT", "READY", "RUNNING", "DISABLED", "ARCHIVED":
		default:
			return result, false
		}
		bind, unbind := fileBindingReason(item.BindReason), fileBindingReason(item.UnbindReason)
		if bind == "" || unbind == "" || item.CanBind != (bind == "AVAILABLE") || item.CanUnbind != (unbind == "AVAILABLE") || item.CanBind && item.Bound || item.CanUnbind && !item.Bound {
			return result, false
		}
		result.Items = append(result.Items, generated.ArtifactBindingTarget{AgentRef: item.AgentRef, AgentVersion: item.AgentVersion, Name: item.Name, State: generated.ArtifactBindingTargetState(state), Bound: item.Bound, CanBind: item.CanBind, CanUnbind: item.CanUnbind, BindReason: generated.ArtifactBindingTargetReason(bind), UnbindReason: generated.ArtifactBindingTargetReason(unbind)})
	}
	return result, true
}

// fileBindingReason maps only the explicit artifact binding availability/blocker reasons to public
// names; unknown or unspecified values return empty text for rejection.
// fileBindingReason преобразует только явные причины доступности и блокировки привязки артефакта в
// публичные имена; неизвестные и unspecified дают пустой текст для отказа.
func fileBindingReason(reason cp.ArtifactBindingTargetReason) string {
	switch reason {
	case cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AVAILABLE, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_ALREADY_BOUND, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_NOT_BOUND, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AGENT_CAPABILITY_REQUIRED, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AGENT_ARCHIVED, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_ARTIFACT_UNAVAILABLE:
		return strings.TrimPrefix(reason.String(), "ARTIFACT_BINDING_TARGET_REASON_")
	}
	return ""
}

// GetRunAttachmentEligibility validates project/target and optional run selectors, maps only
// AGENT/WORKFLOW targets and requests owner eligibility. It validates response identity before
// exposing the read-only eligibility snapshot.
// GetRunAttachmentEligibility проверяет project/target и необязательный run, преобразует только
// цели AGENT/WORKFLOW и запрашивает допуск владельца. Identity ответа проверяется до выдачи снимка
// допуска только для чтения.
func (server *Server) GetRunAttachmentEligibility(w http.ResponseWriter, r *http.Request, project generated.ProjectRef, p generated.GetRunAttachmentEligibilityParams) {
	kind := string(p.TargetType)
	run := stringValue(p.RunRef)
	if !fileTargetRef(project) || !fileTargetRef(p.TargetRef) || (run != "" && !fileTargetRef(run)) || (kind != "AGENT" && kind != "WORKFLOW") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	target := targetProto(kind, p.TargetRef)
	response, err := server.control.Query.GetRunAttachmentEligibility(r.Context(), &cp.GetRunAttachmentEligibilityRequest{ProjectRef: project, Target: target, RunRef: run})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, ok := runAttachmentEligibilityView(response, project, kind, p.TargetRef, run)
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// runAttachmentEligibilityView checks exact project/target/run identity, safe optional run
// version, workflow pin, digest and timestamp, then requires eligible to match the AVAILABLE
// reason. It does not attach a file or authorize a later mutation.
// runAttachmentEligibilityView проверяет точные project/target/run, безопасную необязательную
// версию запуска, workflow pin, digest и время, затем требует соответствия eligible причине
// AVAILABLE. Файл не прикрепляется и последующая мутация не авторизуется.
func runAttachmentEligibilityView(v *cp.GetRunAttachmentEligibilityResponse, project, kind, ref, run string) (generated.RunAttachmentEligibility, bool) {
	result := generated.RunAttachmentEligibility{}
	if v == nil || v.ProjectRef != project || v.Target == nil || kind == "AGENT" && v.Target.GetAgentRef() != ref || kind == "WORKFLOW" && v.Target.GetWorkflowRef() != ref || v.RunRef != run || v.RunVersion < 0 || v.RunVersion > maximumSafeJSONInteger || (run == "") != (v.RunVersion == 0) || !validManagedDigest(v.Digest) || v.EvaluatedAt == nil || v.EvaluatedAt.CheckValid() != nil || v.WorkflowVersionRef != "" && !fileTargetRef(v.WorkflowVersionRef) || kind == "AGENT" && v.WorkflowVersionRef != "" {
		return result, false
	}
	reason := strings.TrimPrefix(v.Reason.String(), "RUN_ATTACHMENT_ELIGIBILITY_REASON_")
	switch reason {
	case "AVAILABLE", "TARGET_UNAVAILABLE", "RUNTIME_NOT_READY", "AGENT_CAPABILITY_REQUIRED", "SESSION_UNAVAILABLE":
	default:
		return result, false
	}
	if v.Eligible != (reason == "AVAILABLE") {
		return result, false
	}
	result = generated.RunAttachmentEligibility{ProjectRef: project, TargetRef: ref, TargetType: generated.RunAttachmentEligibilityTargetType(kind), RunRef: optionalManagedString(run), RunVersion: v.RunVersion, WorkflowVersionRef: optionalManagedString(v.WorkflowVersionRef), Eligible: v.Eligible, Reason: generated.RunAttachmentEligibilityReason(reason), Digest: v.Digest, EvaluatedAt: v.EvaluatedAt.AsTime()}
	return result, true
}
