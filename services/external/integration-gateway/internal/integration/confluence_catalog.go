package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

type confluenceCatalogInput struct {
	PageID          string `json:"page_id"`
	CommentID       string `json:"comment_id"`
	ParentCommentID string `json:"parent_comment_id"`
	AttachmentID    string `json:"attachment_id"`
	Body            string `json:"body"`
	ExpectedVersion int64  `json:"expected_version"`
	Limit           int    `json:"limit"`
	Cursor          string `json:"cursor"`
}

type confluenceComment struct {
	ID              string `json:"id"`
	PageID          string `json:"pageId"`
	ParentCommentID string `json:"parentCommentId"`
	Status          string `json:"status"`
	Version         struct {
		Number int64 `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

type confluenceAttachment struct {
	ID           string `json:"id"`
	PageID       string `json:"pageId"`
	Title        string `json:"title"`
	MediaType    string `json:"mediaType"`
	Size         int64  `json:"fileSize"`
	DownloadLink string `json:"downloadLink"`
	Version      struct {
		Number int64 `json:"number"`
	} `json:"version"`
}

type confluenceDescendant struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	ParentID string `json:"parentId"`
	Depth    int    `json:"depth"`
}

func confluenceCatalogPage[T any](request Request, body []byte, limit int, previous string, valid func(T) bool) (Result, error) {
	var page struct {
		Results []T `json:"results"`
		Links   struct {
			Next string `json:"next"`
		} `json:"_links"`
	}
	if decodeProviderJSON(body, &page) != nil || len(page.Results) > limit {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	for _, item := range page.Results {
		if !valid(item) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	next := ""
	if page.Links.Next != "" {
		link, err := url.Parse(page.Links.Next)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		next = link.Query().Get("cursor")
		if next == "" || next == previous || len(next) > 1024 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	if page.Results == nil {
		page.Results = []T{}
	}
	encoded, err := json.Marshal(page.Results)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, request.Operation+":"+request.EffectKey, struct {
		Items string `json:"items"`
		Count int    `json:"count"`
		Next  string `json:"next_cursor,omitempty"`
	}{string(encoded), len(page.Results), next})
}

func (adapter *Adapter) executeConfluenceCatalog(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	var in confluenceCatalogInput
	if json.Unmarshal(raw, &in) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	for _, id := range []string{in.PageID, in.CommentID, in.ParentCommentID, in.AttachmentID} {
		if id != "" && !decimalID(id) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	call := providerCall{BaseURL: configuration["base_url"], AuthScheme: configuration["auth_scheme"], Username: configuration["username"], Credential: request.Credential, Capability: capability, Method: http.MethodGet}
	if request.Operation == "confluence.space.list" {
		call.Path = "/wiki/api/v2/spaces/" + url.PathEscape(configuration["space_id"])
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		var space struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		}
		if decodeProviderJSON(body, &space) != nil || space.ID != configuration["space_id"] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return catalogListResult(request, []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		}{space}, 0)
	}
	if _, err := adapter.readConfluencePage(ctx, request, capability, configuration, in.PageID); err != nil {
		return Result{}, err
	}
	pagePath := "/wiki/api/v2/pages/" + in.PageID
	query := url.Values{"limit": {strconv.Itoa(in.Limit)}}
	if in.Cursor != "" {
		query.Set("cursor", in.Cursor)
	}
	switch request.Operation {
	case "confluence.page.descendant.list":
		call.Path = pagePath + "/descendants"
		call.Query = query
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		return confluenceCatalogPage(request, body, in.Limit, in.Cursor, func(v confluenceDescendant) bool {
			return decimalID(v.ID) && decimalID(v.ParentID) && v.Depth > 0 && (v.Type == "page" || v.Type == "folder" || v.Type == "database" || v.Type == "embed" || v.Type == "whiteboard")
		})
	case "confluence.page.comment.list", "confluence.page.comment.read", "confluence.page.comment.create", "confluence.page.comment.update", "confluence.page.comment.delete":
		return adapter.confluenceCommentOperation(ctx, request, call, in, pagePath, query)
	case "confluence.attachment.list":
		call.Path = pagePath + "/attachments"
		call.Query = query
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		// Ссылки загрузки не включаем в результат инструмента.
		var page struct {
			Results []confluenceAttachment `json:"results"`
			Links   json.RawMessage        `json:"_links"`
		}
		if decodeProviderJSON(body, &page) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for i := range page.Results {
			page.Results[i].DownloadLink = ""
		}
		clean, err := json.Marshal(page)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return confluenceCatalogPage(request, clean, in.Limit, in.Cursor, func(v confluenceAttachment) bool { return decimalID(v.ID) && v.PageID == in.PageID && v.Size >= 0 })
	case "confluence.attachment.read", "confluence.attachment.delete":
		call.Path = "/wiki/api/v2/attachments/" + in.AttachmentID
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		var attachment confluenceAttachment
		if decodeProviderJSON(body, &attachment) != nil || attachment.ID != in.AttachmentID || attachment.PageID != in.PageID {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if request.Operation == "confluence.attachment.delete" {
			call.Method = http.MethodDelete
			call.EffectKey = request.EffectKey
			if _, err := adapter.callProvider(ctx, call); err != nil {
				return Result{}, err
			}
			return providerResult(request, request.Operation+":"+request.EffectKey, struct {
				Accepted bool `json:"accepted"`
			}{true})
		}
		if attachment.Size < 0 || attachment.Size > maximumResponseBytes {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		base, err := parseProviderBaseURL(configuration["base_url"])
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		link, err := url.Parse(attachment.DownloadLink)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if strings.HasPrefix(link.Path, "/download/attachments/") {
			link.Path = "/wiki" + link.Path
			link.RawPath = ""
		}
		resolved := base.ResolveReference(link)
		if resolved.Scheme != base.Scheme || resolved.Host != base.Host || resolved.User != nil || resolved.Fragment != "" || !strings.HasPrefix(resolved.Path, "/wiki/download/attachments/"+in.PageID+"/") || !validRepositoryPath(strings.TrimPrefix(resolved.Path, "/"), false) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		call.Path = resolved.EscapedPath()
		call.Query = resolved.Query()
		content, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		if int64(len(content)) != attachment.Size {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "confluence-attachment:"+attachment.ID, struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			MediaType string `json:"media_type"`
			Size      int64  `json:"size"`
			Version   int64  `json:"version"`
			Content   string `json:"content_base64"`
		}{attachment.ID, attachment.Title, attachment.MediaType, attachment.Size, attachment.Version.Number, base64.StdEncoding.EncodeToString(content)})
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) confluenceCommentOperation(ctx context.Context, request Request, call providerCall, in confluenceCatalogInput, pagePath string, query url.Values) (Result, error) {
	read := func(id string) (confluenceComment, error) {
		readCall := call
		readCall.Path = "/wiki/api/v2/footer-comments/" + id
		readCall.Query = url.Values{"body-format": {"storage"}}
		body, err := adapter.callProvider(ctx, readCall)
		if err != nil {
			return confluenceComment{}, err
		}
		var comment confluenceComment
		if decodeProviderJSON(body, &comment) != nil || comment.ID != id || comment.PageID != in.PageID || comment.Version.Number < 1 {
			return confluenceComment{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return comment, nil
	}
	if in.ParentCommentID != "" {
		if _, err := read(in.ParentCommentID); err != nil {
			return Result{}, err
		}
	}
	if request.Operation == "confluence.page.comment.list" {
		call.Path = pagePath + "/footer-comments"
		if in.ParentCommentID != "" {
			call.Path = "/wiki/api/v2/footer-comments/" + in.ParentCommentID + "/children"
		}
		query.Set("body-format", "storage")
		call.Query = query
		body, err := adapter.callProvider(ctx, call)
		if err != nil {
			return Result{}, err
		}
		return confluenceCatalogPage(request, body, in.Limit, in.Cursor, func(v confluenceComment) bool {
			return decimalID(v.ID) && v.PageID == in.PageID && v.Version.Number > 0 && (in.ParentCommentID == "" || v.ParentCommentID == in.ParentCommentID)
		})
	}
	if request.Operation != "confluence.page.comment.create" {
		current, err := read(in.CommentID)
		if err != nil {
			return Result{}, err
		}
		if request.Operation == "confluence.page.comment.read" {
			return confluenceCommentResult(request, current)
		}
		if current.Version.Number != in.ExpectedVersion {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	call.Path = "/wiki/api/v2/footer-comments"
	call.Method = http.MethodPost
	call.EffectKey = request.EffectKey
	if request.Operation == "confluence.page.comment.create" {
		pageID := in.PageID
		if in.ParentCommentID != "" {
			pageID = ""
		}
		call.Body = struct {
			PageID string `json:"pageId,omitempty"`
			Parent string `json:"parentCommentId,omitempty"`
			Body   struct {
				Representation string `json:"representation"`
				Value          string `json:"value"`
			} `json:"body"`
		}{PageID: pageID, Parent: in.ParentCommentID, Body: struct {
			Representation string `json:"representation"`
			Value          string `json:"value"`
		}{"storage", in.Body}}
	} else {
		call.Path += "/" + in.CommentID
		if request.Operation == "confluence.page.comment.delete" {
			call.Method = http.MethodDelete
		} else {
			call.Method = http.MethodPut
			call.Body = struct {
				Version struct {
					Number int64 `json:"number"`
				} `json:"version"`
				Body struct {
					Representation string `json:"representation"`
					Value          string `json:"value"`
				} `json:"body"`
			}{Version: struct {
				Number int64 `json:"number"`
			}{in.ExpectedVersion + 1}, Body: struct {
				Representation string `json:"representation"`
				Value          string `json:"value"`
			}{"storage", in.Body}}
		}
	}
	body, err := adapter.callProvider(ctx, call)
	if err != nil {
		return Result{}, err
	}
	if call.Method == http.MethodDelete {
		return providerResult(request, request.Operation+":"+request.EffectKey, struct {
			Accepted bool `json:"accepted"`
		}{true})
	}
	var comment confluenceComment
	if decodeProviderJSON(body, &comment) != nil || !decimalID(comment.ID) || comment.PageID != in.PageID || (in.CommentID != "" && comment.ID != in.CommentID) || (call.Method == http.MethodPut && comment.Version.Number != in.ExpectedVersion+1) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluenceCommentResult(request, comment)
}

func confluenceCommentResult(request Request, comment confluenceComment) (Result, error) {
	return providerResult(request, "confluence-comment:"+comment.ID, struct {
		ID      string `json:"id"`
		PageID  string `json:"page_id"`
		Parent  string `json:"parent_comment_id"`
		Status  string `json:"status"`
		Version int64  `json:"version"`
		Body    string `json:"body"`
	}{comment.ID, comment.PageID, comment.ParentCommentID, comment.Status, comment.Version.Number, comment.Body.Storage.Value})
}
