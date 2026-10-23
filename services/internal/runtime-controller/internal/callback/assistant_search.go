package callback

import (
	"context"
	"errors"
	"net/url"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantResourceSearchTool declares a closed, query-only metadata search tool with ten-result bounds and canonical UI routes.
// Artifact discovery describes navigation only; file content still requires the separate project catalog and exact read pins.
// assistantResourceSearchTool объявляет закрытый инструмент поиска метаданных с единственным запросом, пределом десять результатов и каноническими маршрутами интерфейса.
// Обнаружение артефакта даёт только навигацию; содержимое требует отдельного проектного каталога файлов и точных привязок чтения.
func assistantResourceSearchTool() map[string]any {
	query := stringSchema(2, 160)
	query["description"] = assistantSearchQueryDescription
	return map[string]any{
		"name":        "find_platform_resources",
		"description": "Search resource metadata visible to the initiating user; never returns secret values. Use exact result refs. For another project, ask the user to open its route before proposing changes. Search never changes context or grants access. ARTIFACT results are metadata and UI links only: discovery does not grant file content access. To verify contents, use search_files by file name with purpose PROJECT in your own current file catalog, then read_file with exact returned pins. If that PROJECT catalog/grant is missing or the exact file is absent, state that you cannot verify its contents; never infer access from plan approval, previous turns or a UI link.",
		"inputSchema": objectSchema([]string{"query"}, map[string]any{"query": query}),
		"outputSchema": objectSchema([]string{"current_project_ref", "results", "truncated"}, map[string]any{
			"current_project_ref": map[string]any{"type": "string"},
			"results": map[string]any{"type": "array", "maxItems": maximumAssistantSearchResults,
				"items": objectSchema([]string{"kind", "ref", "project_ref", "title", "subtitle", "state", "route", "requires_context_switch"}, map[string]any{
					"kind": enumSchema("PROJECT", "AGENT", "WORKFLOW", "RUN", "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", "SCHEDULE", "INTEGRATION", "SECRET", "ARTIFACT"), "ref": opaqueRefSchema(),
					"project_ref": stringSchema(0, 96), "title": stringSchema(0, 160), "subtitle": stringSchema(0, 160),
					"state": map[string]any{"type": "string"}, "route": map[string]any{"type": "string"},
					"requires_context_switch": map[string]any{"type": "boolean"},
				}),
			},
			"truncated": map[string]any{"type": "boolean"},
		}),
	}
}

// maximumAssistantSearchResults caps the owner search response and advertised output array at ten entries; it does not implement pagination.
// maximumAssistantSearchResults ограничивает ответ владельца и объявленный массив результатов десятью элементами; постраничная загрузка здесь не реализуется.
const maximumAssistantSearchResults = 10

const (
	// assistantSearchQueryDescription documents the trimmed Unicode query bound shared by the tool schema and recovery guidance, not a full-inventory request.
	// assistantSearchQueryDescription описывает предел Unicode-запроса после обрезки пробелов для схемы инструмента и подсказки восстановления, а не запрос полного реестра.
	assistantSearchQueryDescription = "Specific name or opaque ref: 2..160 trimmed Unicode codepoints, not blank. Search is not full inventory."
	// assistantSearchInputInvalidCode identifies recoverable local input rejection in the MCP response; it is not an owner authorization result.
	// assistantSearchInputInvalidCode обозначает исправимый локальный отказ входных данных в ответе MCP; это не результат авторизации у владельца.
	assistantSearchInputInvalidCode = "SEARCH_INPUT_INVALID"
	// assistantSearchInputInvalidGuidance allows at most one corrected query and directs unavailable authoritative discovery back to the owner instead of guessed references.
	// assistantSearchInputInvalidGuidance разрешает не более одного исправленного запроса и при недоступном авторитетном поиске направляет к владельцу, а не к выдуманным ссылкам.
	assistantSearchInputInvalidGuidance = "Retry at most once with only query: a specific resource name or exact opaque ref containing 2 to 160 Unicode codepoints after trimming surrounding whitespace. Do not send an empty query, task text, kind, ref or filters as separate fields. For supported configuration discovery, use get_configuration_catalog with the current schema; its agent list is turn-pinned and does not prove a complete fresh inventory. If authoritative discovery is unavailable, ask the owner for readback instead of guessing resources."
	// assistantSearchFailureMessage is the fixed public error text that never includes query contents, result data or the wrapped owner failure.
	// assistantSearchFailureMessage задаёт неизменный публичный текст ошибки без содержимого запроса, результатов или вложенной ошибки владельца.
	assistantSearchFailureMessage = "assistant resource search failed"
	// assistantSearchContextInvalid labels local rejection of a non-assistant or missing lease/fence/generation before any owner search.
	// assistantSearchContextInvalid обозначает локальный отказ для не-помощника или отсутствующих аренды, ограждения либо поколения до обращения к владельцу.
	assistantSearchContextInvalid = "assistant_search_context_invalid"
	// assistantSearchInputShapeInvalid labels unexpected query argument keys without recording their names or values in the diagnostic class.
	// assistantSearchInputShapeInvalid обозначает лишние ключи аргументов поиска без включения их имён и значений в диагностический класс.
	assistantSearchInputShapeInvalid = "assistant_search_input_shape_invalid"
	// assistantSearchQueryInvalid labels a non-string or out-of-range trimmed query before the search RPC; it does not expose the rejected value.
	// assistantSearchQueryInvalid обозначает нестроковый запрос либо нарушение предела после обрезки пробелов до поискового RPC, не раскрывая отклонённое значение.
	assistantSearchQueryInvalid = "assistant_search_query_invalid"
	// assistantSearchOwnerFailed labels an actual search RPC error while preserving its cause for internal code classification, not public rendering.
	// assistantSearchOwnerFailed обозначает ошибку поискового RPC с сохранением причины для внутренней классификации кода, но не публичного вывода.
	assistantSearchOwnerFailed = "assistant_search_owner_failed"
	// assistantSearchResponseShapeInvalid labels nil, mixed response arms or an oversized result list before metadata projection.
	// assistantSearchResponseShapeInvalid обозначает пустой ответ, смешанные варианты ответа или слишком большой список до формирования проекции метаданных.
	assistantSearchResponseShapeInvalid = "assistant_search_response_shape_invalid"
	// assistantSearchResultIdentityInvalid labels malformed resource/project references or a project-bound integration result before constructing its route.
	// assistantSearchResultIdentityInvalid обозначает некорректные ссылки ресурса или проекта либо проектную привязку интеграции до построения маршрута.
	assistantSearchResultIdentityInvalid = "assistant_search_result_identity_invalid"
	// assistantSearchResultRouteInvalid labels an unsupported result kind or inconsistent project identity that cannot produce a canonical route.
	// assistantSearchResultRouteInvalid обозначает неподдерживаемый вид результата либо несовпадающую идентичность проекта, для которых нельзя построить канонический маршрут.
	assistantSearchResultRouteInvalid = "assistant_search_result_route_invalid"
)

// assistantSearchError wraps a local closed diagnostic class and optional owner cause while exposing only fixed public text.
// assistantSearchError хранит локальный закрытый диагностический класс и необязательную причину владельца, раскрывая только фиксированный публичный текст.
type assistantSearchError struct {
	class string
	cause error
}

// Error returns the fixed search failure message regardless of class or wrapped owner content.
// Error возвращает фиксированное сообщение ошибки поиска независимо от класса и содержимого вложенной причины владельца.
func (err *assistantSearchError) Error() string { return assistantSearchFailureMessage }

// Unwrap returns the optional internal cause for errors.As/status classification and handles a nil receiver without panic.
// Unwrap возвращает необязательную внутреннюю причину для errors.As и классификации статуса; nil-получатель обрабатывается без паники.
func (err *assistantSearchError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// assistantSearchFailureClass accepts only seven registered classes from a non-nil typed local wrapper; unknown classes and remote text produce no class.
// assistantSearchFailureClass принимает только семь зарегистрированных классов из непустой локальной типизированной оболочки; неизвестные классы и удалённый текст класса не дают.
func assistantSearchFailureClass(err error) string {
	var failure *assistantSearchError
	if !errors.As(err, &failure) || failure == nil {
		return ""
	}
	switch failure.class {
	case assistantSearchContextInvalid, assistantSearchInputShapeInvalid, assistantSearchQueryInvalid,
		assistantSearchOwnerFailed, assistantSearchResponseShapeInvalid, assistantSearchResultIdentityInvalid,
		assistantSearchResultRouteInvalid:
		return failure.class
	default:
		return ""
	}
}

// findPlatformResources validates assistant lease coordinates and a single trimmed 2..160-rune query before the bounded owner RPC.
// Checks response arms, count, references and closed route kinds, then returns truncated titles and navigation hints.
// The owner resolves visibility; metadata, input locators and context-switch hints grant neither content access nor mutations.
// findPlatformResources проверяет координаты аренды помощника и единственный запрос длиной 2..160 рун после обрезки пробелов до ограниченного по времени RPC владельца.
// Проверяет варианты ответа, количество, ссылки и закрытый набор маршрутов, затем возвращает сокращённые названия и навигационные подсказки.
// Видимость определяет владелец; метаданные, входные ссылки и подсказки перехода не дают доступа к содержимому или изменениям.
func (server *Server) findPlatformResources(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if !input.IsAssistant() || input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, &assistantSearchError{class: assistantSearchContextInvalid}
	}
	if !onlyKeys(arguments, "query") {
		return nil, &assistantSearchError{class: assistantSearchInputShapeInvalid}
	}
	query, ok := arguments["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || len([]rune(query)) < 2 || len([]rune(query)) > 160 {
		return nil, &assistantSearchError{class: assistantSearchQueryInvalid}
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Query: query,
	})
	if err != nil {
		return nil, &assistantSearchError{class: assistantSearchOwnerFailed, cause: err}
	}
	if response == nil || response.GetAssistantTaskSession() != nil || response.GetAssistantConfigurationCatalog() != nil || len(response.GetDefinitions()) != 0 || response.GetNextDefinitionOffset() != 0 || len(response.GetResults()) > maximumAssistantSearchResults {
		return nil, &assistantSearchError{class: assistantSearchResponseShapeInvalid}
	}
	items := make([]map[string]any, 0, len(response.GetResults()))
	for _, item := range response.GetResults() {
		if item == nil || !validAssistantResourceRef(item.GetRef()) ||
			(item.GetKind() != controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && !validAssistantResourceRef(item.GetProjectRef())) ||
			(item.GetKind() == controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && item.GetProjectRef() != "") {
			return nil, &assistantSearchError{class: assistantSearchResultIdentityInvalid}
		}
		kind, route := assistantResourceRoute(item)
		if kind == "" {
			return nil, &assistantSearchError{class: assistantSearchResultRouteInvalid}
		}
		items = append(items, map[string]any{
			"kind": kind, "ref": item.GetRef(), "project_ref": item.GetProjectRef(),
			"title": truncateRunes(item.GetTitle(), 160), "subtitle": truncateRunes(item.GetSubtitle(), 160),
			"state": item.GetState(), "route": route,
			"requires_context_switch": assistantSearchRequiresContextSwitch(input, kind, item.GetRef(), item.GetProjectRef(), route),
		})
	}
	return map[string]any{"current_project_ref": input.ProjectRef, "results": items, "truncated": response.GetTruncated()}, nil
}

