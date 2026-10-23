package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraCatalogInput decodes the catalog's issue-related identities, pagination and attachment fields; package validation supplies the operation-specific schema before execution.
// jiraCatalogInput декодирует идентификаторы ресурсов задачи, параметры страниц и поля вложения каталога; проверка пакета задаёт схему конкретной операции до выполнения.
type jiraCatalogInput struct {
	IssueKey     string `json:"issue_key"`
	AccountID    string `json:"account_id"`
	Query        string `json:"query"`
	TransitionID string `json:"transition_id"`
	CommentID    string `json:"comment_id"`
	LinkID       string `json:"link_id"`
	AttachmentID string `json:"attachment_id"`
	Body         string `json:"body"`
	FileName     string `json:"file_name"`
	MediaType    string `json:"media_type"`
	Content      string `json:"content_base64"`
	Limit        int    `json:"limit"`
	Cursor       int    `json:"cursor"`
}

// jiraUserView selects account identity, display name, activity and type from the project-assignable user response, omitting the provider's wider user profile.
// jiraUserView выбирает идентификатор аккаунта, отображаемое имя, активность и тип из ответа о назначаемых пользователях проекта, не включая полный профиль провайдера.
type jiraUserView struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Active      bool   `json:"active"`
	AccountType string `json:"accountType"`
}

// jiraTransitionView decodes transition identity, target state and optional field requirements used by the bounded transition-list result.
// jiraTransitionView декодирует идентификатор перехода, целевое состояние и необязательные требования к полям для ограниченного списка переходов.
type jiraTransitionView struct {
	ID     string                         `json:"id"`
	Name   string                         `json:"name"`
	Fields map[string]jiraTransitionField `json:"fields,omitempty"`
	To     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"to"`
}

// jiraTransitionField retains the provider's required-field flag, schema descriptors and supported operations as transition metadata, not as independently executable authority.
// jiraTransitionField хранит обязательность поля, описатели схемы и поддерживаемые операции как сведения о переходе, а не самостоятельные полномочия выполнения.
type jiraTransitionField struct {
	Required bool   `json:"required"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Schema   struct {
		Type   string `json:"type"`
		Items  string `json:"items,omitempty"`
		System string `json:"system,omitempty"`
		Custom string `json:"custom,omitempty"`
	} `json:"schema"`
	Operations []string `json:"operations,omitempty"`
}

// jiraCommentView keeps the comment ID and raw JSON body so the adapter can verify identity and project a compact document rather than decode arbitrary provider fields.
// jiraCommentView сохраняет ID комментария и исходное JSON-тело, позволяя проверить идентификатор и выдать компактный документ без разбора произвольных полей провайдера.
type jiraCommentView struct {
	ID   string          `json:"id"`
	Body json.RawMessage `json:"body"`
}

// jiraAttachmentView records attachment identity, filename, declared byte size and media type for parent-bound listing, upload readback and bounded download checks.
// jiraAttachmentView хранит идентификатор вложения, имя файла, объявленный размер и тип для списка в границах родителя, сверки загрузки и ограниченного скачивания.
type jiraAttachmentView struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	MediaType string `json:"mimeType"`
}

