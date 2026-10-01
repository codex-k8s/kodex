package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/protobuf/proto"
)

const platformSnapshotPageSize = 50

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
	case "WORKFLOW", "SCHEDULE", "MEMBERSHIP", "PLATFORM_MEMBERSHIP", "ROLE_IMAGE_RECIPE", "RUNTIME_ENVIRONMENT", "PROVIDER_ACCOUNT":
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
	scoped, err := scopedProjectContext(ctx, projectRef)
	if err != nil {
		return nil, err
	}
	requireProject := func() error {
		if projectRef == "" {
			return errors.New("platform snapshot project scope is required")
		}
		return nil
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
		if projectRef != "" {
			selected, selectedErr := server.query.GetProject(scoped, &controlplanev1.GetProjectRequest{ProjectRef: projectRef})
			if selectedErr != nil {
				return nil, selectedErr
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
		snapshot, projectErr := snapshotWithCatalog(response, localize)
		if projectErr != nil {
			return nil, projectErr
		}
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
		response, readErr := server.query.ListProjectMemberships(scoped, &controlplanev1.ListProjectMembershipsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
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
		conversations, readErr := server.assistant.ListAssistantConversations(scoped, &controlplanev1.ListAssistantConversationsRequest{ProjectRef: projectRef, Page: platformPage()})
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
		if err := requireProject(); err != nil {
			return nil, err
		}
		response, readErr := server.roleImages.ListRoleImageRecipes(scoped, &controlplanev1.ListRoleImageRecipesRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	case "RUNTIME_ENVIRONMENT":
		response, readErr := server.query.ListRuntimeEnvironmentSets(scoped, &controlplanev1.ListRuntimeEnvironmentSetsRequest{ProjectRef: projectRef, Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	case "PROVIDER_ACCOUNT":
		response, readErr := server.query.ListProviderAccounts(ctx, &controlplanev1.ListProviderAccountsRequest{Page: platformPage()})
		if readErr != nil {
			return nil, readErr
		}
		return snapshotWithCatalog(response, localize)
	default:
		return nil, errors.New("platform snapshot kind is unsupported")
	}
}

func (multiplexer *sessionMultiplexer) sendPlatformBootstrap() bool {
	for _, kind := range platformBootstrapKinds {
		if multiplexer.projectRef == "" {
			switch kind {
			case "ROLE_IMAGE_RECIPE":
				continue
			}
		}
		rawSnapshot, err := multiplexer.server.projectPlatformSnapshot(multiplexer.ctx, kind, multiplexer.projectRef, multiplexer.localize)
		if err != nil {
			multiplexer.platformAvailable = false
			return multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, "PLATFORM_UNAVAILABLE")
		}
		snapshot, err := typedPlatformSnapshot(kind, rawSnapshot)
		if err != nil {
			multiplexer.platformAvailable = false
			return multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, "INTERNAL")
		}
		envelope := generated.PlatformSnapshotEnvelope{
			Type: "PLATFORM_SNAPSHOT", RequestRef: multiplexer.platformRequestRef,
			StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor,
			Mode: generated.PlatformSnapshotModeBootstrap,
			Kind: generated.PlatformResourceKind(kind), Snapshot: snapshot,
		}
		if multiplexer.projectRef != "" {
			envelope.ProjectRef = &multiplexer.projectRef
		}
		if !multiplexer.send(envelope) {
			return false
		}
	}
	return true
}
