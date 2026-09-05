package httptransport

import (
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef) {
	if !opaqueHTTPReference.MatchString(bundleRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetSkillBundle(r.Context(), &controlplanev1.GetSkillBundleRequest{BundleRef: bundleRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) ArchiveSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.ArchiveSkillBundleParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ArchiveSkillBundle(r.Context(), &controlplanev1.ArchiveSkillBundleRequest{Mutation: mutation, BundleRef: bundleRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) RestoreSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.RestoreSkillBundleParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RestoreSkillBundle(r.Context(), &controlplanev1.RestoreSkillBundleRequest{Mutation: mutation, BundleRef: bundleRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) PurgeSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.PurgeSkillBundleParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PurgeSkillBundle(r.Context(), &controlplanev1.PurgeSkillBundleRequest{Mutation: mutation, BundleRef: bundleRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) BindAgentSkillBundle(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, bundleRef generated.SkillBundleRef, p generated.BindAgentSkillBundleParams) {
	body, ok := decodeJSON[generated.AgentContextBindingInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(agentRef) || !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(body.RevisionRef) || body.ExpectedBindingVersion < 0 || body.ExpectedBindingVersion > maximumSafeJSONInteger {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.BindAgentSkillBundle(r.Context(), &controlplanev1.BindAgentSkillBundleRequest{Mutation: mutation, AgentRef: agentRef, BundleRef: bundleRef, RevisionRef: body.RevisionRef, ExpectedBindingVersion: body.ExpectedBindingVersion})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAgentContextBinding(w, response.GetBinding(), agentRef, bundleRef, body.RevisionRef)
}

func (server *Server) UnbindAgentSkillBundle(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, bundleRef generated.SkillBundleRef, p generated.UnbindAgentSkillBundleParams) {
	body, ok := decodeJSON[generated.AgentContextBindingInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(agentRef) || !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(body.RevisionRef) || body.ExpectedBindingVersion < 0 || body.ExpectedBindingVersion > maximumSafeJSONInteger {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UnbindAgentSkillBundle(r.Context(), &controlplanev1.UnbindAgentSkillBundleRequest{Mutation: mutation, AgentRef: agentRef, BundleRef: bundleRef, RevisionRef: body.RevisionRef, ExpectedBindingVersion: body.ExpectedBindingVersion})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAgentContextBinding(w, response.GetBinding(), agentRef, bundleRef, body.RevisionRef)
}

func (server *Server) ListSkillBundles(w http.ResponseWriter, r *http.Request, p generated.ListSkillBundlesParams) {
	if !validContextListInput(stringValue(p.ProjectRef), stringValue(p.AgentRef), stringValue(p.Query), p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	state, ok := contextStateInput(stringValue(p.State))
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListSkillBundles(r.Context(), &controlplanev1.ListSkillBundlesRequest{ProjectRef: stringValue(p.ProjectRef), AgentRef: stringValue(p.AgentRef), Query: stringValue(p.Query), State: state, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validContextPageResponse(len(response.GetBundles()), response.GetTotal(), response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.SkillBundlePage{Items: []generated.SkillBundle{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken()}
	seen := make(map[string]bool)
	for _, input := range response.GetBundles() {
		item, ok := skillBundleView(input)
		if !ok || seen[item.Ref] || stringValue(p.ProjectRef) != "" && item.ProjectRef != stringValue(p.ProjectRef) {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

func (server *Server) ListSkillBundleRevisions(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.ListSkillBundleRevisionsParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListSkillBundleRevisions(r.Context(), &controlplanev1.ListSkillBundleRevisionsRequest{BundleRef: bundleRef, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validContextPageResponse(len(response.GetRevisions()), response.GetTotal(), response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.SkillBundleRevisionPage{Items: []generated.SkillBundleRevision{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken()}
	seen := make(map[string]bool)
	for _, input := range response.GetRevisions() {
		item, ok := skillRevisionView(input)
		if !ok || seen[item.Ref] {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

func (server *Server) GetMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetMemoryRecord(r.Context(), &controlplanev1.GetMemoryRecordRequest{RecordRef: recordRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), recordRef, 200)
}

func (server *Server) ArchiveMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.ArchiveMemoryRecordParams) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ArchiveMemoryRecord(r.Context(), &controlplanev1.ArchiveMemoryRecordRequest{Mutation: mutation, RecordRef: recordRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), recordRef, 200)
}

func (server *Server) RestoreMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.RestoreMemoryRecordParams) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RestoreMemoryRecord(r.Context(), &controlplanev1.RestoreMemoryRecordRequest{Mutation: mutation, RecordRef: recordRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), recordRef, 200)
}

func (server *Server) PurgeMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.PurgeMemoryRecordParams) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PurgeMemoryRecord(r.Context(), &controlplanev1.PurgeMemoryRecordRequest{Mutation: mutation, RecordRef: recordRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), recordRef, 200)
}

func (server *Server) BindAgentMemoryRecord(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, recordRef generated.MemoryRecordRef, p generated.BindAgentMemoryRecordParams) {
	body, ok := decodeJSON[generated.AgentContextBindingInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(agentRef) || !opaqueHTTPReference.MatchString(recordRef) || !opaqueHTTPReference.MatchString(body.RevisionRef) || body.ExpectedBindingVersion < 0 || body.ExpectedBindingVersion > maximumSafeJSONInteger {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.BindAgentMemoryRecord(r.Context(), &controlplanev1.BindAgentMemoryRecordRequest{Mutation: mutation, AgentRef: agentRef, RecordRef: recordRef, RevisionRef: body.RevisionRef, ExpectedBindingVersion: body.ExpectedBindingVersion})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAgentContextBinding(w, response.GetBinding(), agentRef, recordRef, body.RevisionRef)
}

func (server *Server) UnbindAgentMemoryRecord(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, recordRef generated.MemoryRecordRef, p generated.UnbindAgentMemoryRecordParams) {
	body, ok := decodeJSON[generated.AgentContextBindingInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(agentRef) || !opaqueHTTPReference.MatchString(recordRef) || !opaqueHTTPReference.MatchString(body.RevisionRef) || body.ExpectedBindingVersion < 0 || body.ExpectedBindingVersion > maximumSafeJSONInteger {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UnbindAgentMemoryRecord(r.Context(), &controlplanev1.UnbindAgentMemoryRecordRequest{Mutation: mutation, AgentRef: agentRef, RecordRef: recordRef, RevisionRef: body.RevisionRef, ExpectedBindingVersion: body.ExpectedBindingVersion})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAgentContextBinding(w, response.GetBinding(), agentRef, recordRef, body.RevisionRef)
}

func (server *Server) ListMemoryRecords(w http.ResponseWriter, r *http.Request, p generated.ListMemoryRecordsParams) {
	if !validContextListInput(stringValue(p.ProjectRef), stringValue(p.AgentRef), stringValue(p.Query), p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	state, ok := contextStateInput(stringValue(p.State))
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListMemoryRecords(r.Context(), &controlplanev1.ListMemoryRecordsRequest{ProjectRef: stringValue(p.ProjectRef), AgentRef: stringValue(p.AgentRef), Query: stringValue(p.Query), State: state, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validContextPageResponse(len(response.GetRecords()), response.GetTotal(), response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.MemoryRecordPage{Items: []generated.KodexMemoryRecord{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken()}
	seen := make(map[string]bool)
	for _, input := range response.GetRecords() {
		item, ok := memoryRecordView(input)
		if !ok || seen[item.Ref] || stringValue(p.ProjectRef) != "" && item.ProjectRef != stringValue(p.ProjectRef) {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

func (server *Server) ListMemoryRecordRevisions(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.ListMemoryRecordRevisionsParams) {
	if !opaqueHTTPReference.MatchString(recordRef) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListMemoryRecordRevisions(r.Context(), &controlplanev1.ListMemoryRecordRevisionsRequest{RecordRef: recordRef, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validContextPageResponse(len(response.GetRevisions()), response.GetTotal(), response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.MemoryRecordRevisionPage{Items: []generated.MemoryRecordRevision{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken()}
	seen := make(map[string]bool)
	for _, input := range response.GetRevisions() {
		item, ok := memoryRevisionView(input)
		if !ok || seen[item.Ref] {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

func validContextListInput(project, agent, query string, size *int, token *string) bool {
	return (project == "" || opaqueHTTPReference.MatchString(project)) && (agent == "" || opaqueHTTPReference.MatchString(agent)) && len(query) <= 256 && validHTTPPage(size, token)
}
func validContextPageResponse(count int, total int64, token string) bool {
	return count <= 100 && total >= int64(count) && total <= maximumSafeJSONInteger && len(token) <= 512
}
func contextStateInput(value string) (controlplanev1.ContextResourceState, bool) {
	if value == "" {
		return controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_UNSPECIFIED, true
	}
	for name, number := range controlplanev1.ContextResourceState_value {
		if strings.TrimPrefix(name, "CONTEXT_RESOURCE_STATE_") == value && number > 0 {
			return controlplanev1.ContextResourceState(number), true
		}
	}
	return 0, false
}
