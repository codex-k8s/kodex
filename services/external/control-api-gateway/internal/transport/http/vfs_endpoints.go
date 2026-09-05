package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func vfsRequest(w http.ResponseWriter, r *http.Request, projectRef, query *string, pageSize *int, pageToken *string) (*http.Request, bool) {
	if pageToken != nil && (len(*pageToken) > 2048 || !utf8.ValidString(*pageToken)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return catalogRequest(w, r, projectRef, query, pageSize, nil)
}

func (server *Server) ListVFSNodes(w http.ResponseWriter, r *http.Request, p generated.ListVFSNodesParams) {
	r, ok := vfsRequest(w, r, p.ProjectRef, nil, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	path := stringValue(p.Path)
	if path == "" {
		path = "/projects"
	}
	if !validVFSPath(path) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListVFSNodes(r.Context(), &controlplanev1.ListVFSNodesRequest{
		ProjectRef: stringValue(p.ProjectRef), Path: path, Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !vfsProjectPage(response.GetNodes(), stringValue(p.ProjectRef), int(page(p.PageSize, p.PageToken).PageSize)) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeVFSPage(w, response.GetNodes(), response.GetTotal(), response.GetPage().GetNextPageToken())
}

func (server *Server) SearchVFS(w http.ResponseWriter, r *http.Request, p generated.SearchVFSParams) {
	r, ok := vfsRequest(w, r, p.ProjectRef, &p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Query)) < 2 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.SearchVFS(r.Context(), &controlplanev1.SearchVFSRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: p.Query, Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !vfsProjectPage(response.GetNodes(), stringValue(p.ProjectRef), int(page(p.PageSize, p.PageToken).PageSize)) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeVFSPage(w, response.GetNodes(), response.GetTotal(), response.GetPage().GetNextPageToken())
}

func vfsProjectPage(nodes []*controlplanev1.VFSNode, project string, limit int) bool {
	if len(nodes) > limit {
		return false
	}
	for _, node := range nodes {
		if node == nil || project != "" && node.ProjectRef != project {
			return false
		}
	}
	return true
}

func validVFSPath(value string) bool {
	return len(value) <= 1000 && utf8.ValidString(value) && strings.HasPrefix(value, "/") &&
		!strings.Contains(value, "..") && !strings.ContainsAny(value, "\x00\r\n\\")
}

func writeVFSPage(w http.ResponseWriter, nodes []*controlplanev1.VFSNode, total int64, next string) {
	if len(nodes) > 100 || total < int64(len(nodes)) || total > maximumSafeJSONInteger || len(next) > 2048 || !utf8.ValidString(next) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.VFSNodePage{Items: make([]generated.VFSNode, 0, len(nodes)), Total: total, NextPageToken: next}
	for _, node := range nodes {
		if node == nil || node.GetRef() == "" || len(node.GetRef()) > 1000 || !validVFSPath(node.GetPath()) ||
			!utf8.ValidString(node.GetRef()) || !validSearchText(node.GetName(), 0, 1000) ||
			!validSearchText(node.GetProjectRef(), 0, 128) || !validSearchText(node.GetEntityRef(), 0, 128) ||
			!validSearchText(node.GetRunRef(), 0, 128) || !validSearchText(node.GetDigest(), 0, 128) ||
			node.GetParentPath() != "" && !validVFSPath(node.GetParentPath()) ||
			node.GetSizeBytes() < 0 || node.GetSizeBytes() > maximumSafeJSONInteger {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		switch node.GetKind() {
		case controlplanev1.VFSNodeKind_VFS_NODE_KIND_DIRECTORY, controlplanev1.VFSNodeKind_VFS_NODE_KIND_PROJECT,
			controlplanev1.VFSNodeKind_VFS_NODE_KIND_AGENT, controlplanev1.VFSNodeKind_VFS_NODE_KIND_WORKFLOW,
			controlplanev1.VFSNodeKind_VFS_NODE_KIND_RUN, controlplanev1.VFSNodeKind_VFS_NODE_KIND_INPUT,
			controlplanev1.VFSNodeKind_VFS_NODE_KIND_RESULT, controlplanev1.VFSNodeKind_VFS_NODE_KIND_SKILL,
			controlplanev1.VFSNodeKind_VFS_NODE_KIND_MEMORY, controlplanev1.VFSNodeKind_VFS_NODE_KIND_AUTOMATION,
			controlplanev1.VFSNodeKind_VFS_NODE_KIND_ENVIRONMENT, controlplanev1.VFSNodeKind_VFS_NODE_KIND_AVATAR:
		default:
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		item := generated.VFSNode{
			Ref: node.GetRef(), Path: node.GetPath(), ParentPath: node.GetParentPath(), Name: node.GetName(),
			Kind:      generated.VFSNodeKind(strings.TrimPrefix(node.GetKind().String(), "VFS_NODE_KIND_")),
			Directory: node.GetDirectory(), ProjectRef: node.GetProjectRef(), EntityRef: node.GetEntityRef(),
			RunRef: node.GetRunRef(), SizeBytes: node.GetSizeBytes(), Digest: node.GetDigest(),
		}
		if node.GetModifiedAt() != nil {
			if node.GetModifiedAt().CheckValid() != nil {
				writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
			modified := node.GetModifiedAt().AsTime()
			item.ModifiedAt = &modified
		}
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}
