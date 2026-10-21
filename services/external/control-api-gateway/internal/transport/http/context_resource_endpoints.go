package httptransport

import (
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// GetSkillBundle validates the reference, calls the typed owner query and writes
// only a validated bundle with its ETag; RPC failures retain their mapped status.
// GetSkillBundle проверяет ссылку, вызывает типизированный owner query и выдаёт
// только проверенный bundle с ETag; ошибки RPC сохраняют своё отображение.
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

// ArchiveSkillBundle checks reference and versioned headers before the specialized
// archive RPC; lifecycle authority and idempotent effects remain in the owner.
// ArchiveSkillBundle проверяет ссылку и versioned-заголовки до отдельного archive
// RPC; полномочия lifecycle и идемпотентные эффекты остаются у владельца.
func (server *Server) ArchiveSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.ArchiveSkillBundleParams) {
	mutation, ok := requireContextResourceMutation(w, bundleRef, p.IdempotencyKey, p.IfMatch)
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

// RestoreSkillBundle forwards an exact validated mutation to the restore command
// and checks the returned bundle, without locally rewriting lifecycle state.
// RestoreSkillBundle передаёт точную проверенную mutation в команду restore
// и проверяет ответ bundle, не переписывая lifecycle локально.
func (server *Server) RestoreSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.RestoreSkillBundleParams) {
	mutation, ok := requireContextResourceMutation(w, bundleRef, p.IdempotencyKey, p.IfMatch)
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

// PurgeSkillBundle delegates the exact versioned purge request to its owner and
// validates returned metadata; this handler performs no independent file cleanup.
// PurgeSkillBundle передаёт точный versioned purge владельцу и проверяет metadata
// ответа; самостоятельной файловой очистки этот handler не выполняет.
func (server *Server) PurgeSkillBundle(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, p generated.PurgeSkillBundleParams) {
	mutation, ok := requireContextResourceMutation(w, bundleRef, p.IdempotencyKey, p.IfMatch)
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

// BindAgentSkillBundle validates body, three references and both version inputs
// before its typed bind RPC; the response must match agent/resource/revision.
// BindAgentSkillBundle проверяет body, три ссылки и оба входа версии до typed
// bind RPC; ответ должен соответствовать agent/resource/revision.
func (server *Server) BindAgentSkillBundle(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, bundleRef generated.SkillBundleRef, p generated.BindAgentSkillBundleParams) {
	body, mutation, ok := requireContextBindingMutation(w, r, agentRef, bundleRef, p.IdempotencyKey, p.IfMatch)
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

// UnbindAgentSkillBundle keeps the same strict binding input checks as bind but
// calls the separate unbind command, preserving its own owner-side semantics.
// UnbindAgentSkillBundle сохраняет строгие проверки входа binding, но вызывает
// отдельную unbind-команду с её собственными owner-side правилами.
func (server *Server) UnbindAgentSkillBundle(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, bundleRef generated.SkillBundleRef, p generated.UnbindAgentSkillBundleParams) {
	body, mutation, ok := requireContextBindingMutation(w, r, agentRef, bundleRef, p.IdempotencyKey, p.IfMatch)
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

// ListSkillBundles checks filters and page bounds, then validates every returned
// bundle, uniqueness and optional project scope before writing any page bytes.
// ListSkillBundles проверяет фильтры и страницу, затем каждый bundle, уникальность
// и необязательный project scope до записи каких-либо байтов страницы.
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

// ListSkillBundleRevisions queries a valid bundle reference and page, then rejects
// invalid or repeated revision views rather than returning a partial result.
// ListSkillBundleRevisions запрашивает допустимые bundle/page и отклоняет неверные
// или повторные ревизии вместо выдачи частичного результата.
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

// GetMemoryRecord validates its reference, delegates the owner query and writes
// the validated memory view and ETag without bypassing redaction checks.
// GetMemoryRecord проверяет ссылку, вызывает owner query и выдаёт проверенное
// представление памяти с ETag, не обходя проверки redaction.
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

// ArchiveMemoryRecord checks the exact resource and versioned mutation headers,
// then delegates lifecycle change and validates the returned memory view.
// ArchiveMemoryRecord проверяет точный ресурс и versioned-заголовки mutation,
// затем передаёт lifecycle владельцу и проверяет представление памяти в ответе.
func (server *Server) ArchiveMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.ArchiveMemoryRecordParams) {
	mutation, ok := requireContextResourceMutation(w, recordRef, p.IdempotencyKey, p.IfMatch)
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

// RestoreMemoryRecord calls the specialized restore command only after the shared
// reference/header checks; it does not restore content from a local cache.
// RestoreMemoryRecord вызывает отдельную restore-команду только после общих
// проверок ссылки/заголовков; содержимое не восстанавливается из локального кеша.
func (server *Server) RestoreMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.RestoreMemoryRecordParams) {
	mutation, ok := requireContextResourceMutation(w, recordRef, p.IdempotencyKey, p.IfMatch)
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

// PurgeMemoryRecord forwards the exact versioned purge command and validates its
// memory response; the owner, not this transport helper, controls deletion.
// PurgeMemoryRecord передаёт точную versioned purge-команду и проверяет ответ
// памяти; удалением управляет владелец, а не transport helper.
func (server *Server) PurgeMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.PurgeMemoryRecordParams) {
	mutation, ok := requireContextResourceMutation(w, recordRef, p.IdempotencyKey, p.IfMatch)
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

// BindAgentMemoryRecord checks binding body and mutation headers before the typed
// owner command, requiring the returned binding to match all requested refs.
// BindAgentMemoryRecord проверяет body и заголовки mutation до typed owner-команды
// и требует совпадения всех запрошенных ссылок в возвращённой привязке.
func (server *Server) BindAgentMemoryRecord(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, recordRef generated.MemoryRecordRef, p generated.BindAgentMemoryRecordParams) {
	body, mutation, ok := requireContextBindingMutation(w, r, agentRef, recordRef, p.IdempotencyKey, p.IfMatch)
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

// UnbindAgentMemoryRecord validates the same binding tuple before the separate
// unbind RPC and accepts no unrelated resource or revision in its response.
// UnbindAgentMemoryRecord проверяет ту же связку до отдельного unbind RPC
// и не принимает чужой ресурс или ревизию в ответе.
func (server *Server) UnbindAgentMemoryRecord(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, recordRef generated.MemoryRecordRef, p generated.UnbindAgentMemoryRecordParams) {
	body, mutation, ok := requireContextBindingMutation(w, r, agentRef, recordRef, p.IdempotencyKey, p.IfMatch)
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

// ListMemoryRecords validates query/page/state inputs and each memory view,
// including uniqueness and the requested project, before writing the full page.
// ListMemoryRecords проверяет query/page/state и каждое представление памяти,
// включая уникальность и запрошенный проект, до записи полной страницы.
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

// ListMemoryRecordRevisions validates the resource/page and all revision views;
// an invalid view, duplicate ref or malformed page rejects the whole response.
// ListMemoryRecordRevisions проверяет ресурс/page и все представления ревизий;
// неверная ревизия, повтор ref или неверная страница отклоняет весь ответ.
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

// validContextListInput permits optional bounded opaque project/agent refs,
// limits query to256bytes and delegates pagination checks without normalization.
// validContextListInput допускает необязательные ограниченные project/agent ref,
// ограничивает query256байтами и проверяет page без нормализации входа.
func validContextListInput(project, agent, query string, size *int, token *string) bool {
	return (project == "" || opaqueHTTPReference.MatchString(project)) && (agent == "" || opaqueHTTPReference.MatchString(agent)) && len(query) <= 256 && validHTTPPage(size, token)
}

// validContextPageResponse bounds item count, total within safe JSON integers
// and cursor bytes; callers separately validate every typed item and its scope.
// validContextPageResponse ограничивает число элементов, total безопасным JSON
// числом и cursor по байтам; элементы и scope отдельно проверяет вызывающий код.
func validContextPageResponse(count int, total int64, token string) bool {
	return count <= 100 && total >= int64(count) && total <= maximumSafeJSONInteger && len(token) <= 512
}

// contextStateInput maps empty input to UNSPECIFIED and accepts only exact
// positive enum suffixes, rejecting unknown or differently cased state names.
// contextStateInput переводит пустой ввод в UNSPECIFIED и допускает только точные
// положительные enum-суффиксы, отклоняя неизвестные имена и другой регистр.
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
