package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// platformSnapshotPageSize bounds ordinary initial owner catalogs at fifty records; pagination cursors remain owner-produced and later pages are not fetched here.
	// platformSnapshotPageSize ограничивает обычные начальные каталоги пятьюдесятью записями; курсоры формирует владелец, следующие страницы здесь не загружаются.
	platformSnapshotPageSize = 50
	// assistantSnapshotInitialPageSize starts an unhinted assistant snapshot at twenty-five whole conversations before bounded size adaptation.
	// assistantSnapshotInitialPageSize начинает снимок помощника без подсказки с двадцати пяти целых диалогов до ограниченного подбора размера.
	assistantSnapshotInitialPageSize = 25
)

// assistantSnapshotPageHint retains only the last fitting page size for one socket and exact organization/project. It contains no owner payload, credentials or permission decision and cannot replace fresh reads.
// assistantSnapshotPageHint хранит только последний поместившийся размер страницы одного сокета и точных организации и проекта. Ответ владельца, credentials и решение о правах отсутствуют; свежие чтения не заменяются.
type assistantSnapshotPageHint struct {
	organizationRef string
	projectRef      string
	pageSize        int32
}

const (
	// platformSnapshotReadFailure is the fixed platform dependency-read log message; dynamic causes are represented by closed diagnostic fields instead of raw text.
	// platformSnapshotReadFailure — фиксированное сообщение отказа чтения зависимостей платформы; причины представлены закрытыми полями вместо сырого текста.
	platformSnapshotReadFailure = "platform bootstrap snapshot read failed"
	// platformSnapshotValidationFailure identifies a rejected typed snapshot projection without serializing the invalid owner payload.
	// platformSnapshotValidationFailure обозначает отклонённую типизированную проекцию снимка без сериализации неверного ответа владельца.
	platformSnapshotValidationFailure = "platform bootstrap snapshot validation failed"
	// platformSnapshotSizeFailure identifies full-frame overflow after bounded adaptation, recording size/class rather than snapshot contents.
	// platformSnapshotSizeFailure обозначает превышение полного кадра после ограниченного подбора, записывая размер и класс вместо содержимого снимка.
	platformSnapshotSizeFailure = "platform snapshot frame size exceeded"
)

var (
	// errPlatformSnapshotInvalid rejects malformed/unsupported snapshot projections before that envelope is admitted to the outbound queue.
	// errPlatformSnapshotInvalid отклоняет некорректную или неподдерживаемую проекцию до допуска этого конверта в исходящую очередь.
	errPlatformSnapshotInvalid = errors.New("platform snapshot projection is invalid")
	// errPlatformSnapshotSize reports that a complete envelope still exceeds the fixed frame limit; callers must not silently truncate its conversations or fields.
	// errPlatformSnapshotSize сообщает, что полный конверт всё ещё превышает предел кадра; вызывающий не должен молча усекать диалоги или поля.
	errPlatformSnapshotSize = errors.New("platform snapshot frame exceeds maximum size")
)