// assistantSearchRequiresContextSwitch compares project and selected integration/agent/workflow context with the result to suggest a screen change.
// For agents/workflows the current URL must be relative and have the exact path; query parameters are ignored.
// This hint does not change the screen, selected context, allowed operations or server authority.
// assistantSearchRequiresContextSwitch сопоставляет проект и выбранный контекст интеграции, сотрудника либо процесса с результатом для подсказки перехода.
// Для сотрудников и процессов текущий URL должен быть относительным с точным путём; параметры запроса не учитываются.
// Подсказка не изменяет экран, выбранный контекст, разрешённые операции или серверные полномочия.
func assistantSearchRequiresContextSwitch(input runtimecontract.RunnerInput, kind, ref, projectRef, route string) bool {
	if projectRef != "" && projectRef != input.ProjectRef {
		return true
	}
	current := input.AssistantContext
	switch kind {
	case "INTEGRATION":
		return current == nil || current.EntityKind != "INTEGRATION_CONNECTION" || current.EntityRef != ref
	case "AGENT", "WORKFLOW":
		if current == nil || current.EntityKind != kind || current.EntityRef != ref {
			return true
		}
		currentRoute, err := url.Parse(current.Route)
		return err != nil || currentRoute.IsAbs() || currentRoute.Host != "" || currentRoute.Path != route
	default:
		return false
	}
}

