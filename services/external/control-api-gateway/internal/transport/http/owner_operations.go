package httptransport

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maximumSelectorItems = 300

func (server *Server) ListScheduleSelectors(writer http.ResponseWriter, request *http.Request) {
	resources, err := server.loadScheduleSelectors(request.Context())
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	selectors := make([]generated.ScheduleSelector, 0, len(resources))
	for _, resource := range resources {
		value, convertErr := ConvertResource(resource)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		selector, selectorErr := scheduleSelector(value)
		if selectorErr != nil {
			server.writeInternal(writer, request.Context(), selectorErr)
			return
		}
		selectors = append(selectors, selector)
	}
	sort.Slice(selectors, func(left, right int) bool {
		if selectors[left].Kind == selectors[right].Kind {
			return selectors[left].DisplayName < selectors[right].DisplayName
		}
		return selectors[left].Kind < selectors[right].Kind
	})
	catalog, err := server.control.GetOwnerConfigurationCatalog(request.Context(), &controlplanev1.GetOwnerConfigurationCatalogRequest{PageSize: maximumPageSize})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	defaults, castErr := castScheduleDefaults(catalog.GetScheduleDefaults())
	if castErr != nil {
		server.writeInternal(writer, request.Context(), castErr)
		return
	}
	result := generated.ScheduleSelectorCatalog{Selectors: selectors, RuntimeSelections: make([]generated.RuntimeSelectionCatalogEntry, 0, len(catalog.GetRuntimeSelections())), Presets: make([]generated.SchedulePreset, 0, len(catalog.GetSchedulePresets())), Defaults: defaults, Complete: true}
	for _, item := range catalog.GetRuntimeSelections() {
		value, itemErr := castRuntimeSelectionCatalogEntry(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.RuntimeSelections = append(result.RuntimeSelections, value)
	}
	for _, item := range catalog.GetSchedulePresets() {
		value, itemErr := castSchedulePreset(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Presets = append(result.Presets, value)
	}
	if catalog.GetNextPageToken() != "" {
		server.writeInternal(writer, request.Context(), errors.New("schedule selector catalog is incomplete"))
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) loadScheduleSelectors(ctx context.Context) ([]*controlplanev1.Resource, error) {
	result := make([]*controlplanev1.Resource, 0)
	for kind := 0; kind < 3; kind++ {
		token := ""
		for {
			var page []*controlplanev1.Resource
			var next string
			var err error
			switch kind {
			case 0:
				response, callErr := server.control.ListAgents(ctx, &controlplanev1.ListAgentsRequest{PageSize: maximumPageSize, PageToken: token})
				err = callErr
				page, next = response.GetAgents(), response.GetNextPageToken()
			case 1:
				response, callErr := server.control.ListInstructionSets(ctx, &controlplanev1.ListInstructionSetsRequest{PageSize: maximumPageSize, PageToken: token})
				err = callErr
				page, next = response.GetInstructionSets(), response.GetNextPageToken()
			default:
				response, callErr := server.control.ListResources(ctx, &controlplanev1.ListResourcesRequest{Kind: controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_POOL, PageSize: maximumPageSize, PageToken: token})
				err = callErr
				page, next = response.GetResources(), response.GetNextPageToken()
			}
			if err != nil {
				return nil, err
			}
			if len(result)+len(page) > maximumSelectorItems || (next != "" && (len(page) == 0 || next == token)) {
				return nil, errors.New("schedule selector catalog is incomplete")
			}
			result = append(result, page...)
			if next == "" {
				break
			}
			token = next
		}
	}
	return result, nil
}

func scheduleSelector(resource generated.Resource) (generated.ScheduleSelector, error) {
	result := generated.ScheduleSelector{Ref: resource.Id.String(), DisplayName: resource.Name, Version: resource.Version, State: resource.State, DigestSha256: resource.ProjectionSha256}
	switch resource.Kind {
	case generated.ResourceKindAGENT:
		if resource.Spec.Agent == nil || resource.Spec.Agent.StableKey == "" {
			return generated.ScheduleSelector{}, errors.New("agent selector is invalid")
		}
		result.Kind, result.StableKey = "AGENT", resource.Spec.Agent.StableKey
	case generated.ResourceKindINSTRUCTIONSET:
		if resource.Spec.InstructionSet == nil || resource.Spec.InstructionSet.StableKey == "" {
			return generated.ScheduleSelector{}, errors.New("instruction set selector is invalid")
		}
		result.Kind, result.StableKey = "INSTRUCTION_SET", resource.Spec.InstructionSet.StableKey
	case generated.ResourceKindPROVIDERPOOL:
		if resource.Spec.ProviderPool == nil || resource.Spec.ProviderPool.StableKey == "" {
			return generated.ScheduleSelector{}, errors.New("provider pool selector is invalid")
		}
		result.Kind, result.StableKey = "PROVIDER_POOL", resource.Spec.ProviderPool.StableKey
	default:
		return generated.ScheduleSelector{}, errors.New("unexpected schedule selector kind")
	}
	return result, nil
}

func (server *Server) ListOwnerSchedules(writer http.ResponseWriter, request *http.Request, params generated.ListOwnerSchedulesParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListOwnerSchedules(request.Context(), &controlplanev1.ListOwnerSchedulesRequest{PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.OwnerSchedulePage{Items: make([]generated.OwnerScheduleView, 0, len(response.GetSchedules())), NextPageToken: optionalString(response.GetNextPageToken())}
	for _, item := range response.GetSchedules() {
		value, castErr := castOwnerScheduleView(item)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Items = append(result.Items, value)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetOwnerSchedule(writer http.ResponseWriter, request *http.Request, scheduleRef string) {
	response, err := server.control.GetOwnerSchedule(request.Context(), &controlplanev1.GetOwnerScheduleRequest{ScheduleId: scheduleRef})
	server.writeOwnerScheduleView(writer, request, http.StatusOK, scheduleRef, response.GetSchedule(), err)
}

func (server *Server) CreateScheduleFromSelections(writer http.ResponseWriter, request *http.Request, params generated.CreateScheduleFromSelectionsParams) {
	var body generated.CreateScheduleFromSelectionsJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	intent, ok := ownerScheduleIntent(body.Intent)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageOwnerSchedule(request.Context(), &controlplanev1.ManageOwnerScheduleRequest{IdempotencyKey: params.IdempotencyKey.String(), Action: controlplanev1.OwnerScheduleCommandAction_OWNER_SCHEDULE_COMMAND_ACTION_CREATE, Name: body.Name,
		AgentStableKey: body.AgentStableKey, InstructionSetStableKey: body.InstructionSetStableKey, ProviderPoolStableKey: body.ProviderPoolStableKey, Intent: intent})
	server.writeOwnerScheduleView(writer, request, http.StatusCreated, "", response.GetSchedule(), err)
}

func (server *Server) BindScheduleConfiguration(writer http.ResponseWriter, request *http.Request, scheduleRef string, params generated.BindScheduleConfigurationParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.BindScheduleConfigurationJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	intent, valid := ownerScheduleIntent(body.Intent)
	if !valid {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageOwnerSchedule(request.Context(), &controlplanev1.ManageOwnerScheduleRequest{IdempotencyKey: params.IdempotencyKey.String(), Action: controlplanev1.OwnerScheduleCommandAction_OWNER_SCHEDULE_COMMAND_ACTION_UPDATE, ScheduleId: scheduleRef, ExpectedVersion: version, Name: body.Name,
		AgentStableKey: body.AgentStableKey, InstructionSetStableKey: body.InstructionSetStableKey, ProviderPoolStableKey: body.ProviderPoolStableKey, Intent: intent})
	server.writeOwnerScheduleView(writer, request, http.StatusOK, scheduleRef, response.GetSchedule(), err)
}

func ownerScheduleIntent(input generated.OwnerScheduleIntent) (*controlplanev1.OwnerScheduleIntent, bool) {
	if strings.TrimSpace(input.Timezone) == "" || strings.TrimSpace(input.PresetKey) == "" || (input.Prompt.InlineMarkdown == nil) == (input.Prompt.ArtifactSelector == nil) {
		return nil, false
	}
	result := &controlplanev1.OwnerScheduleIntent{Timezone: input.Timezone, PresetKey: input.PresetKey, RoomStableKey: stringValue(input.RoomStableKey), Prompt: &controlplanev1.OwnerSchedulePromptIntent{}}
	if input.Prompt.InlineMarkdown != nil {
		if strings.TrimSpace(*input.Prompt.InlineMarkdown) == "" {
			return nil, false
		}
		result.Prompt.Source = &controlplanev1.OwnerSchedulePromptIntent_InlineMarkdown{InlineMarkdown: *input.Prompt.InlineMarkdown}
	} else {
		if strings.TrimSpace(*input.Prompt.ArtifactSelector) == "" {
			return nil, false
		}
		result.Prompt.Source = &controlplanev1.OwnerSchedulePromptIntent_ArtifactName{ArtifactName: *input.Prompt.ArtifactSelector}
	}
	if input.AdvancedOverrides != nil {
		advanced, ok := ownerScheduleAdvancedOverrides(*input.AdvancedOverrides)
		if !ok {
			return nil, false
		}
		result.AdvancedOverrides = advanced
	}
	return result, true
}

func ownerScheduleAdvancedOverrides(input generated.OwnerScheduleAdvancedOverrides) (*controlplanev1.OwnerScheduleAdvancedOverrides, bool) {
	if input.Cron != nil && input.IntervalSeconds != nil {
		return nil, false
	}
	result := &controlplanev1.OwnerScheduleAdvancedOverrides{Cron: input.Cron, Calendar: stringPointerValue(input.Calendar), DeliveryPolicy: stringPointerValue(input.DeliveryPolicy), MaximumAttempts: uint32Pointer(input.MaximumAttempts), Coalesce: input.Coalesce}
	if input.IntervalSeconds != nil {
		result.Interval = seconds(*input.IntervalSeconds)
	}
	if input.MisfireGraceSeconds != nil {
		result.MisfireGrace = seconds(*input.MisfireGraceSeconds)
	}
	if input.InitialBackoffSeconds != nil {
		result.InitialBackoff = seconds(*input.InitialBackoffSeconds)
	}
	if input.MaximumBackoffSeconds != nil {
		result.MaximumBackoff = seconds(*input.MaximumBackoffSeconds)
	}
	if input.DeadLetterAfterSeconds != nil {
		result.DeadLetterAfter = seconds(*input.DeadLetterAfterSeconds)
	}
	if input.MaximumExecutionSeconds != nil {
		result.MaximumExecutionDuration = seconds(*input.MaximumExecutionSeconds)
	}
	if input.OverlapPolicy != nil {
		value, ok := controlplanev1.ScheduleOverlapPolicy_value["SCHEDULE_OVERLAP_POLICY_"+string(*input.OverlapPolicy)]
		if !ok {
			return nil, false
		}
		converted := controlplanev1.ScheduleOverlapPolicy(value)
		result.OverlapPolicy = &converted
	}
	if input.MisfirePolicy != nil {
		value, ok := controlplanev1.ScheduleMisfirePolicy_value["SCHEDULE_MISFIRE_POLICY_"+string(*input.MisfirePolicy)]
		if !ok {
			return nil, false
		}
		converted := controlplanev1.ScheduleMisfirePolicy(value)
		result.MisfirePolicy = &converted
	}
	if input.SessionPolicy != nil {
		value, ok := controlplanev1.ScheduleSessionPolicy_value["SCHEDULE_SESSION_POLICY_"+string(*input.SessionPolicy)]
		if !ok {
			return nil, false
		}
		converted := controlplanev1.ScheduleSessionPolicy(value)
		result.SessionPolicy = &converted
	}
	if input.NotificationPolicy != nil {
		value, ok := controlplanev1.ScheduleNotificationPolicy_value["SCHEDULE_NOTIFICATION_POLICY_"+string(*input.NotificationPolicy)]
		if !ok {
			return nil, false
		}
		converted := controlplanev1.ScheduleNotificationPolicy(value)
		result.NotificationPolicy = &converted
	}
	return result, true
}

func stringPointerValue[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func uint32Pointer(value *int) *uint32 {
	if value == nil || *value < 0 {
		return nil
	}
	converted := uint32(*value)
	return &converted
}

func (server *Server) writeOwnerScheduleView(writer http.ResponseWriter, request *http.Request, status int, expected string, input *controlplanev1.OwnerScheduleProjection, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	value, castErr := castOwnerScheduleView(input)
	if castErr != nil || (expected != "" && value.ScheduleRef != expected) {
		server.writeInternal(writer, request.Context(), errors.New("owner schedule readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, status, value)
}

func seconds(value int64) *durationpb.Duration {
	return durationpb.New(time.Duration(value) * time.Second)
}

func (server *Server) GetRunDetail(writer http.ResponseWriter, request *http.Request, runRef generated.RunRef) {
	response, err := server.control.GetRunDetail(request.Context(), &controlplanev1.GetRunDetailRequest{ProcessRunId: string(runRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	run, convertErr := castRunView(response.GetProjection())
	if convertErr != nil || run.RunRef != string(runRef) || len(response.GetIncidents()) != len(response.GetIncidentProjections()) {
		server.writeInternal(writer, request.Context(), errors.New("run detail readback is invalid"))
		return
	}
	result := generated.RunDetail{Run: run, Incidents: make([]generated.IncidentView, 0, len(response.GetIncidentProjections())), IncidentsNextPageToken: optionalString(response.GetIncidentsNextPageToken())}
	if response.GetSession() != nil {
		value, castErr := castRunSession(response.GetSession())
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Session = &value
	}
	if response.GetTurn() != nil {
		value, castErr := castRunTurn(response.GetTurn())
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Turn = &value
	}
	for _, input := range response.GetIncidentProjections() {
		value, castErr := castIncidentView(input)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Incidents = append(result.Incidents, value)
	}
	writer.Header().Set("ETag", etag(uint64(run.Version)))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ListRunTimeline(writer http.ResponseWriter, request *http.Request, runRef generated.RunRef, params generated.ListRunTimelineParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListRunTimeline(request.Context(), &controlplanev1.ListRunTimelineRequest{ProcessRunId: string(runRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	run, castErr := castRunView(response.GetRun())
	next, nextErr := castRunNextActions(response.GetNextActions())
	if castErr != nil || nextErr != nil || run.RunRef != string(runRef) || len(response.GetEvents()) != len(response.GetProjections()) {
		server.writeInternal(writer, request.Context(), errors.New("run timeline readback is invalid"))
		return
	}
	result := generated.RunTimelinePage{Run: run, Entries: make([]generated.RunTimelineEntry, 0, len(response.GetProjections())), NextActions: next}
	for _, item := range response.GetProjections() {
		value, itemErr := castRunTimelineEntry(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Entries = append(result.Entries, value)
	}
	if nextToken := response.GetNextPageToken(); nextToken != "" {
		result.NextPageToken = &nextToken
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetRunLineage(writer http.ResponseWriter, request *http.Request, runRef generated.RunRef, params generated.GetRunLineageParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.GetRunLineage(request.Context(), &controlplanev1.GetRunLineageRequest{ProcessRunId: string(runRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	run, runErr := castRunView(response.GetRun())
	next, nextErr := castRunNextActions(response.GetNextActions())
	if runErr != nil || nextErr != nil || run.RunRef != string(runRef) || len(response.GetProjections()) > 500 {
		server.writeInternal(writer, request.Context(), errors.New("run lineage readback is invalid"))
		return
	}
	result := generated.RunLineage{Run: run, Nodes: make([]generated.RunLineageNode, 0, len(response.GetProjections())), NextActions: next, Truncated: response.GetTruncated(), NextPageToken: optionalString(response.GetNextPageToken())}
	for _, item := range response.GetProjections() {
		node, itemErr := castRunLineageNode(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Nodes = append(result.Nodes, node)
	}
	if result.Truncated != (result.NextPageToken != nil) {
		server.writeInternal(writer, request.Context(), errors.New("run lineage continuation is inconsistent"))
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ListRunArtifacts(writer http.ResponseWriter, request *http.Request, runRef generated.RunRef, params generated.ListRunArtifactsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListRunArtifacts(request.Context(), &controlplanev1.ListRunArtifactsRequest{ProcessRunId: string(runRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	if len(response.GetArtifacts()) != len(response.GetProjections()) {
		server.writeInternal(writer, request.Context(), errors.New("run artifact projection page is incomplete"))
		return
	}
	next, nextErr := castRunNextActions(response.GetNextActions())
	if nextErr != nil {
		server.writeInternal(writer, request.Context(), nextErr)
		return
	}
	result := generated.RunArtifactPage{Artifacts: make([]generated.RunArtifactView, 0, len(response.GetProjections())), NextActions: next, NextPageToken: optionalString(response.GetNextPageToken())}
	for _, item := range response.GetProjections() {
		value, itemErr := castRunArtifact(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Artifacts = append(result.Artifacts, value)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ManageRun(writer http.ResponseWriter, request *http.Request, runRef generated.RunRef, params generated.ManageRunParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ManageRunJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]controlplanev1.RunAction{"CANCEL": controlplanev1.RunAction_RUN_ACTION_CANCEL, "RETRY": controlplanev1.RunAction_RUN_ACTION_RETRY}[string(body.Action)]
	if action == controlplanev1.RunAction_RUN_ACTION_UNSPECIFIED {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageRun(request.Context(), &controlplanev1.ManageRunRequest{IdempotencyKey: params.IdempotencyKey.String(), ProcessRunId: string(runRef), ExpectedVersion: version, Action: action, ReasonCode: body.ReasonCode})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	run, convertErr := castRunView(response.GetProjection())
	if convertErr != nil || run.RunRef != string(runRef) || len(response.GetIncidentProjections()) > 100 {
		server.writeInternal(writer, request.Context(), errors.New("run command readback is invalid"))
		return
	}
	result := generated.RunCommandResult{Run: run, Incidents: make([]generated.IncidentView, 0, len(response.GetIncidentProjections())), IncidentsNextPageToken: optionalString(response.GetIncidentsNextPageToken())}
	for _, item := range response.GetIncidentProjections() {
		value, itemErr := castIncidentView(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Incidents = append(result.Incidents, value)
	}
	writer.Header().Set("ETag", etag(uint64(run.Version)))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetIncident(writer http.ResponseWriter, request *http.Request, incidentRef generated.IncidentRef) {
	response, err := server.control.GetRuntimeIncident(request.Context(), &controlplanev1.GetRuntimeIncidentRequest{IncidentId: string(incidentRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := castIncidentView(response.GetProjection())
	if convertErr != nil || value.IncidentRef != string(incidentRef) {
		server.writeInternal(writer, request.Context(), errors.New("runtime incident readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) ListIncidentHistory(writer http.ResponseWriter, request *http.Request, incidentRef generated.IncidentRef, params generated.ListIncidentHistoryParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListRuntimeIncidentHistory(request.Context(), &controlplanev1.ListRuntimeIncidentHistoryRequest{IncidentId: string(incidentRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	next, nextErr := castIncidentNextActions(response.GetNextActions())
	if nextErr != nil {
		server.writeInternal(writer, request.Context(), nextErr)
		return
	}
	result := generated.IncidentHistoryPage{Entries: make([]generated.IncidentHistoryEntry, 0, len(response.GetEntries())), NextActions: next}
	for _, item := range response.GetEntries() {
		value, itemErr := incidentHistoryEntry(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.Entries = append(result.Entries, value)
	}
	result.NextPageToken = optionalString(response.GetNextPageToken())
	writeJSON(writer, http.StatusOK, result)
}

func incidentHistoryEntry(input *controlplanev1.RuntimeIncidentHistoryEntry) (generated.IncidentHistoryEntry, error) {
	if input == nil || input.GetVersion() == 0 || input.GetExecutionFence() == 0 || len(input.GetReasonCode()) > 96 {
		return generated.IncidentHistoryEntry{}, errors.New("incident history entry is incomplete")
	}
	state, stateOK := runtimeIncidentState(input.GetState())
	action, actionOK := runtimeIncidentAction(input.GetAction())
	occurred, err := requiredTimestamp(input.GetOccurredAt())
	next, nextErr := castIncidentNextActions(input.GetNextActions())
	if !stateOK || !actionOK || err != nil || nextErr != nil {
		return generated.IncidentHistoryEntry{}, errors.New("incident history entry values are invalid")
	}
	return generated.IncidentHistoryEntry{Version: int64(input.GetVersion()), State: state, Action: action, ReasonCode: input.GetReasonCode(), OccurredAt: occurred, ExecutionFence: int64(input.GetExecutionFence()), NextActions: next}, nil
}

func (server *Server) ManageIncident(writer http.ResponseWriter, request *http.Request, incidentRef generated.IncidentRef, params generated.ManageIncidentParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ManageIncidentJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]controlplanev1.RuntimeIncidentAction{"ACKNOWLEDGE": controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_ACKNOWLEDGE, "RETRY": controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RETRY,
		"RELEASE": controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RELEASE, "CLOSE": controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_CLOSE}[string(body.Action)]
	if action == controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_UNSPECIFIED {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.control.ManageRuntimeIncident(request.Context(), &controlplanev1.ManageRuntimeIncidentRequest{IdempotencyKey: params.IdempotencyKey.String(), IncidentId: string(incidentRef), ExpectedVersion: version, Action: action, ReasonCode: body.ReasonCode})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	incident, convertErr := castIncidentView(response.GetProjection())
	if convertErr != nil || incident.IncidentRef != string(incidentRef) {
		server.writeInternal(writer, request.Context(), errors.New("incident command readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(incident.Version)))
	writeJSON(writer, http.StatusOK, generated.IncidentCommandResult{Incident: incident})
}

func runtimeIncidentState(value controlplanev1.RuntimeIncidentState) (generated.IncidentState, bool) {
	states := map[controlplanev1.RuntimeIncidentState]generated.IncidentState{controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_OPEN: "OPEN", controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_ACKNOWLEDGED: "ACKNOWLEDGED", controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_RETRYING: "RETRYING", controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_RELEASED: "RELEASED", controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_CLOSED: "CLOSED"}
	result := states[value]
	return result, result != ""
}

func runtimeIncidentAction(value controlplanev1.RuntimeIncidentAction) (generated.IncidentHistoryEntryAction, bool) {
	actions := map[controlplanev1.RuntimeIncidentAction]generated.IncidentHistoryEntryAction{controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_ACKNOWLEDGE: "ACKNOWLEDGE", controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RETRY: "RETRY", controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RELEASE: "RELEASE", controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_CLOSE: "CLOSE"}
	result := actions[value]
	return result, result != ""
}

func (server *Server) ListWorkspaceBackups(writer http.ResponseWriter, request *http.Request, params generated.ListWorkspaceBackupsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListWorkspaceBackups(request.Context(), &controlplanev1.ListWorkspaceBackupsRequest{PageSize: size, PageToken: token})
	server.writeResourcePage(writer, request, response.GetBackups(), response.GetNextPageToken(), err)
}

func (server *Server) GetWorkspaceBackup(writer http.ResponseWriter, request *http.Request, backupRef generated.BackupRef) {
	response, err := server.control.GetWorkspaceBackup(request.Context(), &controlplanev1.GetWorkspaceBackupRequest{BackupId: string(backupRef)})
	server.writeExactResource(writer, request, response.GetBackup(), string(backupRef), err)
}

func (server *Server) ManageWorkspaceBackup(writer http.ResponseWriter, request *http.Request, params generated.ManageWorkspaceBackupParams) {
	var body generated.ManageWorkspaceBackupJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]controlplanev1.WorkspaceBackupAction{"CREATE": controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CREATE, "CANCEL": controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CANCEL, "RETRY": controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_RETRY}[string(body.Action)]
	create := action == controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CREATE
	version, ok := commandVersion(writer, create, params.IfMatch)
	if !ok || action == controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_UNSPECIFIED || !validWorkspaceBackupCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	scope := map[string]controlplanev1.WorkspaceBackupScope{"WORKSPACE": controlplanev1.WorkspaceBackupScope_WORKSPACE_BACKUP_SCOPE_WORKSPACE, "ALL_WORKSPACES": controlplanev1.WorkspaceBackupScope_WORKSPACE_BACKUP_SCOPE_ALL_WORKSPACES}[stringValue(body.Scope)]
	requestProto := &controlplanev1.ManageWorkspaceBackupRequest{IdempotencyKey: params.IdempotencyKey.String(), Action: action, BackupId: stringValue(body.BackupRef), ExpectedVersion: version, Scope: scope, Name: stringValue(body.Name), TerminalReasonCode: stringValue(body.TerminalReasonCode)}
	if body.RetainUntil != nil {
		requestProto.RetainUntil = timestamppb.New(body.RetainUntil.UTC())
	}
	response, err := server.control.ManageWorkspaceBackup(request.Context(), requestProto)
	statusCode := http.StatusOK
	if create || action == controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_RETRY {
		statusCode = http.StatusAccepted
	}
	server.writeResourceResponse(writer, request.Context(), statusCode, response.GetBackup(), err, true)
}

func validWorkspaceBackupCommand(body generated.WorkspaceBackupCommand, action controlplanev1.WorkspaceBackupAction) bool {
	switch action {
	case controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CREATE:
		return body.BackupRef == nil && body.Scope != nil && body.Scope.Valid() && validBoundedText(body.Name, 160) && body.RetainUntil != nil && body.TerminalReasonCode == nil
	case controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_CANCEL:
		return validBoundedText(body.BackupRef, 160) && body.Scope == nil && body.Name == nil && body.RetainUntil == nil && validBoundedText(body.TerminalReasonCode, 96)
	case controlplanev1.WorkspaceBackupAction_WORKSPACE_BACKUP_ACTION_RETRY:
		return validBoundedText(body.BackupRef, 160) && body.Scope == nil && body.Name == nil && body.RetainUntil == nil && body.TerminalReasonCode == nil
	default:
		return false
	}
}

func (server *Server) ListWorkspaceRestores(writer http.ResponseWriter, request *http.Request, params generated.ListWorkspaceRestoresParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListWorkspaceRestores(request.Context(), &controlplanev1.ListWorkspaceRestoresRequest{BackupId: stringValue(params.BackupRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	if len(response.GetRestores()) != len(response.GetProjections()) {
		server.writeInternal(writer, request.Context(), errors.New("workspace restore projection page is incomplete"))
		return
	}
	result := generated.WorkspaceRestorePage{Restores: make([]generated.WorkspaceRestoreView, 0, len(response.GetProjections())), NextPageToken: optionalString(response.GetNextPageToken())}
	for _, item := range response.GetProjections() {
		value, castErr := castWorkspaceRestoreView(item)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Restores = append(result.Restores, value)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetWorkspaceRestore(writer http.ResponseWriter, request *http.Request, restoreRef generated.RestoreRef) {
	response, err := server.control.GetWorkspaceRestore(request.Context(), &controlplanev1.GetWorkspaceRestoreRequest{RestoreId: string(restoreRef)})
	server.writeWorkspaceRestoreView(writer, request, http.StatusOK, string(restoreRef), response.GetProjection(), err, false)
}

func (server *Server) ManageWorkspaceRestore(writer http.ResponseWriter, request *http.Request, params generated.ManageWorkspaceRestoreParams) {
	var body generated.ManageWorkspaceRestoreJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]controlplanev1.WorkspaceRestoreAction{"CREATE": controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CREATE, "CANCEL": controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CANCEL, "RETRY": controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_RETRY}[string(body.Action)]
	create := action == controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CREATE
	version, ok := commandVersion(writer, create, params.IfMatch)
	if !ok || action == controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_UNSPECIFIED || !validWorkspaceRestoreCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	response, err := server.control.ManageWorkspaceRestore(request.Context(), &controlplanev1.ManageWorkspaceRestoreRequest{IdempotencyKey: params.IdempotencyKey.String(), Action: action, RestoreId: stringValue(body.RestoreRef), ExpectedVersion: version,
		BackupId: stringValue(body.BackupRef), ExpectedBackupVersion: uint64Value(body.BackupVersion), MembershipSha256: shaValue(body.MembershipSha256), Name: stringValue(body.Name), TerminalReasonCode: stringValue(body.TerminalReasonCode)})
	statusCode := http.StatusOK
	if create || action == controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_RETRY {
		statusCode = http.StatusAccepted
	}
	server.writeWorkspaceRestoreView(writer, request, statusCode, stringValue(body.RestoreRef), response.GetProjection(), err, true)
}

func validWorkspaceRestoreCommand(body generated.WorkspaceRestoreCommand, action controlplanev1.WorkspaceRestoreAction) bool {
	switch action {
	case controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CREATE:
		return body.RestoreRef == nil && validBoundedText(body.BackupRef, 160) && uint64Value(body.BackupVersion) > 0 && validSHA256(shaValue(body.MembershipSha256)) && validBoundedText(body.Name, 160) && body.TerminalReasonCode == nil
	case controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_CANCEL:
		return validBoundedText(body.RestoreRef, 160) && body.BackupRef == nil && body.BackupVersion == nil && body.MembershipSha256 == nil && body.Name == nil && validBoundedText(body.TerminalReasonCode, 96)
	case controlplanev1.WorkspaceRestoreAction_WORKSPACE_RESTORE_ACTION_RETRY:
		return validBoundedText(body.RestoreRef, 160) && body.BackupRef == nil && body.BackupVersion == nil && body.MembershipSha256 == nil && body.Name == nil && body.TerminalReasonCode == nil
	default:
		return false
	}
}

func validBoundedText(value *string, maximum int) bool {
	return value != nil && strings.TrimSpace(*value) != "" && len(*value) <= maximum
}

func (server *Server) writeWorkspaceRestoreView(writer http.ResponseWriter, request *http.Request, status int, expected string, input *controlplanev1.WorkspaceRestoreOwnerProjection, err error, mutation bool) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, mutation)
		return
	}
	value, castErr := castWorkspaceRestoreView(input)
	if castErr != nil || (expected != "" && value.RestoreRef != expected) {
		server.writeInternal(writer, request.Context(), errors.New("workspace restore readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, status, value)
}

func (server *Server) writeExactResource(writer http.ResponseWriter, request *http.Request, input *controlplanev1.Resource, expected string, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertResource(input)
	if convertErr != nil || value.Id.String() != expected {
		server.writeInternal(writer, request.Context(), errors.New("exact resource readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) GetHealthSeries(writer http.ResponseWriter, request *http.Request) {
	observedAt := time.Now().UTC()
	control, controlErr := server.control.GetDiagnostics(request.Context(), &controlplanev1.GetDiagnosticsRequest{})
	interaction, interactionErr := server.interaction.CheckReadiness(request.Context(), &interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest{})
	integration, integrationErr := server.integration.GetManagementDiagnostics(request.Context(), &integrationgatewayv1.GetManagementDiagnosticsRequest{})
	if controlErr != nil || interactionErr != nil || integrationErr != nil || control == nil || interaction == nil || integration == nil {
		failure := controlErr
		if failure == nil {
			failure = interactionErr
		}
		if failure == nil {
			failure = integrationErr
		}
		if failure == nil {
			failure = errors.New("owner health readback is unavailable")
		}
		server.writeRPCError(writer, request.Context(), failure, false)
		return
	}
	if control.GetSchemaVersion() == 0 || interaction.GetSchemaVersion() == 0 {
		server.writeInternal(writer, request.Context(), errors.New("owner health readback is incomplete"))
		return
	}
	interactionStatus := generated.HealthObservationStatusDEGRADED
	interactionValue := int64(0)
	if interaction.GetReady() {
		interactionStatus = generated.HealthObservationStatusOK
		interactionValue = 1
	}
	integrationStatus, integrationStatusOK := healthObservationStatus(integration.GetStatus())
	if !integrationStatusOK {
		server.writeInternal(writer, request.Context(), errors.New("integration health status is invalid"))
		return
	}
	observations := []generated.HealthObservation{
		{Source: "CONTROL_PLANE", Component: "schema", Status: "OK", Value: int64(control.GetSchemaVersion()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "pending_outbox", Status: "UNKNOWN", Value: int64(control.GetPendingOutboxEvents()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "terminal_outbox", Status: "UNKNOWN", Value: int64(control.GetTerminalOutboxEvents()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "active_turn_leases", Status: "UNKNOWN", Value: int64(control.GetActiveTurnLeases()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "queued_schedule_occurrences", Status: "UNKNOWN", Value: int64(control.GetQueuedScheduleOccurrences()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "INTERACTION_GATEWAY", Component: "mattermost_team_working_path", Status: interactionStatus, Value: interactionValue, Version: int64(interaction.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "INTEGRATION_GATEWAY", Component: "overall", Status: integrationStatus, Value: healthStatusValue(integrationStatus), Version: 0, ObservedAt: observedAt},
	}
	for _, item := range integration.GetDependencies() {
		checked, err := requiredTimestamp(item.GetCheckedAt())
		status, statusOK := healthObservationStatus(item.GetStatus())
		if err != nil || item.GetDependency() == "" || !statusOK || item.GetVersion() == 0 {
			server.writeInternal(writer, request.Context(), errors.New("integration health observation is invalid"))
			return
		}
		value := int64(0)
		if status == generated.HealthObservationStatusOK {
			value = 1
		}
		observation := generated.HealthObservation{Source: "INTEGRATION_GATEWAY", Component: item.GetDependency(), Status: status, Value: value, Version: int64(item.GetVersion()), ObservedAt: checked}
		if item.GetDigestSha256() != "" {
			if !validSHA256(item.GetDigestSha256()) {
				server.writeInternal(writer, request.Context(), errors.New("integration health digest is invalid"))
				return
			}
			digest := generated.Sha256(strings.ToLower(item.GetDigestSha256()))
			observation.DigestSha256 = &digest
		}
		observations = append(observations, observation)
	}
	writeJSON(writer, http.StatusOK, generated.HealthSeries{Observations: observations, Complete: true})
}

func healthStatusValue(status generated.HealthObservationStatus) int64 {
	if status == generated.HealthObservationStatusOK {
		return 1
	}
	return 0
}

func healthObservationStatus(value string) (generated.HealthObservationStatus, bool) {
	mapping := map[string]generated.HealthObservationStatus{"READY": "OK", "DEGRADED": "DEGRADED", "UNAVAILABLE": "UNAVAILABLE", "UNKNOWN": "UNKNOWN"}
	result, ok := mapping[value]
	return result, ok
}

func (server *Server) ExportAudit(writer http.ResponseWriter, request *http.Request, params generated.ExportAuditParams) {
	kind := controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED
	if params.ResourceKind != nil {
		value, ok := controlplanev1.ResourceKind_value["RESOURCE_KIND_"+string(*params.ResourceKind)]
		if !ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return
		}
		kind = controlplanev1.ResourceKind(value)
	}
	events, continuation, err := server.scanAudit(request.Context(), &controlplanev1.ListAuditEventsRequest{ResourceKind: kind, ResourceId: stringValue(params.ResourceRef), Action: stringValue(params.Action), PageSize: maximumAuditScan}, nil)
	if err != nil {
		server.writeAuditScanError(writer, request.Context(), err)
		return
	}
	if continuation != "" {
		writeProblem(writer, localProblem(http.StatusServiceUnavailable, "EXPORT_TRUNCATED", true))
		return
	}
	converted := make([]generated.AuditEvent, 0, len(events))
	for _, item := range events {
		value, castErr := ConvertAuditEvent(item)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		converted = append(converted, value)
	}
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.Header().Set("Content-Disposition", "attachment; filename=owner-audit.csv")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	csvWriter := csv.NewWriter(writer)
	_ = csvWriter.Write([]string{"id", "action", "resource_kind", "resource_id", "resource_version", "outcome", "actor_id", "correlation_id", "policy_revision", "occurred_at"})
	for _, value := range converted {
		_ = csvWriter.Write([]string{value.Id.String(), value.Action, string(value.ResourceKind), value.ResourceId.String(), strconv.FormatInt(value.ResourceVersion, 10), value.Outcome, value.ActorId.String(), value.CorrelationId.String(), strconv.FormatInt(value.PolicyRevision, 10), value.OccurredAt.Format(time.RFC3339Nano)})
	}
	csvWriter.Flush()
}

func (server *Server) GetConfigurationSourceDetail(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.GetConfigurationSourceDetailParams) {
	var resource *controlplanev1.Resource
	var err error
	switch string(params.Kind) {
	case "ROLE_DEFINITION":
		response, callErr := server.control.GetRoleDefinition(request.Context(), &controlplanev1.GetRoleDefinitionRequest{RoleDefinitionId: string(resourceRef)})
		resource, err = response.GetRoleDefinition(), callErr
	case "AGENT":
		response, callErr := server.control.GetAgent(request.Context(), &controlplanev1.GetAgentRequest{AgentId: string(resourceRef)})
		resource, err = response.GetAgent(), callErr
	case "INSTRUCTION_SET":
		response, callErr := server.control.GetInstructionSet(request.Context(), &controlplanev1.GetInstructionSetRequest{InstructionSetId: string(resourceRef)})
		resource, err = response.GetInstructionSet(), callErr
	case "PROVIDER_POOL":
		response, callErr := server.control.GetResource(request.Context(), &controlplanev1.GetResourceRequest{ResourceId: string(resourceRef), ExpectedKind: controlplanev1.ResourceKind_RESOURCE_KIND_PROVIDER_POOL})
		resource, err = response.GetResource(), callErr
	default:
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, convertErr := ConvertResource(resource)
	if convertErr != nil || value.Id.String() != string(resourceRef) {
		server.writeInternal(writer, request.Context(), errors.New("configuration source readback is invalid"))
		return
	}
	ownership, ownershipErr := configurationOwnership(value)
	if ownershipErr != nil {
		server.writeInternal(writer, request.Context(), ownershipErr)
		return
	}
	writeJSON(writer, http.StatusOK, generated.ConfigurationSourceDetail{ResourceRef: string(resourceRef), DisplayName: value.Name, Version: value.Version, ManagedBy: ownership.ManagedBy, Source: ownership.Source, SourceRevision: ownership.Revision, UpdatedAt: value.UpdatedAt})
}

func configurationOwnership(resource generated.Resource) (generated.ConfigurationOwnershipProjection, error) {
	switch resource.Kind {
	case generated.ResourceKindROLEDEFINITION:
		if resource.Spec.RoleDefinition != nil {
			return resource.Spec.RoleDefinition.Ownership, nil
		}
	case generated.ResourceKindAGENT:
		if resource.Spec.Agent != nil {
			return resource.Spec.Agent.Ownership, nil
		}
	case generated.ResourceKindINSTRUCTIONSET:
		if resource.Spec.InstructionSet != nil {
			return resource.Spec.InstructionSet.Ownership, nil
		}
	case generated.ResourceKindPROVIDERPOOL:
		if resource.Spec.ProviderPool != nil {
			return resource.Spec.ProviderPool.Ownership, nil
		}
	}
	return generated.ConfigurationOwnershipProjection{}, errors.New("configuration ownership is missing")
}
