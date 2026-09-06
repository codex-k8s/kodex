package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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

func fileTargetPage(size *int, token *string) bool {
	return (size == nil || *size >= 1 && *size <= 100) && (token == nil || len(*token) <= 2048 && utf8.ValidString(*token))
}

func fileTargetRef(ref string) bool {
	return len(ref) >= 8 && len(ref) <= 128 && opaqueHTTPReference.MatchString(ref)
}

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

func fileBindingReason(reason cp.ArtifactBindingTargetReason) string {
	switch reason {
	case cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AVAILABLE, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_ALREADY_BOUND, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_NOT_BOUND, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AGENT_CAPABILITY_REQUIRED, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AGENT_ARCHIVED, cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_ARTIFACT_UNAVAILABLE:
		return strings.TrimPrefix(reason.String(), "ARTIFACT_BINDING_TARGET_REASON_")
	}
	return ""
}

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
