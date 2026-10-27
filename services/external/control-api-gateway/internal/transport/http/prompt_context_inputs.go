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

// promptOptional converts an optional string-like API value to a plain string, preserving a
// supplied value and representing absence as empty text without assigning a default.
// promptOptional преобразует необязательное строкоподобное API значение в строку, сохраняя
// переданное значение и представляя отсутствие пустым текстом без назначения default.
func promptOptional[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// promptText checks a byte limit, UTF-8 encoding and absence of NUL while allowing empty text.
// Line breaks and other valid characters remain unchanged.
// promptText проверяет байтовый лимит, кодировку UTF-8 и отсутствие NUL, допуская пустой текст.
// Переводы строк и другие корректные символы не меняются.
func promptText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// promptContextInput converts optional public preview context through bounded JSON to the strict
// protobuf shape, checking explicitly present positive version pins, locator/text limits and
// bounded structured input. It supplies context selectors rather than authority.
// promptContextInput преобразует необязательный публичный preview context через ограниченный JSON
// в строгую protobuf форму, проверяя явно заданные положительные pins версий, лимиты
// адресации/текста и структурного входа. Это selectors контекста, а не полномочия.
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

// validPromptTemplateInput checks the shared template byte/UTF-8/NUL bound before the
// target-specific locator, digest and converted-context rules. Callers first reject failed
// context conversion; selectors carry no authority, and this predicate neither materializes
// a prompt nor changes the context. Empty template text remains accepted by promptText.
// validPromptTemplateInput проверяет общий лимит байтов/UTF-8/NUL шаблона до правил адресации,
// digest и преобразованного контекста для выбранной цели. Callers сначала отвергают отказ
// преобразования контекста; selectors не несут полномочий, а предикат не материализует prompt
// и не меняет контекст. Пустой шаблон остаётся допустимым по правилам promptText.
func validPromptTemplateInput(template, kind, ref, expected string, context *cp.PromptPreviewContext) bool {
	return promptText(template, 256<<10) && validPromptSelection(kind, ref, expected, context)
}

// validPromptSelection checks target-kind-specific locator/context combinations and an optional
// expected digest. Synthetic/run/session selectors forbid nonempty preview context; agent and
// workflow-stage selectors have distinct compatibility rules.
// validPromptSelection проверяет сочетания адресации и контекста по виду цели и необязательный
// ожидаемый digest. Synthetic/run/session запрещают непустой preview context; у agent и
// workflow-stage отдельные правила совместимости.
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

// validPromptContextReadback matches an expected digest and target-specific agent/workflow
// identity or previous runtime revision in the owner pin. It does not establish eligibility or
// validate every pin field; projection validation is separate.
// validPromptContextReadback сопоставляет ожидаемый digest и agent/workflow identity либо
// предыдущую runtime revision по виду цели в pin владельца. Допуск и все поля pin здесь не
// проверяются; валидация проекции выполняется отдельно.
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

// validPromptSelectedPin requires each explicitly requested context coordinate and version to
// equal the owner pin. Empty/zero selectors impose no additional equality condition and never
// authorize access.
// validPromptSelectedPin требует совпадения каждой явно запрошенной координаты и версии контекста
// с pin владельца. Пустые и нулевые selectors не добавляют условий равенства и не авторизуют
// доступ.
func validPromptSelectedPin(input *cp.PromptPreviewContext, pin *cp.PromptContextPin) bool {
	return (input.GetAgentRef() == "" || input.GetAgentRef() == pin.GetAgentRef()) &&
		(input.GetExpectedAgentVersion() == 0 || input.GetExpectedAgentVersion() == pin.GetAgentVersion()) &&
		(input.GetExpectedWorkflowVersion() == 0 || input.GetExpectedWorkflowVersion() == pin.GetWorkflowVersion()) &&
		(input.GetWorkflowRevisionRef() == "" || input.GetWorkflowRevisionRef() == pin.GetWorkflowRevisionRef()) &&
		(input.GetWorkflowStageKey() == "" || input.GetWorkflowStageKey() == pin.GetWorkflowStageKey()) &&
		(input.GetAttachmentSetRef() == "" || input.GetAttachmentSetRef() == pin.GetAttachmentSetRef())
}

// promptScopeInput maps optional template scope to INSTRUCTIONS/CONTINUATION protobuf input and
// permits only AGENT or WORKFLOW_STAGE with compatible bounded coordinates. Resource lookup and
// expected-digest authority remain owner responsibilities.
// promptScopeInput преобразует необязательную область шаблона во вход protobuf
// INSTRUCTIONS/CONTINUATION и допускает только AGENT или WORKFLOW_STAGE с совместимыми
// ограниченными координатами. Поиск ресурса и проверка ожидаемого digest остаются у владельца.
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

// promptScopeView validates the optional owner template scope, requires a consistent context pin
// and known template/target kinds, then converts the public shape without exposing additional
// internal fields.
// promptScopeView проверяет необязательную область шаблона владельца, требует согласованный
// context pin и известные виды шаблона и цели, затем преобразует публичную форму без
// дополнительных внутренних полей.
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

// validPromptScopeReceipt checks that a nonempty requested scope is reproduced by the owner
// receipt, including supplied agent/revision/stage/digest constraints. An absent input scope
// imposes no scope-match requirement.
// validPromptScopeReceipt проверяет воспроизведение запрошенной области в receipt владельца,
// включая заданные agent/revision/stage/digest. Отсутствие входной области не добавляет требования
// её совпадения.
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

// QueryPromptTemplateVariables validates target, context and catalog controls, binds optional
// project routing and queries owner variables. It requires a matching returned context pin before
// the shared page writer emits data; selectors alone do not grant context access.
// QueryPromptTemplateVariables проверяет цель, контекст и параметры каталога, связывает
// необязательный проект маршрутизации и запрашивает переменные владельца. До выдачи общим writer
// требуется совпадение context pin; сами selectors доступа к контексту не дают.
func (server *Server) QueryPromptTemplateVariables(w http.ResponseWriter, r *http.Request, _ generated.QueryPromptTemplateVariablesParams) {
	body, ok := decodeJSON[generated.PromptVariableCatalogInput](w, r)
	if !ok {
		return
	}
	context, ok := promptContextInput(body.Context)
	source := stringValue(body.Source)
	if !ok || !validPromptSelection(string(body.TargetKind), body.TargetRef, stringValue(body.ExpectedContextDigest), context) || !validFilteredHTTPPage(body.PageSize, body.PageToken, body.Query) || !validTemplateVariableSource(source) {
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
	response, err := server.control.Query.ListTemplateVariables(r.Context(), &cp.ListTemplateVariablesRequest{ProjectRef: project, Query: stringValue(body.Query), Source: source, Page: paging, TargetKind: string(body.TargetKind), TargetRef: body.TargetRef, Context: context, ExpectedContextDigest: stringValue(body.ExpectedContextDigest)})
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
