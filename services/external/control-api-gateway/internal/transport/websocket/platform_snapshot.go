package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const platformSnapshotPageSize = 50

const (
	platformSnapshotReadFailure       = "platform bootstrap snapshot read failed"
	platformSnapshotValidationFailure = "platform bootstrap snapshot validation failed"
	platformSnapshotSizeFailure       = "platform snapshot frame size exceeded"
)

var (
	errPlatformSnapshotInvalid = errors.New("platform snapshot projection is invalid")
	errPlatformSnapshotSize    = errors.New("platform snapshot frame exceeds maximum size")
)

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

func platformPage() *controlplanev1.PageRequest {
	return &controlplanev1.PageRequest{PageSize: platformSnapshotPageSize}
}

func projectSnapshotPart(message proto.Message, localize func(string) string) (map[string]any, error) {
	projection, err := httptransport.ProtoMap(message)
	if err != nil {
		return nil, err
	}
	httptransport.LocalizeSafeErrors(projection, localize)
	return projection, nil
}

func snapshotWithCatalog(message proto.Message, localize func(string) string) (map[string]any, error) {
	catalog, err := projectSnapshotPart(message, localize)
	if err != nil {
		return nil, err
	}
	return map[string]any{"catalog": catalog}, nil
}

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

func scopedProjectContext(ctx context.Context, projectRef string) (context.Context, error) {
	if projectRef == "" {
		return ctx, nil
	}
	return controlplaneclient.WithProjectReference(ctx, projectRef)
}

func (server *Server) projectPlatformSnapshot(ctx context.Context, kind, projectRef string, localize func(string) string) (map[string]any, error) {
	return server.projectPlatformSnapshotPage(ctx, kind, projectRef, localize, platformSnapshotPageSize)
}

func (server *Server) projectPlatformSnapshotPage(ctx context.Context, kind, projectRef string, localize func(string) string, assistantPageSize int32) (map[string]any, error) {
	scoped, err := scopedProjectContext(ctx, projectRef)
	if err != nil {
		return nil, err
	}
	withOverview := func(snapshot map[string]any) (map[string]any, error) {
		overview, readErr := server.query.GetOverview(scoped, &controlplanev1.GetOverviewRequest{ProjectRef: projectRef})
		if readErr != nil {
			return nil, readErr
		}
		projected, projectErr := projectSnapshotPart(overview, localize)
		if projectErr != nil {
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
			return nil, readErr
		}
		catalog, projectErr := projectSnapshotPart(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		gates, readErr := server.query.ListOwnerGates(scoped, &controlplanev1.ListOwnerGatesRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		gateCatalog, projectErr := projectSnapshotPart(gates, localize)
		if projectErr != nil {
			return nil, projectErr
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
			return nil, readErr
		}
		conversations, readErr := server.assistant.ListAssistantConversations(scoped, &controlplanev1.ListAssistantConversationsRequest{ProjectRef: projectRef, Page: &controlplanev1.PageRequest{PageSize: assistantPageSize}})
		if readErr != nil {
			return nil, readErr
		}
		bootstrap, readErr := server.query.GetBootstrapState(ctx, &controlplanev1.GetBootstrapStateRequest{})
		if readErr != nil {
			return nil, readErr
		}
		assistantProjection, projectErr := projectSnapshotPart(assistant, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		conversationProjection, projectErr := projectSnapshotPart(conversations, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		bootstrapProjection, projectErr := projectSnapshotPart(bootstrap, localize)
		if projectErr != nil {
			return nil, projectErr
		}
		state, ok := bootstrapProjection["state"].(map[string]any)
		if !ok {
			return nil, errors.New("bootstrap state projection is invalid")
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
		response, readErr := server.query.ListProviderAccounts(ctx, &controlplanev1.ListProviderAccountsRequest{Page: platformPage()})
		if readErr != nil {
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

// boundedPlatformSnapshot уменьшает только целую авторитетную страницу.
// Содержимое разговоров и cursor не переписываются; размер включает envelope.
func (multiplexer *sessionMultiplexer) boundedPlatformSnapshot(envelope generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error) {
	for pageSize := int32(platformSnapshotPageSize); ; pageSize = max(1, pageSize/2) {
		rawSnapshot, err := multiplexer.server.projectPlatformSnapshotPage(multiplexer.ctx, string(envelope.Kind), multiplexer.projectRef, multiplexer.localize, pageSize)
		if err != nil {
			if status.Code(err) != codes.PermissionDenied {
				slog.Error(platformSnapshotReadFailure, "kind", envelope.Kind, "error_class", "dependency")
			}
			return generated.PlatformSnapshotEnvelope{}, err
		}
		snapshot, err := typedPlatformSnapshot(string(envelope.Kind), rawSnapshot)
		if err != nil {
			slog.Error(platformSnapshotValidationFailure, "kind", envelope.Kind, "error_class", "contract")
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
		}
		envelope.Snapshot = snapshot
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
		}
		if len(encoded) <= maximumFrameBytes {
			return envelope, nil
		}
		if envelope.Kind != generated.PlatformResourceKindSystemAssistant || pageSize == 1 {
			slog.Error(platformSnapshotSizeFailure, "kind", envelope.Kind, "error_class", "frame_size", "frame_bytes", len(encoded))
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotSize
		}
	}
}

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