// jiraLinkView decodes an issue link and its inward/outward endpoints so related-resource operations can exclude links outside the configured project.
// jiraLinkView декодирует связь задач и её входящий и исходящий концы, позволяя исключать связи за пределами заданного проекта.
type jiraLinkView struct {
	ID   string `json:"id"`
	Type struct {
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	Inward struct {
		Key string `json:"key"`
	} `json:"inwardIssue"`
	Outward struct {
		Key string `json:"key"`
	} `json:"outwardIssue"`
}

// catalogListResult encodes a non-null item array with its count and optional numeric continuation cursor; callers own item eligibility, page bounds and cursor semantics.
// catalogListResult кодирует ненулевой массив элементов, их число и необязательный числовой курсор продолжения; допустимость элементов, размер страницы и смысл курсора определяет вызывающий код.
func catalogListResult[T any](request Request, items []T, next int) (Result, error) {
	if items == nil {
		items = []T{}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, request.Operation+":"+request.EffectKey, struct {
		Items string `json:"items"`
		Count int    `json:"count"`
		Next  int    `json:"next_cursor,omitempty"`
	}{string(encoded), len(items), next})
}

// executeJiraCatalog enforces project-local issue keys and numeric related-resource IDs, then dispatches scoped users, transitions, comments, links and attachments using the configured authentication and effect key.
// executeJiraCatalog проверяет ключи задач внутри проекта и числовые ID связанных ресурсов, затем направляет операции пользователей, переходов, комментариев, связей и вложений с заданной авторизацией и ключом эффекта.
func (adapter *Adapter) executeJiraCatalog(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	var in jiraCatalogInput
	if json.Unmarshal(raw, &in) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	project := configuration["project_key"]
	if !strings.HasPrefix(request.Operation, "jira.project.user.") && !jiraIssueInProject(in.IssueKey, project) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	for _, id := range []string{in.TransitionID, in.CommentID, in.LinkID, in.AttachmentID} {
		if id != "" && !decimalID(id) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	call := providerCall{BaseURL: configuration["base_url"], AuthScheme: configuration["auth_scheme"], Username: configuration["username"], Credential: request.Credential, Capability: capability, Method: http.MethodGet}
	issuePath := "/rest/api/3/issue/" + url.PathEscape(in.IssueKey)
	reference := request.Operation + ":" + request.EffectKey
	switch request.Operation {
	case "jira.project.user.search", "jira.project.user.read":
		call.Path = "/rest/api/3/user/assignable/search"
		call.Query = url.Values{"project": {project}, "startAt": {strconv.Itoa(in.Cursor)}, "maxResults": {strconv.Itoa(in.Limit)}}
		if request.Operation == "jira.project.user.read" {
			call.Query.Set("accountId", in.AccountID)
		} else if in.Query != "" {
			call.Query.Set("query", in.Query)
		}
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		var users []jiraUserView
		if decodeProviderJSON(body, &users) != nil || len(users) > in.Limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for _, user := range users {
			if user.AccountID == "" {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		}
		if request.Operation == "jira.project.user.read" {
			if len(users) != 1 || users[0].AccountID != in.AccountID {
				return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
			user := users[0]
			return providerResult(request, reference, struct {
				AccountID   string `json:"account_id"`
				DisplayName string `json:"display_name"`
				Active      bool   `json:"active"`
				AccountType string `json:"account_type"`
			}{user.AccountID, user.DisplayName, user.Active, user.AccountType})
		}
		next := 0
		if len(users) == in.Limit && in.Cursor+len(users) <= 10000 {
			next = in.Cursor + len(users)
		}
		return catalogListResult(request, users, next)
	case "jira.issue.transition.list", "jira.issue.transition.apply":
		call.Path = issuePath + "/transitions"
		if request.Operation == "jira.issue.transition.list" {
			call.Query = url.Values{"expand": {"transitions.fields"}}
		}
		if request.Operation == "jira.issue.transition.apply" {
			call.Method = http.MethodPost
			call.EffectKey = request.EffectKey
			call.Body = struct {
				Transition struct {
					ID string `json:"id"`
				} `json:"transition"`
			}{Transition: struct {
				ID string `json:"id"`
			}{in.TransitionID}}
		}
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		if call.Method == http.MethodPost {
			return providerResult(request, reference, struct {
				Accepted bool `json:"accepted"`
			}{true})
		}
		var result struct {
			Transitions []jiraTransitionView `json:"transitions"`
		}
		if decodeProviderJSON(body, &result) != nil || len(result.Transitions) > 100 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for _, transition := range result.Transitions {
			if !decimalID(transition.ID) || len(transition.Fields) > 100 {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		}
		return catalogListResult(request, result.Transitions, 0)
	case "jira.issue.comment.list":
		call.Path = issuePath + "/comment"
		call.Query = url.Values{"startAt": {strconv.Itoa(in.Cursor)}, "maxResults": {strconv.Itoa(in.Limit)}}
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		var result struct {
			Comments []jiraCommentView `json:"comments"`
			StartAt  int               `json:"startAt"`
			Total    int               `json:"total"`
		}
		if decodeProviderJSON(body, &result) != nil || len(result.Comments) > in.Limit || result.StartAt != in.Cursor || result.Total < in.Cursor+len(result.Comments) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for _, comment := range result.Comments {
			if !decimalID(comment.ID) {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		}
		next := 0
		if len(result.Comments) > 0 && in.Cursor+len(result.Comments) < result.Total && in.Cursor+len(result.Comments) <= 10000 {
			next = in.Cursor + len(result.Comments)
		}
		return catalogListResult(request, result.Comments, next)
	case "jira.issue.comment.read", "jira.issue.comment.update", "jira.issue.comment.delete":
		call.Path = issuePath + "/comment/" + in.CommentID
		if request.Operation == "jira.issue.comment.update" {
			call.Method = http.MethodPut
			call.Body = struct {
				Body any `json:"body"`
			}{jiraADF(in.Body)}
		}
		if request.Operation == "jira.issue.comment.delete" {
			call.Method = http.MethodDelete
		}
		if call.Method != http.MethodGet {
			call.EffectKey = request.EffectKey
		}
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		if call.Method == http.MethodDelete {
			return providerResult(request, reference, struct {
				Accepted bool `json:"accepted"`
			}{true})
		}
		var comment jiraCommentView
		if decodeProviderJSON(body, &comment) != nil || comment.ID != in.CommentID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, reference, struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		}{comment.ID, compactJSON(comment.Body)})
	case "jira.issue.link.list", "jira.issue.link.read", "jira.issue.link.delete", "jira.attachment.list", "jira.attachment.read", "jira.attachment.upload", "jira.attachment.delete":
		return adapter.jiraRelatedResource(ctx, request, call, in, project, issuePath)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

// decimalID accepts a nonempty sequence of decimal digits containing at least one nonzero digit; it validates syntax without converting to a bounded integer.
// decimalID принимает непустую последовательность десятичных цифр хотя бы с одной ненулевой; проверяет форму без преобразования в целое ограниченной разрядности.
func decimalID(value string) bool {
	return value != "" && strings.Trim(value, "0123456789") == "" && strings.Trim(value, "0") != ""
}

// jiraRelatedResource binds links and attachments to the exact issue before reads or deletes, filters cross-project links, and bounds upload/download bytes; uploads verify the single returned attachment's filename and ID.
// jiraRelatedResource привязывает связи и вложения к точной задаче до чтения или удаления, исключает межпроектные связи и ограничивает байты загрузки и скачивания; загрузка проверяет имя и ID единственного возвращённого вложения.
func (adapter *Adapter) jiraRelatedResource(ctx context.Context, request Request, call providerCall, in jiraCatalogInput, project, issuePath string) (Result, error) {
	reference := request.Operation + ":" + request.EffectKey
	if request.Operation == "jira.attachment.upload" {
		body, contentType, err := boundedMultipart(in.FileName, in.MediaType, in.Content)
		if err != nil {
			return Result{}, err
		}
		call.Path = issuePath + "/attachments"
		call.Method = http.MethodPost
		call.MultipartBody = body
		call.MultipartType = contentType
		call.EffectKey = request.EffectKey
		response, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		var attachments []jiraAttachmentView
		if decodeProviderJSON(response, &attachments) != nil || len(attachments) != 1 || !decimalID(attachments[0].ID) || attachments[0].Filename != in.FileName {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return jiraAttachmentResult(request, reference, attachments[0], "")
	}
	call.Path = issuePath
	call.Query = url.Values{"fields": {"attachment,issuelinks"}}
	body, err := adapter.callProvider(ctx, call)
	if err != nil {
		return Result{}, err
	}
	var issue struct {
		Key    string `json:"key"`
		Fields struct {
			Attachments []jiraAttachmentView `json:"attachment"`
			Links       []jiraLinkView       `json:"issuelinks"`
		} `json:"fields"`
	}
	if decodeProviderJSON(body, &issue) != nil || issue.Key != in.IssueKey || len(issue.Fields.Attachments) > 100 || len(issue.Fields.Links) > 100 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	call.Query = nil
	if strings.HasPrefix(request.Operation, "jira.issue.link.") {
		links := []jiraLinkView{}
		for _, link := range issue.Fields.Links {
			if !decimalID(link.ID) {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			if link.Inward.Key != "" && !jiraIssueInProject(link.Inward.Key, project) || link.Outward.Key != "" && !jiraIssueInProject(link.Outward.Key, project) || link.Inward.Key == "" && link.Outward.Key == "" {
				continue
			}
			links = append(links, link)
		}
		if request.Operation == "jira.issue.link.list" {
			return catalogListResult(request, links, 0)
		}
		for _, link := range links {
			if link.ID == in.LinkID {
				if link.Inward.Key == "" {
					link.Inward.Key = in.IssueKey
				}
				if link.Outward.Key == "" {
					link.Outward.Key = in.IssueKey
				}
				if request.Operation == "jira.issue.link.delete" {
					call.Path = "/rest/api/3/issueLink/" + in.LinkID
					call.Method = http.MethodDelete
					call.EffectKey = request.EffectKey
					if _, err := adapter.callProvider(ctx, call); err != nil {
						return Result{}, err
					}
					return providerResult(request, reference, struct {
						Accepted bool `json:"accepted"`
					}{true})
				}
				return providerResult(request, reference, struct {
					ID      string `json:"id"`
					Type    string `json:"type"`
					Inward  string `json:"inward_issue_key"`
					Outward string `json:"outward_issue_key"`
				}{link.ID, link.Type.Name, link.Inward.Key, link.Outward.Key})
			}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.Operation == "jira.attachment.list" {
		return catalogListResult(request, issue.Fields.Attachments, 0)
	}
	for _, attachment := range issue.Fields.Attachments {
		if attachment.ID == in.AttachmentID {
			if request.Operation == "jira.attachment.delete" {
				call.Path = "/rest/api/3/attachment/" + in.AttachmentID
				call.Method = http.MethodDelete
				call.EffectKey = request.EffectKey
				if _, err := adapter.callProvider(ctx, call); err != nil {
					return Result{}, err
				}
				return providerResult(request, reference, struct {
					Accepted bool `json:"accepted"`
				}{true})
			}
			if attachment.Size < 0 || attachment.Size > maximumResponseBytes {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			call.Path = "/rest/api/3/attachment/content/" + in.AttachmentID
			call.Query = url.Values{"redirect": {"false"}}
			content, err := adapter.callProvider(ctx, call)
			if err != nil {
				return Result{}, err
			}
			if int64(len(content)) != attachment.Size {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			return jiraAttachmentResult(request, reference, attachment, base64.StdEncoding.EncodeToString(content))
		}
	}
	return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
}

// jiraAttachmentResult projects attachment metadata and optional Base64 content through the standard result encoder without exposing a provider download URL.
// jiraAttachmentResult выдаёт сведения вложения и необязательное содержимое Base64 через стандартный кодировщик результата, не раскрывая URL скачивания провайдера.
func jiraAttachmentResult(request Request, reference string, attachment jiraAttachmentView, content string) (Result, error) {
	return providerResult(request, reference, struct {
		ID        string `json:"id"`
		Filename  string `json:"file_name"`
		Size      int64  `json:"size"`
		MediaType string `json:"media_type"`
		Content   string `json:"content_base64,omitempty"`
	}{attachment.ID, attachment.Filename, attachment.Size, attachment.MediaType, content})
}

// boundedMultipart rejects path/control characters in the filename, parses the media type and strict Base64, then builds one multipart file under decoded and encoded size limits.
// boundedMultipart отклоняет разделители пути и управляющие символы в имени, разбирает тип и строгий Base64, затем создаёт один multipart-файл с ограничением декодированного и итогового размера.
func boundedMultipart(filename, mediaType, encoded string) ([]byte, string, error) {
	if filename == "" || strings.ContainsAny(filename, "\r\n/\\\x00") {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	content, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(content) > maximumResponseBytes {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	header.Set("Content-Type", parsed)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err = part.Write(content); err != nil {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if writer.Close() != nil || body.Len() > maximumResponseBytes {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}
