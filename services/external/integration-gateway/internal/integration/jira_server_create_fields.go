package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraCreateIntent is the immutable Human Gate input, not an operator-selected JSON payload.
// jiraCreateIntent — неизменяемый ввод Human Gate, а не произвольный payload провайдера.
type jiraCreateIntent struct {
	Summary, Description string
	FieldsJSON           string `json:"fields_json"`
	ReporterSelf         bool   `json:"reporter_self"`
}

// jiraCreateSchema admits only observed primitive/select and Epic Link schemas on the create screen.
// jiraCreateSchema допускает только проверенные primitive/select и Epic Link schemas формы создания.
type jiraCreateSchema struct{ Type, Custom, System, Items string }

// jiraCreateMetadataField retains required/set/option evidence without following provider links.
// jiraCreateMetadataField сохраняет required/set/option evidence без переходов по ссылкам провайдера.
type jiraCreateMetadataField struct {
	FieldID       string `json:"fieldId"`
	Required      *bool
	Schema        jiraCreateSchema
	Operations    []string
	AllowedValues []struct {
		ID       string
		Disabled bool
	}
}

// jiraCreateMetadataPage accepts two separately validated Server pagination dialects, never a mix.
// jiraCreateMetadataPage принимает две отдельно проверяемые Server формы пагинации без смешивания.
type jiraCreateMetadataPage struct {
	StartAt, MaxResults, Total *int
	IsLast                     *bool
	Start, Size                *int
	Last                       *bool
	Values                     []jiraCreateMetadataField
}

// jiraCreateScreen binds the resolved non-subtask type ID to the complete field metadata checked before create.
// jiraCreateScreen связывает найденный ID обычного типа с полной metadata полей, проверяемых перед созданием.
type jiraCreateScreen struct {
	IssueTypeID string
	Fields      map[string]jiraCreateMetadataField
}