// typedPlatformSnapshot JSON-roundtrips a server-built map into the generated payload with unknown-field rejection and validates which top-level sections are present for the exact kind. It does not itself authorize resources or validate every business-field invariant.
// typedPlatformSnapshot преобразует серверную map через JSON в generated payload с отказом на неизвестных полях и проверяет набор верхнеуровневых разделов точного вида. Права на ресурсы и все бизнес-инварианты отдельных полей здесь не проверяются.
func typedPlatformSnapshot(kind string, value map[string]any) (generated.PlatformSnapshotPayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return generated.PlatformSnapshotPayload{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var snapshot generated.PlatformSnapshotPayload
	if err := decoder.Decode(&snapshot); err != nil {
		return generated.PlatformSnapshotPayload{}, err
	}
	present := 0
	for _, field := range []bool{
		snapshot.Catalog != nil,
		snapshot.SelectedProject != nil,
		snapshot.Overview != nil,
		snapshot.Definitions != nil,
		snapshot.Connections != nil,
		snapshot.Assistant != nil,
		snapshot.Conversations != nil,
		snapshot.Bootstrap != nil,
	} {
		if field {
			present++
		}
	}
	valid := false
	switch kind {
	case "PROJECT":
		valid = snapshot.Catalog != nil && snapshot.Overview != nil && (present == 2 || (present == 3 && snapshot.SelectedProject != nil))
	case "AGENT", "INSTRUCTIONS", "RUN", "ARTIFACT":
		valid = snapshot.Catalog != nil && snapshot.Overview != nil && present == 2
	case "WORKFLOW", "SCHEDULE", "MEMBERSHIP", "PLATFORM_MEMBERSHIP", "ROLE_IMAGE_RECIPE", "RUNTIME_ENVIRONMENT", "PROVIDER_ACCOUNT", "RUNTIME_SECRET", "MANAGED_CONFIGURATION", "RUNTIME_SELECTION":
		valid = snapshot.Catalog != nil && present == 1
	case "INTEGRATION_CONNECTION", "INTEGRATION_GRANT":
		valid = snapshot.Definitions != nil && snapshot.Connections != nil && present == 2
	case "SYSTEM_ASSISTANT":
		valid = snapshot.Assistant != nil && snapshot.Conversations != nil && snapshot.Bootstrap != nil && present == 3
	}
	if !valid {
		return generated.PlatformSnapshotPayload{}, errors.New("platform snapshot payload does not match kind")
	}
	return snapshot, nil
}

// platformBootstrapKinds defines the fixed ordered initial catalog kinds; denied kinds are skipped by bootstrap rather than synthesized from a client's request.
// platformBootstrapKinds задаёт фиксированный порядок начальных видов каталогов; запрещённые виды bootstrap пропускает, а не создаёт по запросу клиента.
var platformBootstrapKinds = []string{
	"PROJECT",
	"AGENT",
	"WORKFLOW",
	"RUN",
	"ARTIFACT",
	"SCHEDULE",
	"INTEGRATION_CONNECTION",
	"MEMBERSHIP",
	"PLATFORM_MEMBERSHIP",
	"SYSTEM_ASSISTANT",
	"ROLE_IMAGE_RECIPE",
	"RUNTIME_ENVIRONMENT",
	"PROVIDER_ACCOUNT",
	"RUNTIME_SECRET",
	"MANAGED_CONFIGURATION",
	"RUNTIME_SELECTION",
}

// platformPage returns a fresh first-page request with the common fifty-item budget and no fabricated owner pagination token.
// platformPage возвращает новый запрос первой страницы с общим пределом пятьдесят и без выдуманного токена пагинации владельца.
func platformPage() *controlplanev1.PageRequest {
	return &controlplanev1.PageRequest{PageSize: platformSnapshotPageSize}
}

// projectSnapshotPart converts one protobuf response through the HTTP-safe projection and localizes only explicit safe errors, propagating projection failure without owner writes.
// projectSnapshotPart преобразует один protobuf-ответ через безопасную HTTP-проекцию и локализует только явные безопасные ошибки, возвращая отказ проекции без записей у владельца.
func projectSnapshotPart(message proto.Message, localize func(string) string) (map[string]any, error) {
	projection, err := httptransport.ProtoMap(message)
	if err != nil {
		return nil, err
	}
	httptransport.LocalizeSafeErrors(projection, localize)
	return projection, nil
}

// snapshotWithCatalog wraps a successfully projected protobuf response in the catalog section, returning no map on conversion failure.
// snapshotWithCatalog помещает успешно спроецированный protobuf-ответ в раздел catalog и не возвращает map при ошибке преобразования.
func snapshotWithCatalog(message proto.Message, localize func(string) string) (map[string]any, error) {
	catalog, err := projectSnapshotPart(message, localize)
	if err != nil {
		return nil, err
	}
	return map[string]any{"catalog": catalog}, nil
}

// projectSpeechAvailability combines owner STT availability with the configured speech-client projection and formats an optional UTC validity ceiling into the generated browser type; it does not transcribe audio.
// projectSpeechAvailability объединяет доступность STT владельца с проекцией настроенного speech client и форматирует необязательный срок UTC в generated browser type; аудио не транскрибируется.
func (server *Server) projectSpeechAvailability(ctx context.Context, owner *controlplanev1.SpeechTranscriptionAvailability) *generated.SpeechTranscriptionAvailability {
	availability := httptransport.SpeechAvailability(ctx, server.speech, owner)
	projected := &generated.SpeechTranscriptionAvailability{
		Available: availability.Available,
		Reason:    string(availability.Reason),
	}
	if availability.ValidUntil != nil {
		value := availability.ValidUntil.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		projected.ValidUntil = &value
	}
	return projected
}

// speechAvailabilityMap copies nonnil generated availability into the exact available/reason/optional-validUntil map expected by bootstrap projection.
// speechAvailabilityMap копирует ненулевую generated доступность в точную map available/reason/необязательный validUntil для bootstrap-проекции.
func speechAvailabilityMap(value *generated.SpeechTranscriptionAvailability) map[string]any {
	projected := map[string]any{
		"available": value.Available,
		"reason":    value.Reason,
	}
	if value.ValidUntil != nil {
		projected["validUntil"] = *value.ValidUntil
	}
	return projected
}

// scopedProjectContext preserves an empty project context or attaches the explicit transport selector through controlplaneclient; attaching a selector does not grant project access.
// scopedProjectContext сохраняет контекст без проекта либо добавляет явный транспортный селектор через controlplaneclient; добавление селектора прав не выдаёт.
func scopedProjectContext(ctx context.Context, projectRef string) (context.Context, error) {
	if projectRef == "" {
		return ctx, nil
	}
	return controlplaneclient.WithProjectReference(ctx, projectRef)
}

// projectPlatformSnapshot reads the requested owner-backed catalog with the common first-page budget, delegating kind-specific scope, projection and errors to projectPlatformSnapshotPage.
// projectPlatformSnapshot читает каталог владельца с общим бюджетом первой страницы, передавая выбор области, проекцию и ошибки вида в projectPlatformSnapshotPage.
func (server *Server) projectPlatformSnapshot(ctx context.Context, kind, projectRef string, localize func(string) string) (map[string]any, error) {
	return server.projectPlatformSnapshotPage(ctx, kind, projectRef, localize, platformSnapshotPageSize)
}

// projectPlatformSnapshotPage composes one kind's fresh typed owner reads, preserving explicit project context and separate organizational catalogs/cursors. It adds overview/gates, access registries, assistant/bootstrap and runtime/provider data only in their declared branches; financial summary is mandatory even when the typed provider-catalog size outcome permits a reduced catalog. Managed configurations keep four kind-specific page records. Optional denied/not-found parts are handled only at their existing branches; other failures abort this kind without inventing data. It returns projections, not authority or durable state.
// projectPlatformSnapshotPage объединяет свежие типизированные чтения одного вида у владельцев, сохраняя явный контекст проекта и отдельные организационные каталоги с курсорами. Обзор, подтверждения, реестры доступа, помощник, bootstrap, runtime и provider добавляются только в своих ветвях; финансовая сводка обязательна даже при допустимом сокращении каталога по типизированному исходу размера. Управляемые конфигурации сохраняют четыре записи страниц по видам. Необязательные отказанные или отсутствующие части обрабатываются только в прежних ветвях; другие ошибки прерывают этот вид без выдуманных данных. Результат — проекции, а не полномочия или устойчивое состояние.
func (server *Server) projectPlatformSnapshotPage(ctx context.Context, kind, projectRef string, localize func(string) string, assistantPageSize int32) (map[string]any, error) {
	scoped, err := scopedProjectContext(ctx, projectRef)
	if err != nil {
		return nil, err
	}
	withOverview := func(snapshot map[string]any) (map[string]any, error) {
		overview, readErr := server.query.GetOverview(scoped, &controlplanev1.GetOverviewRequest{ProjectRef: projectRef})
		if readErr != nil {
			if kind == "RUN" {
				return nil, withPlatformSnapshotReadStage(platformSnapshotOverviewGetStage, readErr)
			}
			return nil, readErr
		}
		projected, projectErr := projectSnapshotPart(overview, localize)
		if projectErr != nil {
			if kind == "RUN" {
				return nil, withPlatformSnapshotReadStage(platformSnapshotRunProjectStage, projectErr)
			}
			return nil, projectErr
		}
		snapshot["overview"] = projected
		return snapshot, nil
	}

	switch kind {
	case "PROJECT":
		response, readErr := server.query.ListProjects(ctx, &controlplanev1.ListProjectsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		snapshot, projectErr := snapshotWithCatalog(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		trash, trashErr := server.query.ListTrashedProjects(ctx, &controlplanev1.ListTrashedProjectsRequest{Page: platformPage()})
		if trashErr == nil {
			trashCatalog, projectionErr := projectSnapshotPart(trash, localize)
			if projectionErr != nil {
				return nil, projectionErr
			}
			catalog := snapshot["catalog"].(map[string]any)
			catalog["trashedProjects"] = trashCatalog["projects"]
			catalog["trashPage"] = trashCatalog["page"]
		} else if code := status.Code(trashErr); code != codes.NotFound && code != codes.PermissionDenied {
			return nil, trashErr
		}
		if projectRef != "" {
			selected, selectedErr := server.query.GetProject(scoped, &controlplanev1.GetProjectRequest{ProjectRef: projectRef})
			if selectedErr != nil {
				if code := status.Code(selectedErr); code != codes.NotFound && code != codes.PermissionDenied {
					return nil, selectedErr
				}
				overview, overviewErr := server.query.GetOverview(ctx, &controlplanev1.GetOverviewRequest{})
				if overviewErr != nil {
					return nil, overviewErr
				}
				overviewProjection, projectionErr := projectSnapshotPart(overview, localize)
				if projectionErr != nil {
					return nil, projectionErr
				}
				snapshot["overview"] = overviewProjection
				return snapshot, nil
			}
			selectedProjection, projectionErr := projectSnapshotPart(selected, localize)
			if projectionErr != nil {
				return nil, projectionErr
			}
			snapshot["selectedProject"] = selectedProjection
		}
		return withOverview(snapshot)
	case "AGENT", "INSTRUCTIONS":
		response, readErr := server.query.ListAgents(scoped, &controlplanev1.ListAgentsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		snapshot, projectErr := snapshotWithCatalog(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		return withOverview(snapshot)
	case "WORKFLOW":
		response, readErr := server.query.ListWorkflows(scoped, &controlplanev1.ListWorkflowsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	case "RUN":
		response, readErr := server.query.ListRuns(scoped, &controlplanev1.ListRunsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotRunsListStage, readErr)
		}
		catalog, projectErr := projectSnapshotPart(response, localize)
		if projectErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotRunProjectStage, projectErr)
		}
		gates, readErr := server.query.ListOwnerGates(scoped, &controlplanev1.ListOwnerGatesRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotOwnerGatesListStage, readErr)
		}
		gateCatalog, projectErr := projectSnapshotPart(gates, localize)
		if projectErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotRunProjectStage, projectErr)
		}
		catalog["gates"] = gateCatalog["gates"]
		catalog["gatesPage"] = gateCatalog["page"]
		snapshot := map[string]any{"catalog": catalog}
		return withOverview(snapshot)
	case "ARTIFACT":
		response, readErr := server.query.ListArtifacts(scoped, &controlplanev1.ListArtifactsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		snapshot, projectErr := snapshotWithCatalog(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		return withOverview(snapshot)
	case "SCHEDULE":
		response, readErr := server.query.ListSchedules(scoped, &controlplanev1.ListSchedulesRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	case "INTEGRATION_CONNECTION", "INTEGRATION_GRANT":
		definitions, readErr := server.query.ListIntegrationDefinitions(ctx, &controlplanev1.ListIntegrationDefinitionsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		connections, readErr := server.query.ListIntegrationConnections(ctx, &controlplanev1.ListIntegrationConnectionsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		definitionProjection, projectErr := projectSnapshotPart(definitions, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		connectionProjection, projectErr := projectSnapshotPart(connections, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		return map[string]any{"definitions": definitionProjection, "connections": connectionProjection}, nil
	case "MEMBERSHIP":
		catalog := map[string]any{}
		if projectRef != "" {
			response, readErr := server.query.ListProjectMemberships(scoped, &controlplanev1.ListProjectMembershipsRequest{ProjectRef: projectRef, Page: platformPage()})
			if readErr != nil {
				return nil, readErr
			}
			membershipCatalog, projectionErr := projectSnapshotPart(response, localize)
			if projectionErr != nil {
				return nil, projectionErr
			}
			for key, value := range membershipCatalog {
				catalog[key] = value
			}
		} else {
			catalog["memberships"] = []any{}
			catalog["nextActions"] = []any{}
			catalog["page"] = map[string]any{}
		}
		permissions, readErr := server.access.ListPermissionRegistry(ctx, &controlplanev1.ListPermissionRegistryRequest{})
		if readErr != nil {
			return nil, readErr
		}
		subjects, readErr := server.access.ListAccessSubjects(ctx, &controlplanev1.ListAccessSubjectsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		groups, readErr := server.access.ListOIDCGroups(ctx, &controlplanev1.ListOIDCGroupsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		roles, readErr := server.access.ListAccessRoles(ctx, &controlplanev1.ListAccessRolesRequest{Page: platformPage(), IncludeArchived: true})
		if readErr != nil {
			return nil, readErr
		}
		bindings, readErr := server.access.ListAccessBindings(ctx, &controlplanev1.ListAccessBindingsRequest{Page: platformPage(), ProjectRef: projectRef})
		if readErr != nil {
			return nil, readErr
		}
		projections := []struct {
			message proto.Message
			apply   func(map[string]any)
		}{
			{permissions, func(value map[string]any) { catalog["permissions"] = value["permissions"] }},
			{subjects, func(value map[string]any) {
				catalog["accessSubjects"], catalog["accessSubjectsPage"] = value["subjects"], value["page"]
			}},
			{groups, func(value map[string]any) {
				catalog["oidcGroups"], catalog["oidcGroupsPage"] = value["groups"], value["page"]
			}},
			{roles, func(value map[string]any) {
				catalog["accessRoles"], catalog["accessRolesPage"] = value["roles"], value["page"]
			}},
			{bindings, func(value map[string]any) {
				catalog["accessBindings"], catalog["accessBindingsPage"] = value["bindings"], value["page"]
			}},
		}
		for _, item := range projections {
			projection, projectionErr := projectSnapshotPart(item.message, localize)
			if projectionErr != nil {
				return nil, projectionErr
			}
			item.apply(projection)
		}
		return map[string]any{"catalog": catalog}, nil
	case "PLATFORM_MEMBERSHIP":
		response, readErr := server.query.ListPlatformMemberships(ctx, &controlplanev1.ListPlatformMembershipsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	case "SYSTEM_ASSISTANT":
		assistant, readErr := server.assistant.GetSystemAssistant(ctx, &controlplanev1.GetSystemAssistantRequest{})
		if readErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotAssistantGetStage, readErr)
		}
		conversations, readErr := server.assistant.ListAssistantConversations(scoped, &controlplanev1.ListAssistantConversationsRequest{ProjectRef: projectRef, Page: &controlplanev1.PageRequest{PageSize: assistantPageSize}})
		if readErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotConversationListStage, readErr)
		}
		bootstrap, readErr := server.query.GetBootstrapState(ctx, &controlplanev1.GetBootstrapStateRequest{})
		if readErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotBootstrapGetStage, readErr)
		}
		assistantProjection, projectErr := projectSnapshotPart(assistant, localize)
		if projectErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotAssistantProjectStage, projectErr)
		}
		conversationProjection, projectErr := projectSnapshotPart(conversations, localize)
		if projectErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotAssistantProjectStage, projectErr)
		}
		bootstrapProjection, projectErr := projectSnapshotPart(bootstrap, localize)
		if projectErr != nil {
			return nil, withPlatformSnapshotReadStage(platformSnapshotAssistantProjectStage, projectErr)
		}
		state, ok := bootstrapProjection["state"].(map[string]any)
		if !ok {
			return nil, withPlatformSnapshotReadStage(platformSnapshotAssistantProjectStage, errors.New("bootstrap state projection is invalid"))
		}
		state["speechTranscription"] = speechAvailabilityMap(server.projectSpeechAvailability(ctx, bootstrap.GetState().GetSpeechTranscription()))
		return map[string]any{"assistant": assistantProjection, "conversations": conversationProjection, "bootstrap": bootstrapProjection}, nil
	case "ROLE_IMAGE_RECIPE":
		response := &controlplanev1.ListRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{}}
		if projectRef != "" {
			var readErr error
			response, readErr = server.roleImages.ListRoleImageRecipes(scoped, &controlplanev1.ListRoleImageRecipesRequest{ProjectRef: projectRef, Page: platformPage()})
			if readErr != nil {
				return nil, readErr
			}
		}
		environments, readErr := server.roleImages.ListRoleEnvironments(scoped, &controlplanev1.ListRoleEnvironmentsRequest{})
		if readErr != nil {
			return nil, readErr
		}
		catalog, projectErr := projectSnapshotPart(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		environmentCatalog, projectErr := projectSnapshotPart(environments, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		catalog["roleEnvironments"] = environmentCatalog["environments"]
		if err := server.projectOrganizationRecipes(ctx, catalog, localize); err != nil {
			return nil, err
		}
		return map[string]any{"catalog": catalog}, nil
	case "RUNTIME_ENVIRONMENT":
		response, readErr := server.query.ListRuntimeEnvironmentSets(scoped, &controlplanev1.ListRuntimeEnvironmentSetsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		catalog, projectErr := projectSnapshotPart(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		roleEnvironments, readErr := server.roleImages.ListRoleEnvironments(scoped, &controlplanev1.ListRoleEnvironmentsRequest{})
		if readErr != nil {
			return nil, readErr
		}
		roleEnvironmentCatalog, projectErr := projectSnapshotPart(roleEnvironments, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		catalog["roleEnvironments"] = roleEnvironmentCatalog["environments"]
		runtimes, readErr := server.query.ListRuntimeSelections(ctx, &controlplanev1.ListRuntimeSelectionsRequest{})
		if readErr != nil {
			return nil, readErr
		}
		runtimeCatalog, projectErr := projectSnapshotPart(runtimes, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		catalog["runtimes"] = runtimeCatalog["runtimes"]
		return map[string]any{"catalog": catalog}, nil
	case "PROVIDER_ACCOUNT":
		finance, financeErr := server.query.ListProviderAccounts(scoped, &controlplanev1.ListProviderAccountsRequest{FinancialSummaryOnly: true})
		if financeErr != nil {
			return nil, financeErr
		}
		if finance.GetFinancialSummary() == nil {
			return nil, errPlatformSnapshotInvalid
		}
		financeMap, financeErr := projectSnapshotPart(finance.GetFinancialSummary(), localize)
		if financeErr != nil {
			return nil, financeErr
		}
		response, readErr := server.query.ListProviderAccounts(scoped, &controlplanev1.ListProviderAccountsRequest{Page: &controlplanev1.PageRequest{PageSize: assistantPageSize}})
		if readErr != nil {
			if providerCatalogOversized(readErr) {
				return map[string]any{"catalog": map[string]any{"financialSummary": financeMap, "providerCatalogUnavailable": true}}, nil
			}
			return nil, readErr
		}
		definitions, readErr := server.query.ListProviderDefinitions(ctx, &controlplanev1.ListProviderDefinitionsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		catalog, projectErr := projectSnapshotPart(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		definitionCatalog, projectErr := projectSnapshotPart(definitions, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		catalog["financialSummary"] = financeMap
		catalog["providerDefinitions"] = definitionCatalog["definitions"]
		catalog["providerDefinitionsPage"] = definitionCatalog["page"]
		runtimes, readErr := server.query.ListRuntimeSelections(ctx, &controlplanev1.ListRuntimeSelectionsRequest{})
		if readErr != nil {
			return nil, readErr
		}
		runtimeCatalog, projectErr := projectSnapshotPart(runtimes, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		catalog["runtimes"] = runtimeCatalog["runtimes"]
		return map[string]any{"catalog": catalog}, nil
	case "RUNTIME_SECRET":
		response, readErr := server.query.ListRuntimeSecrets(scoped, &controlplanev1.ListRuntimeSecretsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		snapshot, err := snapshotWithCatalog(response, localize)
		if err != nil {
			return nil, err
		}
		if err := server.projectOrganizationSecrets(ctx, snapshot["catalog"].(map[string]any), localize); err != nil {
			return nil, err
		}
		return snapshot, nil
	case "MANAGED_CONFIGURATION":
		catalog := map[string]any{
			"managedConfigurations":     []any{},
			"managedConfigurationPages": []any{},
		}
		configurationKinds := []struct {
			name string
			kind controlplanev1.ManagedConfigurationKind
		}{
			{"PROMPT_TEMPLATE", controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE},
			{"ROLE_IMAGE", controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE},
			{"INTEGRATION_DEFINITION", controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION},
			{"SYSTEM_STT", controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT},
		}
		for _, configurationKind := range configurationKinds {
			configurationProjectRef := ""
			if configurationKind.name == "PROMPT_TEMPLATE" || configurationKind.name == "ROLE_IMAGE" {
				if projectRef == "" {
					catalog["managedConfigurationPages"] = append(catalog["managedConfigurationPages"].([]any), map[string]any{
						"kind": configurationKind.name, "total": int64(0),
					})
					continue
				}
				configurationProjectRef = projectRef
			}
			response, readErr := server.query.ListManagedConfigurations(ctx, &controlplanev1.ListManagedConfigurationsRequest{
				ProjectRef: configurationProjectRef,
				Kind:       configurationKind.kind,
				Page:       platformPage(),
			})
			if readErr != nil {
				return nil, readErr
			}
			page, projectionErr := httptransport.ManagedConfigurationPageView(response)
			if projectionErr != nil {
				return nil, projectionErr
			}
			encoded, projectionErr := json.Marshal(page)
			if projectionErr != nil {
				return nil, projectionErr
			}
			projection := map[string]any{}
			if projectionErr = json.Unmarshal(encoded, &projection); projectionErr != nil {
				return nil, projectionErr
			}
			items, ok := projection["items"].([]any)
			if !ok {
				return nil, errors.New("managed configuration snapshot items are invalid")
			}
			catalog["managedConfigurations"] = append(catalog["managedConfigurations"].([]any), items...)
			pageProjection := map[string]any{"kind": configurationKind.name, "total": projection["total"]}
			if next, ok := projection["nextPageToken"].(string); ok && next != "" {
				pageProjection["nextPageToken"] = next
			}
			catalog["managedConfigurationPages"] = append(catalog["managedConfigurationPages"].([]any), pageProjection)
		}
		return map[string]any{"catalog": catalog}, nil
	case "RUNTIME_SELECTION":
		response, readErr := server.query.ListRuntimeSelections(ctx, &controlplanev1.ListRuntimeSelectionsRequest{})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	default:
		return nil, errors.New("platform snapshot kind is unsupported")
	}
}

// boundedPlatformSnapshot uses the socket lifecycle context for the complete bounded snapshot path; it does not create a new request lifetime or reuse owner payloads.
// boundedPlatformSnapshot использует жизненный контекст сокета для полного ограниченного снимка, не создавая новый срок запроса и не переиспользуя ответы владельца.
func (multiplexer *sessionMultiplexer) boundedPlatformSnapshot(envelope generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error) {
	return multiplexer.boundedPlatformSnapshotWithin(multiplexer.ctx, envelope)
}

// boundedPlatformSnapshotWithin freshly reads/projects and measures the entire envelope against maximumFrameBytes under the supplied context. Only assistant/provider kinds halve whole owner pages to one; provider overflow may retain the verified financial summary with an unavailable-catalog flag. A successful live assistant read remembers only its scoped page size. Denials/errors are not retried as size failures, content is never truncated, and this method does not enqueue or advance a cursor.
// boundedPlatformSnapshotWithin заново читает, проецирует и измеряет полный конверт относительно maximumFrameBytes в переданном контексте. Только виды помощника и провайдера делят целые owner-страницы до единицы; переполнение провайдера может сохранить проверенную финансовую сводку с признаком недоступного каталога. Успешное активное чтение помощника запоминает только размер страницы своей области. Отказы и ошибки не повторяются как ошибки размера, содержимое не усекается, очередь и курсор этот метод не меняет.
func (multiplexer *sessionMultiplexer) boundedPlatformSnapshotWithin(ctx context.Context, envelope generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error) {
	initialPageSize := int32(platformSnapshotPageSize)
	if envelope.Kind == generated.PlatformResourceKindSystemAssistant {
		initialPageSize = multiplexer.assistantSnapshotInitialPageSize()
	}
	for attempt, pageSize := 1, initialPageSize; ; attempt, pageSize = attempt+1, max(1, pageSize/2) {
		started := time.Now()
		parentBudget := runSnapshotParentBudget(ctx, started)
		rawSnapshot, err := multiplexer.server.projectPlatformSnapshotPage(ctx, string(envelope.Kind), multiplexer.projectRef, multiplexer.localize, pageSize)
		if err != nil {
			if status.Code(err) != codes.PermissionDenied {
				observePlatformSnapshotReadFailure(ctx, started, parentBudget, string(envelope.Kind), err, attempt, pageSize)
			}
			return generated.PlatformSnapshotEnvelope{}, err
		}
		snapshot, err := typedPlatformSnapshot(string(envelope.Kind), rawSnapshot)
		if err != nil {
			if envelope.Kind == generated.PlatformResourceKindRun {
				observePlatformSnapshotReadFailure(ctx, started, parentBudget, string(envelope.Kind), withPlatformSnapshotReadStage(platformSnapshotRunProjectStage, errPlatformSnapshotInvalid), attempt, pageSize)
			} else {
				slog.Error(platformSnapshotValidationFailure, "kind", envelope.Kind, "error_class", "contract")
			}
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
		}
		envelope.Snapshot = snapshot
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
		}
		if len(encoded) <= maximumFrameBytes {
			if envelope.Kind == generated.PlatformResourceKindSystemAssistant && ctx.Err() == nil && multiplexer.ctx.Err() == nil {
				multiplexer.assistantPageHint = assistantSnapshotPageHint{
					organizationRef: multiplexer.organizationRef, projectRef: multiplexer.projectRef, pageSize: pageSize,
				}
			}
			return envelope, nil
		}
		if envelope.Kind == generated.PlatformResourceKindProviderAccount && pageSize == 1 {
			catalog, ok := rawSnapshot["catalog"].(map[string]any)
			if !ok || catalog["financialSummary"] == nil {
				return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
			}
			limited := map[string]any{"catalog": map[string]any{"financialSummary": catalog["financialSummary"], "providerCatalogUnavailable": true}}
			envelope.Snapshot, err = typedPlatformSnapshot("PROVIDER_ACCOUNT", limited)
			if err != nil {
				return generated.PlatformSnapshotEnvelope{}, err
			}
			encoded, err = json.Marshal(envelope)
			if err == nil && len(encoded) <= maximumFrameBytes {
				return envelope, nil
			}
		}
		if (envelope.Kind != generated.PlatformResourceKindSystemAssistant && envelope.Kind != generated.PlatformResourceKindProviderAccount) || pageSize == 1 {
			slog.Error(platformSnapshotSizeFailure, "kind", envelope.Kind, "error_class", "frame_size", "frame_bytes", len(encoded))
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotSize
		}
	}
}

// assistantSnapshotInitialPageSize accepts a known hint only for the current organization/project and active socket context; otherwise it clears the hint and returns twenty-five.
// assistantSnapshotInitialPageSize принимает известную подсказку только для текущих организации, проекта и активного контекста сокета; иначе очищает её и возвращает двадцать пять.
func (multiplexer *sessionMultiplexer) assistantSnapshotInitialPageSize() int32 {
	hint := multiplexer.assistantPageHint
	_, size := platformSnapshotDiagnosticPage("SYSTEM_ASSISTANT", 1, hint.pageSize)
	if size == 0 || hint.organizationRef != multiplexer.organizationRef || hint.projectRef != multiplexer.projectRef || multiplexer.ctx.Err() != nil {
		multiplexer.assistantPageHint = assistantSnapshotPageHint{}
		return assistantSnapshotInitialPageSize
	}
	return size
}

// sendPlatformBootstrap reads and enqueues kinds in fixed order, skipping permission-denied kinds and returning admitted kinds. Other read failure marks the platform unavailable; queue overflow fails immediately. Earlier queued kinds are not rolled back, so this is not an atomic all-kinds bundle.
// sendPlatformBootstrap читает и ставит виды в очередь в фиксированном порядке, пропускает запрещённые виды и возвращает допущенные. Другой отказ чтения отмечает недоступность платформы; переполнение очереди немедленно прерывает работу. Уже поставленные виды не откатываются, это не атомарный набор всех видов.
func (multiplexer *sessionMultiplexer) sendPlatformBootstrap() ([]generated.PlatformResourceKind, error) {
	available := make([]generated.PlatformResourceKind, 0, len(platformBootstrapKinds))
	for _, kind := range platformBootstrapKinds {
		envelope := generated.PlatformSnapshotEnvelope{
			Type: "PLATFORM_SNAPSHOT", RequestRef: multiplexer.platformRequestRef,
			StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor,
			Mode: generated.PlatformSnapshotModeBootstrap,
			Kind: generated.PlatformResourceKind(kind),
		}
		if multiplexer.projectRef != "" {
			envelope.ProjectRef = &multiplexer.projectRef
		}
		envelope, err := multiplexer.boundedPlatformSnapshot(envelope)
		if err != nil {
			if status.Code(err) == codes.PermissionDenied {
				continue
			}
			multiplexer.platformAvailable = false
			return nil, err
		}
		if !multiplexer.send(envelope) {
			return nil, errOutboundOverflow
		}
		available = append(available, generated.PlatformResourceKind(kind))
	}
	return available, nil
}

// providerCatalogOversized recognizes Unavailable only when at least one typed ErrorInfo has exact control-plane domain/reason and empty metadata. It never converts ordinary authorization/network failures into an empty successful catalog.
// providerCatalogOversized распознаёт Unavailable только при наличии типизированного ErrorInfo с точными domain/reason control-plane и пустой metadata. Обычные ошибки авторизации и сети в пустой успешный каталог не превращаются.
func providerCatalogOversized(err error) bool {
	if status.Code(err) != codes.Unavailable {
		return false
	}
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "kodex.control-plane" && info.Reason == "PROVIDER_CATALOG_TOO_LARGE" && len(info.Metadata) == 0 {
			return true
		}
	}
	return false
}
