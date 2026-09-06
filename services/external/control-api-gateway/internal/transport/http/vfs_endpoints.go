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
	r, ok := vfsRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
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
	kinds, valid := vfsKindsRequest(p.Kinds)
	state := ""
	if p.LifecycleState != nil {
		state = string(*p.LifecycleState)
	}
	if !valid || !vfsStateRequest(state) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListVFSNodes(r.Context(), &controlplanev1.ListVFSNodesRequest{
		ProjectRef: stringValue(p.ProjectRef), Path: path, Query: stringValue(p.Query), LifecycleState: state, Kinds: kinds, Page: page(p.PageSize, p.PageToken),
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
	kinds, valid := vfsKindsRequest(p.Kinds)
	state, path := "", stringValue(p.Path)
	if p.LifecycleState != nil {
		state = string(*p.LifecycleState)
	}
	if !valid || !vfsStateRequest(state) || path != "" && !validVFSPath(path) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.SearchVFS(r.Context(), &controlplanev1.SearchVFSRequest{
		ProjectRef: stringValue(p.ProjectRef), Query: p.Query, Path: path, LifecycleState: state, Kinds: kinds, Page: page(p.PageSize, p.PageToken),
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
			Kind:      generated.VFSKind(strings.TrimPrefix(node.GetKind().String(), "VFS_NODE_KIND_")),
			Directory: node.GetDirectory(), ProjectRef: node.GetProjectRef(), EntityRef: node.GetEntityRef(),
			RunRef: node.GetRunRef(), SizeBytes: node.GetSizeBytes(), Digest: node.GetDigest(),
		}
		if !vfsDescriptor(node, &item) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
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

func vfsStateRequest(state string) bool {
	return state == "" || state == "ACTIVE" || state == "DELETED"
}
func vfsKindsRequest(input *generated.VFSKinds) ([]controlplanev1.VFSNodeKind, bool) {
	result := []controlplanev1.VFSNodeKind{}
	if input == nil {
		return result, true
	}
	if len(*input) > 12 {
		return nil, false
	}
	seen := map[generated.VFSKind]bool{}
	for _, kind := range *input {
		value, ok := controlplanev1.VFSNodeKind_value["VFS_NODE_KIND_"+string(kind)]
		if !ok || value == 0 || seen[kind] {
			return nil, false
		}
		seen[kind] = true
		result = append(result, controlplanev1.VFSNodeKind(value))
	}
	return result, true
}
func vfsDescriptor(node *controlplanev1.VFSNode, item *generated.VFSNode) bool {
	if node.Version < 0 || node.Version > maximumSafeJSONInteger || node.Revision < 0 || node.Revision > maximumSafeJSONInteger || len(node.RevisionRef) > 128 || node.RevisionRef != "" && !opaqueHTTPReference.MatchString(node.RevisionRef) {
		return false
	}
	switch node.LifecycleState {
	case "ACTIVE", "DELETED", "ARCHIVED":
	default:
		return false
	}
	switch node.ScanState {
	case "", "PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED":
	default:
		return false
	}
	switch node.ResourceKind {
	case "", "ARTIFACT", "SKILL_BUNDLE", "MEMORY_RECORD":
	default:
		return false
	}
	switch node.SelectionReason {
	case "AVAILABLE", "DIRECTORY", "PERMISSION_REQUIRED", "IMMUTABLE_CONTEXT", "LIFECYCLE_BLOCKED", "ARTIFACT_USED_BY_SKILL", "ARTIFACT_NOT_ACTIVE", "ARTIFACT_NOT_DELETED", "ARTIFACT_HAS_BINDINGS", "ACTIVE_RUN_USES_ARTIFACT":
	default:
		return false
	}
	if node.Selectable != (node.SelectionReason == "AVAILABLE") || len(node.NextActions) > 6 {
		return false
	}
	item.Version, item.Revision, item.RevisionRef = node.Version, node.Revision, node.RevisionRef
	item.LifecycleState = generated.VFSNodeLifecycleState(node.LifecycleState)
	item.ScanState = generated.VFSNodeScanState(node.ScanState)
	item.ResourceKind = generated.VFSNodeResourceKind(node.ResourceKind)
	item.Selectable = node.Selectable
	item.SelectionReason = generated.VFSNodeSelectionReason(node.SelectionReason)
	item.NextActions = []generated.VFSNodeNextActions{}
	seen := map[string]bool{}
	for _, action := range node.NextActions {
		switch action {
		case "DOWNLOAD", "DELETE", "RESTORE", "PURGE", "ARCHIVE", "BIND":
		default:
			return false
		}
		if seen[action] {
			return false
		}
		seen[action] = true
		item.NextActions = append(item.NextActions, generated.VFSNodeNextActions(action))
	}
	return true
}