// validAssistantResourceRef recognizes an 8..96-byte ASCII alphanumeric/underscore/hyphen locator; existence and visibility remain owner checks.
// validAssistantResourceRef распознаёт ссылку длиной 8..96 байт из ASCII-букв, цифр, подчёркивания и дефиса; существование и видимость проверяет владелец.
func validAssistantResourceRef(ref string) bool {
	return len(ref) <= 96 && safeInvocationRef(ref)
}

// assistantResourceRoute maps ten closed result kinds to escaped local UI routes after caller-side reference validation.
// Project refs must match their resource identity, and integrations must have no project; unknown kinds return no route.
// assistantResourceRoute преобразует десять закрытых видов результата в экранированные локальные маршруты интерфейса после проверки ссылок вызывающей стороной.
// Для проекта ссылка должна совпадать с идентичностью ресурса, у интеграции не должно быть проекта; неизвестный вид маршрута не даёт.
func assistantResourceRoute(item *controlplanev1.SearchResult) (string, string) {
	project := "/projects/" + url.PathEscape(item.GetProjectRef())
	switch item.GetKind() {
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_PROJECT:
		if item.GetRef() != item.GetProjectRef() {
			return "", ""
		}
		return "PROJECT", project
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT:
		return "AGENT", project + "/agents/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_WORKFLOW:
		return "WORKFLOW", project + "/workflows/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_RUN:
		return "RUN", project + "/runs/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT:
		return "ARTIFACT", project + "/files?artifactRef=" + url.QueryEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_ROLE_IMAGE:
		return "ROLE_IMAGE", project + "/role-images/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_RUNTIME_ENVIRONMENT:
		return "RUNTIME_ENVIRONMENT", project + "/environments/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SCHEDULE:
		return "SCHEDULE", project + "/automations?scheduleRef=" + url.QueryEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SECRET:
		return "SECRET", project + "/secrets"
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION:
		if item.GetProjectRef() != "" {
			return "", ""
		}
		return "INTEGRATION", "/integrations?connectionRef=" + url.QueryEscape(item.GetRef())
	default:
		return "", ""
	}
}
