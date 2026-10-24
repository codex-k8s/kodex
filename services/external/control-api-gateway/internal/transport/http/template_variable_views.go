package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// listTemplateVariables validates optional catalog/source/agent/revision selectors, attaches
// project routing when present and calls the owner query under the request context. The shared
// writer validates the returned variable page.
// listTemplateVariables проверяет необязательные catalog/source/agent/revision selectors,
// добавляет проектную маршрутизацию при наличии и вызывает запрос владельца в контексте запроса.
// Общий writer проверяет возвращённую страницу переменных.
func (server *Server) listTemplateVariables(w http.ResponseWriter, r *http.Request, project, agent, revision, query, source string, size *int, token *string) {
	if !validHTTPPage(size, token) || !validSearchText(query, 0, 200) || !validTemplateVariableSource(source) || (agent != "" && !opaqueHTTPReference.MatchString(agent)) || (revision != "" && !opaqueHTTPReference.MatchString(revision)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if project != "" {
		var ok bool
		r, ok = withProjectReference(w, r, project)
		if !ok {
			return
		}
	}
	paging := page(size, token)
	response, err := server.control.Query.ListTemplateVariables(r.Context(), &cp.ListTemplateVariablesRequest{ProjectRef: project, AgentRef: agent, RuntimeRevisionRef: revision, Query: query, Source: source, Page: paging})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	server.writeTemplateVariablePage(w, response, paging.GetPageSize())
}

// validTemplateVariableSource allows an omitted source filter or an exact generated public source
// discriminator, rejecting arbitrary source labels without normalization.
// validTemplateVariableSource допускает отсутствие фильтра источника либо точный generated
// публичный вид источника, отклоняя произвольные метки без нормализации.
func validTemplateVariableSource(source string) bool {
	return source == "" || generated.TemplateVariableSourceQuery(source).Valid()
}

// writeTemplateVariablePage checks owner totals, bounded pagination, distinct variable names and
// optional context pin before emitting public JSON. Descriptions are localized after validation; a
// malformed item rejects the complete page rather than being filtered out.
// writeTemplateVariablePage проверяет total владельца, ограниченную пагинацию, уникальность имён
// переменных и необязательный context pin до публичного JSON. Описания переводятся после проверки;
// неверный элемент отклоняет всю страницу без фильтрации.
func (server *Server) writeTemplateVariablePage(w http.ResponseWriter, response *cp.ListTemplateVariablesResponse, size int32) {
	next := response.GetPage().GetNextPageToken()
	if response == nil || response.GetTotal() < int64(len(response.GetVariables())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetVariables()) > int(size) || len(next) > 512 || !utf8.ValidString(next) {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	result := generated.TemplateVariablePage{Items: make([]generated.TemplateVariable, 0, len(response.GetVariables())), Total: response.GetTotal(), NextPageToken: optionalManagedString(next)}
	seen := map[string]bool{}
	for _, variable := range response.GetVariables() {
		item, ok := templateVariableView(variable)
		if !ok || seen[item.Name] {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		seen[item.Name] = true
		localizeTemplateVariable(w, &item)
		result.Items = append(result.Items, item)
	}
	pin, ok := promptContextPinView(response.GetContextPin())
	if !ok {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	output := map[string]any{"items": result.Items, "total": result.Total}
	if result.NextPageToken != nil {
		output["nextPageToken"] = *result.NextPageToken
	}
	if pin != nil {
		output["contextPin"] = pin
	}
	writeJSON(w, http.StatusOK, output)
}

// localizeTemplateVariable replaces only i18n-prefixed variable and item-field descriptions using
// the response localizer. Without a localizer it leaves the item unchanged and never translates
// example values or identifiers.
// localizeTemplateVariable заменяет только описания переменной и полей элементов с префиксом i18n
// через localizer ответа. Без localizer элемент не меняется; примеры значений и identifiers не
// переводятся.
func localizeTemplateVariable(w http.ResponseWriter, item *generated.TemplateVariable) {
	localizer, ok := w.(interface{ Localize(string) string })
	if !ok {
		return
	}
	if strings.HasPrefix(item.Description, "i18n:") {
		item.Description = localizer.Localize(strings.TrimPrefix(item.Description, "i18n:"))
	}
	for i := range item.ItemFields {
		if strings.HasPrefix(item.ItemFields[i].Description, "i18n:") {
			item.ItemFields[i].Description = localizer.Localize(strings.TrimPrefix(item.ItemFields[i].Description, "i18n:"))
		}
	}
}

// templateVariableView converts known variable type/source/reason values, checks availability
// consistency and bounded item-field definitions, preserving optional item type and range example.
// It describes metadata without materializing variable values.
// templateVariableView преобразует известные type/source/reason переменной, проверяет
// согласованность available и ограниченные определения полей, сохраняя необязательные item type и
// range example. Описываются метаданные без материализации значений переменных.
func templateVariableView(v *cp.TemplateVariable) (generated.TemplateVariable, bool) {
	if v == nil {
		return generated.TemplateVariable{}, false
	}
	item := generated.TemplateVariable{
		Name: v.GetName(), Description: v.GetDescription(), Example: v.GetExample(), Collection: v.GetCollection(), Available: v.GetAvailable(),
		ValueType: generated.TemplateVariableValueType(templateVariableType(v.GetValueType())), Source: generated.TemplateVariableSource(v.GetSource()),
		Reason:       generated.TemplateVariableAvailabilityReason(strings.TrimPrefix(v.GetReason().String(), "TEMPLATE_VARIABLE_AVAILABILITY_REASON_")),
		RangeExample: optionalManagedString(v.GetRangeExample()), ItemFields: make([]generated.TemplateVariableField, 0, len(v.GetItemFields())),
	}
	if !item.Reason.Valid() || item.Available != (item.Reason == "AVAILABLE") || !item.ValueType.Valid() || !item.Source.Valid() || !validSearchText(item.Name, 1, 160) || len(v.GetItemFields()) > 32 {
		return item, false
	}
	if v.GetItemValueType() != "" {
		value := generated.TemplateVariableItemValueType(templateVariableType(v.GetItemValueType()))
		if !value.Valid() {
			return item, false
		}
		item.ItemValueType = &value
	}
	for _, field := range v.GetItemFields() {
		if field == nil {
			return item, false
		}
		value := generated.TemplateVariableField{Name: field.GetName(), Description: field.GetDescription(), ValueType: generated.TemplateVariableFieldValueType(templateVariableType(field.GetValueType()))}
		if !value.ValueType.Valid() {
			return item, false
		}
		item.ItemFields = append(item.ItemFields, value)
	}
	return item, true
}

// templateVariableType maps the owner catalog vocabulary to its closed public type names,
// including opaque refs and file/tool/integration descriptors. Unknown types return empty text for
// the caller to reject.
// templateVariableType сопоставляет словарю каталога владельца закрытые публичные имена типов,
// включая opaque refs и file/tool/integration descriptors. Неизвестный тип даёт пустой текст для
// отказа вызывающего.
func templateVariableType(value string) string {
	switch value {
	case "string":
		return "STRING"
	case "reference":
		return "OPAQUE_REF"
	case "integer":
		return "INTEGER"
	case "object":
		return "OBJECT"
	case "collection":
		return "COLLECTION"
	case "file_descriptor":
		return "FILE_DESCRIPTOR"
	case "tool_descriptor":
		return "TOOL_DESCRIPTOR"
	case "integration_descriptor":
		return "INTEGRATION_DESCRIPTOR"
	default:
		return ""
	}
}
