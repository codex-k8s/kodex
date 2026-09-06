package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) listTemplateVariables(w http.ResponseWriter, r *http.Request, project, agent, revision, query string, size *int, token *string) {
	if !validHTTPPage(size, token) || !validSearchText(query, 0, 200) || (agent != "" && !opaqueHTTPReference.MatchString(agent)) || (revision != "" && !opaqueHTTPReference.MatchString(revision)) {
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
	response, err := server.control.Query.ListTemplateVariables(r.Context(), &cp.ListTemplateVariablesRequest{ProjectRef: project, AgentRef: agent, RuntimeRevisionRef: revision, Query: query, Page: paging})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	server.writeTemplateVariablePage(w, response, paging.GetPageSize())
}

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

// Каталог CP использует отдельный закрытый словарь типов, включая descriptor.
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
