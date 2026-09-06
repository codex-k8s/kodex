package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func promptOptional[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func promptText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func promptContextInput(value *generated.PromptPreviewContext) (*cp.PromptPreviewContext, bool) {
	if value == nil {
		return nil, true
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 128<<10 {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false
	}
	for _, key := range []string{"expectedAgentVersion", "expectedWorkflowVersion"} {
		if encoded, exists := fields[key]; exists {
			var version int64
			if json.Unmarshal(encoded, &version) != nil || !validManagedVersion(version) {
				return nil, false
			}
		}
	}
	result := &cp.PromptPreviewContext{}
	if (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, result) != nil {
		return nil, false
	}
	for _, ref := range []string{result.AgentRef, result.WorkflowRevisionRef, result.AttachmentSetRef} {
		if ref != "" && !fileTargetRef(ref) {
			return nil, false
		}
	}
	if result.ExpectedAgentVersion < 0 || result.ExpectedAgentVersion > maximumSafeJSONInteger || result.ExpectedWorkflowVersion < 0 || result.ExpectedWorkflowVersion > maximumSafeJSONInteger || !promptText(result.WorkflowStageKey, 128) || !promptText(result.Task, 64<<10) {
		return nil, false
	}
	if result.Input != nil {
		values, err := json.Marshal(result.Input.AsMap())
		if err != nil || len(values) > 64<<10 {
			return nil, false
		}
	}
	return result, true
}

func validPromptSelection(kind, ref, expected string, context *cp.PromptPreviewContext) bool {
	if expected != "" && !validManagedDigest(expected) {
		return false
	}
	switch kind {
	case "", "SYNTHETIC":
		return ref == "" && (context == nil || proto.Size(context) == 0)
	case "RUN", "SESSION":
		return fileTargetRef(ref) && (context == nil || proto.Size(context) == 0)
	case "AGENT":
		return fileTargetRef(ref) && (context.GetAgentRef() == "" || context.GetAgentRef() == ref) && context.GetWorkflowRevisionRef() == "" && context.GetWorkflowStageKey() == "" && context.GetExpectedWorkflowVersion() == 0
	case "WORKFLOW_STAGE":
		return fileTargetRef(ref) && context.GetWorkflowStageKey() != ""
	case "SESSION_CONTINUATION":
		return fileTargetRef(ref)
	default:
		return false
	}
}

func validPromptContextReadback(kind, ref, expected string, pin *cp.PromptContextPin) bool {
	if expected != "" && pin.GetDigest() != expected {
		return false
	}
	switch kind {
	case "AGENT":
		return pin != nil && pin.AgentRef == ref
	case "WORKFLOW_STAGE":
		return pin != nil && pin.WorkflowRef == ref
	case "SESSION_CONTINUATION":
		return pin != nil && fileTargetRef(pin.PreviousRuntimeRevisionRef)
	default:
		return true
	}
}

func validPromptSelectedPin(input *cp.PromptPreviewContext, pin *cp.PromptContextPin) bool {
	return (input.GetAgentRef() == "" || input.GetAgentRef() == pin.GetAgentRef()) &&
		(input.GetExpectedAgentVersion() == 0 || input.GetExpectedAgentVersion() == pin.GetAgentVersion()) &&
		(input.GetExpectedWorkflowVersion() == 0 || input.GetExpectedWorkflowVersion() == pin.GetWorkflowVersion()) &&
		(input.GetWorkflowRevisionRef() == "" || input.GetWorkflowRevisionRef() == pin.GetWorkflowRevisionRef()) &&
		(input.GetWorkflowStageKey() == "" || input.GetWorkflowStageKey() == pin.GetWorkflowStageKey()) &&
		(input.GetAttachmentSetRef() == "" || input.GetAttachmentSetRef() == pin.GetAttachmentSetRef())
}

func promptScopeInput(v *generated.PromptTemplateScopeInput) (*cp.PromptTemplateScopeInput, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.PromptTemplateScopeInput{TargetKind: string(v.TargetKind), TargetRef: v.TargetRef, AgentRef: stringValue(v.AgentRef), WorkflowRevisionRef: stringValue(v.WorkflowRevisionRef), WorkflowStageKey: stringValue(v.WorkflowStageKey), ExpectedContextDigest: stringValue(v.ExpectedContextDigest)}
	switch v.TemplateKind {
	case "INSTRUCTIONS":
		result.TemplateKind = cp.PromptTemplateKind_PROMPT_TEMPLATE_KIND_INSTRUCTIONS
	case "CONTINUATION":
		result.TemplateKind = cp.PromptTemplateKind_PROMPT_TEMPLATE_KIND_CONTINUATION
	default:
		return nil, false
	}
	if result.TargetKind != "AGENT" && result.TargetKind != "WORKFLOW_STAGE" || !validPromptSelection(result.TargetKind, result.TargetRef, result.ExpectedContextDigest, &cp.PromptPreviewContext{AgentRef: result.AgentRef, WorkflowRevisionRef: result.WorkflowRevisionRef, WorkflowStageKey: result.WorkflowStageKey}) {
		return nil, false
	}
	for _, ref := range []string{result.AgentRef, result.WorkflowRevisionRef} {
		if ref != "" && !fileTargetRef(ref) {
			return nil, false
		}
	}
	if !promptText(result.WorkflowStageKey, 128) {
		return nil, false
	}
	return result, true
}

func promptScopeView(v *cp.PromptTemplateScope) (*generated.PromptTemplateScope, bool) {
	if v == nil {
		return nil, true
	}
	pin, ok := promptContextPinView(v.ContextPin)
	if !ok || pin == nil || (v.TargetKind != "AGENT" && v.TargetKind != "WORKFLOW_STAGE") || !fileTargetRef(v.TargetRef) {
		return nil, false
	}
	if v.TargetKind == "AGENT" && v.ContextPin.AgentRef != v.TargetRef || v.TargetKind == "WORKFLOW_STAGE" && v.ContextPin.WorkflowRef != v.TargetRef {
		return nil, false
	}
	kind := strings.TrimPrefix(v.TemplateKind.String(), "PROMPT_TEMPLATE_KIND_")
	if kind != "INSTRUCTIONS" && kind != "CONTINUATION" {
		return nil, false
	}
	raw, err := json.Marshal(map[string]any{"targetKind": v.TargetKind, "targetRef": v.TargetRef, "templateKind": kind, "contextPin": pin})
	if err != nil {
		return nil, false
	}
	var result generated.PromptTemplateScope
	if json.Unmarshal(raw, &result) != nil {
		return nil, false
	}
	return &result, true
}

func validPromptScopeReceipt(input *cp.PromptTemplateScopeInput, output *cp.PromptTemplateScope) bool {
	if input == nil {
		return true
	}
	return output != nil && input.TargetKind == output.TargetKind && input.TargetRef == output.TargetRef && input.TemplateKind == output.TemplateKind &&
		(input.AgentRef == "" || input.AgentRef == output.GetContextPin().GetAgentRef()) &&
		(input.WorkflowRevisionRef == "" || input.WorkflowRevisionRef == output.GetContextPin().GetWorkflowRevisionRef()) &&
		(input.WorkflowStageKey == "" || input.WorkflowStageKey == output.GetContextPin().GetWorkflowStageKey()) &&
		(input.ExpectedContextDigest == "" || input.ExpectedContextDigest == output.GetContextPin().GetDigest())
}

func (server *Server) QueryPromptTemplateVariables(w http.ResponseWriter, r *http.Request, _ generated.QueryPromptTemplateVariablesParams) {
	body, ok := decodeJSON[generated.PromptVariableCatalogInput](w, r)
	if !ok {
		return
	}
	context, ok := promptContextInput(body.Context)
	if !ok || !validPromptSelection(string(body.TargetKind), body.TargetRef, stringValue(body.ExpectedContextDigest), context) || !validHTTPPage(body.PageSize, body.PageToken) || !validSearchText(stringValue(body.Query), 0, 200) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	project := stringValue(body.ProjectRef)
	if project != "" {
		r, ok = withProjectReference(w, r, project)
		if !ok {
			return
		}
	}
	paging := page(body.PageSize, body.PageToken)
	response, err := server.control.Query.ListTemplateVariables(r.Context(), &cp.ListTemplateVariablesRequest{ProjectRef: project, Query: stringValue(body.Query), Page: paging, TargetKind: string(body.TargetKind), TargetRef: body.TargetRef, Context: context, ExpectedContextDigest: stringValue(body.ExpectedContextDigest)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetContextPin() == nil || !validPromptContextReadback(string(body.TargetKind), body.TargetRef, stringValue(body.ExpectedContextDigest), response.GetContextPin()) || !validPromptSelectedPin(context, response.GetContextPin()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	server.writeTemplateVariablePage(w, response, paging.GetPageSize())
}
