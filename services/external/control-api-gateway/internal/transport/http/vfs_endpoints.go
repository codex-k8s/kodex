package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// vfsRequest validates the VFS-specific longer UTF-8 cursor limit before applying common
// project/search/page controls without the common shorter cursor check. It preserves the original
// request token for owner pagination.
// vfsRequest проверяет отдельный более длинный UTF-8 cursor лимит VFS до общих project/search/page
// проверок без их более короткого cursor ограничения. Исходный токен сохраняется для пагинации
// владельца.
func vfsRequest(w http.ResponseWriter, r *http.Request, projectRef, query *string, pageSize *int, pageToken *string) (*http.Request, bool) {
	if pageToken != nil && (len(*pageToken) > 2048 || !utf8.ValidString(*pageToken)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return catalogRequest(w, r, projectRef, query, pageSize, nil)
}

// ListVFSNodes validates project/page/search, path and kind/lifecycle selectors, defaults an empty
// path to /projects and queries owner-visible VFS nodes. Project consistency and public node shape
// are checked before output.
// ListVFSNodes проверяет project/page/search, path и kind/lifecycle selectors, заменяет пустой
// path на /projects и запрашивает видимые у владельца VFS nodes. Согласованность проекта и
// публичная форма узлов проверяются до выдачи.
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

// SearchVFS requires at least two nonpadding query runes and valid optional path/kind/lifecycle
// controls before owner search. The exact returned project page is validated without filtering
// hidden or malformed nodes locally.
// SearchVFS требует не менее двух рун запроса без внешних пробелов и корректные optional
// path/kind/lifecycle до поиска владельца. Возвращённая проектная страница проверяется без
// локальной фильтрации скрытых и неверных узлов.
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

// vfsProjectPage checks the requested result-count bound and that every present node belongs to
// the optional selected project. It does not authorize the project or validate the remaining node
// fields.
// vfsProjectPage проверяет ограничение числа результатов и принадлежность каждого ненулевого узла
// необязательно выбранному проекту. Проект не авторизуется и прочие поля узла не проверяются.
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

// validVFSPath requires a bounded UTF-8 absolute virtual path without any double-dot substring,
// NUL, line breaks or backslashes. It does not resolve a filesystem path, normalize segments or
// provide host filesystem access.
// validVFSPath требует ограниченный UTF-8 абсолютный виртуальный путь без любого
// двойного-точечного фрагмента, NUL, переводов строк и обратных слешей. Путь файловой системы не
// разрешается, сегменты не нормализуются и доступ к хосту не выдаётся.
func validVFSPath(value string) bool {
	return len(value) <= 1000 && utf8.ValidString(value) && strings.HasPrefix(value, "/") &&
		!strings.Contains(value, "..") && !strings.ContainsAny(value, "\x00\r\n\\")
}

// writeVFSPage validates total/cursor bounds, every node path/text/size/kind and public descriptor
// plus optional timestamp before returning JSON. An invalid node rejects the entire owner page;
// the gateway never reads bytes from the virtual path.
// writeVFSPage проверяет total/cursor, path/text/size/kind каждого узла, публичный descriptor и
// optional время до JSON. Неверный узел отклоняет всю страницу владельца; gateway не читает байты
// по виртуальному пути.
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

// vfsStateRequest allows only omitted, ACTIVE or DELETED request lifecycle filters. ARCHIVED may
// exist in read projections but is not accepted as a request filter here.
// vfsStateRequest допускает только отсутствие фильтра lifecycle либо ACTIVE и DELETED. ARCHIVED
// возможен в проекции чтения, но здесь не принимается как фильтр запроса.
func vfsStateRequest(state string) bool {
	return state == "" || state == "ACTIVE" || state == "DELETED"
}

// vfsKindsRequest maps an optional list to at most twelve distinct nonzero known protobuf VFS
// kinds, preserving input order and an explicit empty slice for no filter.
// vfsKindsRequest преобразует необязательный список в не более двенадцати уникальных ненулевых
// известных protobuf видов VFS, сохраняя порядок и явный пустой срез при отсутствии фильтра.
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

// vfsDescriptor checks safe version/revision bounds, lifecycle/scan/resource/selection allowlists
// and up to six unique actions, then updates the public node metadata. Selectable must agree with
// AVAILABLE; these UI facts do not authorize a subsequent command.
// vfsDescriptor проверяет безопасные version/revision, закрытые lifecycle/scan/resource/selection
// и до шести уникальных actions, затем заполняет публичные metadata узла. Selectable соответствует
// AVAILABLE; UI факты не авторизуют следующую команду.
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
	case "AVAILABLE", "DIRECTORY", "PERMISSION_REQUIRED", "IMMUTABLE_CONTEXT", "LIFECYCLE_BLOCKED", "ARTIFACT_USED_BY_SKILL", "ARTIFACT_NOT_ACTIVE", "ARTIFACT_NOT_DELETED", "ARTIFACT_HAS_BINDINGS", "ARTIFACT_HAS_RETAINED_REVISIONS", "ACTIVE_RUN_USES_ARTIFACT":
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