// jiraCreateFieldValues resolves explicit aliases under the pinned connection allowlist. It cannot
// overwrite project/type/labels, choose another reporter, or accept an unconfigured custom field.
// jiraCreateFieldValues разрешает явные aliases по закреплённому allowlist подключения. Нельзя
// заменить project/type/labels, выбрать другого reporter либо ненастроенный custom field.
func jiraCreateFieldValues(configuration map[string]string, intent jiraCreateIntent) (map[string]any, map[string]integrationpackage.JiraCreateField, error) {
	allowed, err := integrationpackage.ParseJiraCreateFields(configuration["issue_create_fields"])
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	values, err := integrationpackage.ParseJiraCreateValues(intent.FieldsJSON)
	if err != nil || intent.FieldsJSON != "" && len(allowed) == 0 {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	result := map[string]any{}
	byID := map[string]integrationpackage.JiraCreateField{}
	for _, field := range allowed {
		byID[field.ID] = field
	}
	for alias, value := range values {
		field, exists := allowed[alias]
		valid := exists
		switch field.Type {
		case "STRING":
			_, valid = value.(string)
		case "NUMBER":
			_, valid = value.(json.Number)
		case "BOOLEAN":
			_, valid = value.(bool)
		case "OPTION":
			name, ok := value.(string)
			id := field.Options[name]
			valid, value = ok && id != "", map[string]string{"id": id}
		case "ISSUE_KEY":
			key, ok := value.(string)
			valid = ok && jiraIssueInProject(key, configuration["project_key"])
		default:
			valid = false
		}
		if !valid {
			return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		result[field.ID] = value
	}
	return result, byID, nil
}

// jiraCreateMetadataNext requires contiguous finite pages and stable totals when present. A page
// claiming EOF before the observed total, a changed dialect, or an empty nonterminal page is invalid.
// jiraCreateMetadataNext требует непрерывные конечные страницы и неизменный total при его наличии.
// Преждевременный EOF, смена формы и пустая незавершённая страница недопустимы.
func jiraCreateMetadataNext(page jiraCreateMetadataPage, offset int, dialect *string, total **int) (int, bool, error) {
	start, size, last, kind := page.StartAt, page.MaxResults, page.IsLast, "offset"
	if page.Start != nil || page.Size != nil || page.Last != nil {
		if start != nil || size != nil || last != nil {
			return 0, false, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		start, size, last, kind = page.Start, page.Size, page.Last, "start"
	}
	count := len(page.Values)
	if start == nil || *start != offset || size == nil || *size < 1 || *size > 50 || count > *size || last == nil ||
		*dialect != "" && *dialect != kind || offset+count > 400 || !*last && count == 0 || offset > 0 && (page.Total == nil) != (*total == nil) {
		return 0, false, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if page.Total != nil {
		if *page.Total < 0 || *page.Total > 400 || *total != nil && **total != *page.Total ||
			offset+count > *page.Total || *last != (offset+count == *page.Total) {
			return 0, false, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	} else if *total != nil || kind == "offset" {
		return 0, false, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	*dialect, *total = kind, page.Total
	return offset + count, *last, nil
}

// readJiraCreateMetadata resolves the exact configured non-subtask type in the configured project,
// then exhausts its fixed metadata endpoint and preserves that exact type ID for the single POST.
// readJiraCreateMetadata находит точный настроенный обычный тип в разрешённом проекте и исчерпывает
// только его metadata endpoint, сохраняя точный ID типа для единственного POST.
func (adapter *Adapter) readJiraCreateMetadata(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (*jiraCreateScreen, error) {
	raw, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/project/"+url.PathEscape(configuration["project_key"]), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var project struct {
		ID, Key    string
		IssueTypes []struct {
			ID, Name string
			Subtask  *bool
		}
	}
	if decodeAtlassianRecoveryJSON(raw, &project) != nil || !decimalID(project.ID) || project.Key != configuration["project_key"] || len(project.IssueTypes) == 0 || len(project.IssueTypes) > 256 {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	typeID := ""
	seenTypes := map[string]bool{}
	for _, item := range project.IssueTypes {
		if !decimalID(item.ID) || seenTypes[item.ID] || item.Name == "" || item.Subtask == nil {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seenTypes[item.ID] = true
		if item.Name == configuration["issue_type"] && !*item.Subtask {
			if typeID != "" {
				return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			typeID = item.ID
		}
	}
	if typeID == "" {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	result, dialect, offset := map[string]jiraCreateMetadataField{}, "", 0
	var total *int
	for iteration := 0; iteration < 8; iteration++ {
		raw, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet,
			"/issue/createmeta/"+url.PathEscape(project.Key)+"/issuetypes/"+typeID,
			url.Values{"startAt": {strconv.Itoa(offset)}, "maxResults": {"50"}}, nil, "")
		if err != nil {
			return nil, err
		}
		var page jiraCreateMetadataPage
		if decodeAtlassianRecoveryJSON(raw, &page) != nil || page.Values == nil {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		next, done, err := jiraCreateMetadataNext(page, offset, &dialect, &total)
		if err != nil {
			return nil, err
		}
		for _, field := range page.Values {
			if field.FieldID == "" || len(field.FieldID) > 128 || field.Required == nil || len(field.Operations) > 16 || len(field.AllowedValues) > 1024 {
				return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			if _, exists := result[field.FieldID]; exists {
				return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			result[field.FieldID] = field
		}
		if done {
			return &jiraCreateScreen{IssueTypeID: typeID, Fields: result}, nil
		}
		offset = next
	}
	return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
}

// jiraCreateSchemaMatches narrows provider metadata to the implemented wire shapes. In particular,
// Epic Link is not an arbitrary string field and select is not an arbitrary nested object.
// jiraCreateSchemaMatches сужает metadata до реализованных форм. Epic Link не произвольная строка,
// а select не произвольный вложенный объект.
func jiraCreateSchemaMatches(field integrationpackage.JiraCreateField, metadata jiraCreateMetadataField) bool {
	schema := metadata.Schema
	if schema.Custom == "" {
		return false
	}
	switch field.Type {
	case "STRING":
		return schema.Type == "string" && schema.Custom != "com.pyxis.greenhopper.jira:gh-epic-link"
	case "NUMBER":
		return schema.Type == "number"
	case "BOOLEAN":
		return schema.Type == "boolean"
	case "OPTION":
		return schema.Type == "option" && schema.Custom == "com.atlassian.jira.plugin.system.customfieldtypes:select"
	case "ISSUE_KEY":
		return (schema.Type == "any" || schema.Type == "string") && schema.Custom == "com.pyxis.greenhopper.jira:gh-epic-link"
	}
	return false
}

// prepareJiraCreateFields checks all required create-screen fields and every supplied custom value
// before one POST. Defaults are never silently adopted; reporter_self resolves only authenticated self.
// prepareJiraCreateFields проверяет все обязательные поля и переданные custom values до одного POST.
// Defaults не подставляются скрыто; reporter_self разрешается только в текущую внешнюю identity.
func (adapter *Adapter) prepareJiraCreateFields(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, intent jiraCreateIntent, fields map[string]any) error {
	custom, allowed, err := jiraCreateFieldValues(configuration, intent)
	if err != nil {
		return err
	}
	if !request.PersonalCredentialAdmission {
		permissionRequest := request
		permissionRequest.PersonalCredentialAdmission = true
		permissionRequest.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionJiraIssueCreateV1
		if err := adapter.requireJiraServerWritePermission(ctx, permissionRequest, capability, configuration, ""); err != nil {
			return err
		}
	}
	screen, err := adapter.readJiraCreateMetadata(ctx, request, capability, configuration)
	if err != nil {
		return err
	}
	for id, value := range custom {
		field, exists := screen.Fields[id]
		if !exists || !jiraCreateSchemaMatches(allowed[id], field) {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if allowed[id].Type == "OPTION" {
			wanted := value.(map[string]string)["id"]
			matched, seen := false, map[string]bool{}
			for _, option := range field.AllowedValues {
				if !decimalID(option.ID) || seen[option.ID] {
					return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
				}
				seen[option.ID] = true
				matched = matched || option.ID == wanted && !option.Disabled
			}
			if !matched {
				return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
		}
		fields[id] = value
	}
	if intent.ReporterSelf {
		raw, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/myself", nil, nil, "")
		if err != nil {
			return err
		}
		var user struct {
			Key, Name string
			Active    *bool
		}
		if decodeAtlassianRecoveryJSON(raw, &user) != nil || !validAtlassianRecoveryKey(user.Key) || !validAtlassianRecoveryKey(user.Name) || user.Active == nil || !*user.Active {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		fields["reporter"] = map[string]string{"name": user.Name}
	}
	for id, field := range screen.Fields {
		_, supplied := fields[id]
		if *field.Required && !supplied {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if supplied {
			expectedType := map[string]string{"project": "project", "issuetype": "issuetype", "summary": "string", "description": "string", "labels": "array", "reporter": "user"}[id]
			if expectedType != "" && (field.Schema.Type != expectedType || field.Schema.System != id || id == "labels" && field.Schema.Items != "string") {
				return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			if id == "issuetype" && field.Schema.Type == "issuetype" && field.Schema.System == "issuetype" {
				continue
			}
			set := false
			for _, operation := range field.Operations {
				set = set || operation == "set"
			}
			if !set {
				return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
		}
	}
	for id := range fields {
		if _, exists := screen.Fields[id]; !exists {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	for id, value := range custom {
		if allowed[id].Type == "ISSUE_KEY" {
			if _, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.(string)); err != nil {
				return err
			}
		}
	}
	fields["issuetype"] = map[string]string{"id": screen.IssueTypeID}
	return nil
}

// jiraCreateReadFields requests custom evidence only from the pinned allowlist, never caller paths.
// jiraCreateReadFields запрашивает custom evidence только по закреплённому allowlist, без caller paths.
func jiraCreateReadFields(configuration map[string]string) string {
	fields := []string{"project", "summary", "description", "labels", "creator", "issuetype"}
	if configuration["issue_create_fields"] != "" {
		allowed, err := integrationpackage.ParseJiraCreateFields(configuration["issue_create_fields"])
		if err == nil {
			for _, field := range allowed {
				fields = append(fields, field.ID)
			}
		}
	}
	fields = append(fields, "reporter")
	sort.Strings(fields)
	return strings.Join(fields, ",")
}
