package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func confluenceExtendedResponse(t *testing.T, operation string, r *http.Request) (string, bool) {
	t.Helper()
	switch operation {
	case "confluence.space.list", "confluence.page.descendant.list", "confluence.page.comment.list", "confluence.page.comment.read", "confluence.page.comment.create", "confluence.page.comment.update", "confluence.page.comment.delete", "confluence.attachment.list", "confluence.attachment.read", "confluence.attachment.delete":
	default:
		return "", false
	}
	if r.URL.Path == "/wiki/api/v2/pages/3" && r.Method == "GET" {
		return `{"id":"3","title":"Title","spaceId":"42","status":"current","version":{"number":1},"body":{"storage":{"value":"Text"}}}`, true
	}
	check := func(method, path string) {
		if r.Method != method || r.URL.Path != path {
			t.Errorf("Confluence route changed: %s %s", r.Method, r.URL.Path)
		}
	}
	comment := `{"id":"4","pageId":"3","status":"current","version":{"number":1},"body":{"storage":{"value":"Text"}}}`
	attachment := `{"id":"4","pageId":"3","title":"a.txt","mediaType":"text/plain","fileSize":4,"downloadLink":"/wiki/download/attachments/3/a.txt?version=1","version":{"number":1}}`
	list := func(path, item string) (string, bool) {
		check("GET", path)
		if r.URL.Query().Get("cursor") != "cursor-2" || r.URL.Query().Get("limit") != "1" {
			t.Error("Confluence pagination lost")
		}
		return fmt.Sprintf(`{"results":[%s],"_links":{"next":"%s?cursor=cursor-3"}}`, item, path), true
	}
	switch operation {
	case "confluence.space.list":
		check("GET", "/wiki/api/v2/spaces/42")
		return `{"id":"42","key":"OPS","name":"Space"}`, true
	case "confluence.page.descendant.list":
		return list("/wiki/api/v2/pages/3/descendants", `{"id":"4","parentId":"3","title":"Child","status":"current","type":"page","depth":1}`)
	case "confluence.page.comment.list":
		return list("/wiki/api/v2/pages/3/footer-comments", comment)
	case "confluence.page.comment.read":
		check("GET", "/wiki/api/v2/footer-comments/4")
		return comment, true
	case "confluence.page.comment.create":
		check("POST", "/wiki/api/v2/footer-comments")
		return comment, true
	case "confluence.page.comment.update":
		if r.Method == "GET" {
			check("GET", "/wiki/api/v2/footer-comments/4")
			return comment, true
		}
		check("PUT", "/wiki/api/v2/footer-comments/4")
		return strings.Replace(comment, `"number":1`, `"number":2`, 1), true
	case "confluence.page.comment.delete":
		if r.Method == "GET" {
			check("GET", "/wiki/api/v2/footer-comments/4")
			return comment, true
		}
		check("DELETE", "/wiki/api/v2/footer-comments/4")
		return `{}`, true
	case "confluence.attachment.list":
		return list("/wiki/api/v2/pages/3/attachments", attachment)
	case "confluence.attachment.read":
		if r.URL.Path == "/wiki/api/v2/attachments/4" {
			check("GET", "/wiki/api/v2/attachments/4")
			return attachment, true
		}
		check("GET", "/wiki/download/attachments/3/a.txt")
		return "Text", true
	case "confluence.attachment.delete":
		if r.Method == "GET" {
			check("GET", "/wiki/api/v2/attachments/4")
			return attachment, true
		}
		check("DELETE", "/wiki/api/v2/attachments/4")
		return `{}`, true
	}
	return "", false
}
