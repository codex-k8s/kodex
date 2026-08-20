package websockettransport

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/projection"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	httpgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	protocol             = "mattercodex.control.v1"
	maximumReadBytes     = 16 << 10
	maximumItems         = 500
	maximumResourceKinds = 32
	rpcPageSize          = 100
	readTimeout          = 10 * time.Second
	writeTimeout         = 5 * time.Second
)

var errSnapshotLimit = errors.New("snapshot limit exceeded")

type ControlPlane interface {
	ListResources(context.Context, *controlplanev1.ListResourcesRequest, ...grpc.CallOption) (*controlplanev1.ListResourcesResponse, error)
	ListOwnerRuns(context.Context, *controlplanev1.ListOwnerRunsRequest, ...grpc.CallOption) (*controlplanev1.ListOwnerRunsResponse, error)
	ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error)
	ListRuntimeIncidents(context.Context, *controlplanev1.ListRuntimeIncidentsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeIncidentsResponse, error)
	ListWorkspaceBackups(context.Context, *controlplanev1.ListWorkspaceBackupsRequest, ...grpc.CallOption) (*controlplanev1.ListWorkspaceBackupsResponse, error)
	GetDiagnostics(context.Context, *controlplanev1.GetDiagnosticsRequest, ...grpc.CallOption) (*controlplanev1.GetDiagnosticsResponse, error)
}

type trackedConnection interface {
	CloseNow() error
}

type Server struct {
	control        ControlPlane
	interaction    interactiongatewayv1.MattermostTeamServiceClient
	integration    integrationgatewayv1.IntegrationManagementServiceClient
	security       *boundary.Boundary
	metrics        *internalobservability.Metrics
	logger         *slog.Logger
	originPatterns []string
	pollInterval   time.Duration
	rpcTimeout     time.Duration
	connections    atomic.Int64
	connectionMu   sync.Mutex
	active         map[trackedConnection]struct{}
	connectionWG   sync.WaitGroup
	stopping       bool
}

func New(control ControlPlane, interaction interactiongatewayv1.MattermostTeamServiceClient, integration integrationgatewayv1.IntegrationManagementServiceClient, security *boundary.Boundary, metrics *internalobservability.Metrics, logger *slog.Logger, origins []string, pollInterval, rpcTimeout time.Duration) (*Server, error) {
	if control == nil || interaction == nil || integration == nil || security == nil || metrics == nil || logger == nil || len(origins) == 0 ||
		pollInterval < time.Second || pollInterval > time.Minute || rpcTimeout < time.Second || rpcTimeout > 10*time.Second {
		return nil, errors.New("control API WebSocket configuration is invalid")
	}
	patterns := make([]string, 0, len(origins))
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Port() != "" || parsed.Path != "" {
			return nil, errors.New("control API WebSocket origin is invalid")
		}
		patterns = append(patterns, parsed.Hostname())
	}
	return &Server{control: control, interaction: interaction, integration: integration, security: security, metrics: metrics, logger: logger, originPatterns: patterns, pollInterval: pollInterval, rpcTimeout: rpcTimeout, active: make(map[trackedConnection]struct{})}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !server.security.AllowsOrigin(request.Header.Get("Origin")) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "ORIGIN_REJECTED", false)
		return
	}
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok {
		httptransport.WriteLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	csrf, ok := websocketCSRF(request)
	if !ok || !boundary.VerifyCSRFToken(identity, csrf) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	csrfCookie, err := request.Cookie(boundary.CSRFCookieName)
	if err != nil || len(csrfCookie.Value) != len(csrf) || subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(csrf)) != 1 {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{OriginPatterns: server.originPatterns, Subprotocols: []string{protocol}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	if connection.Subprotocol() != protocol {
		_ = connection.Close(websocket.StatusPolicyViolation, "required subprotocol is unavailable")
		return
	}
	server.connectionMu.Lock()
	if server.stopping {
		server.connectionMu.Unlock()
		_ = connection.Close(websocket.StatusGoingAway, "gateway stopping")
		return
	}
	server.active[connection] = struct{}{}
	server.connectionWG.Add(1)
	server.connectionMu.Unlock()
	defer func() {
		server.connectionMu.Lock()
		delete(server.active, connection)
		server.connectionMu.Unlock()
		server.connectionWG.Done()
	}()
	current := server.connections.Add(1)
	server.metrics.SetWebSockets(int(current))
	defer func() { server.metrics.SetWebSockets(int(server.connections.Add(-1))) }()
	connection.SetReadLimit(maximumReadBytes)
	ctx, cancel := context.WithDeadline(request.Context(), identity.ExpiresAt)
	defer cancel()
	subscribe, err := readSubscribe(ctx, connection)
	if err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "invalid subscription")
		return
	}
	sequence := uint64(0)
	if err := server.publish(ctx, connection, subscribe, &sequence); err != nil {
		if expectedDisconnect(ctx, err) {
			return
		}
		server.sendProblem(ctx, connection, subscribe.RequestID, err)
		return
	}
	ticker := time.NewTicker(server.pollInterval)
	defer ticker.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = connection.CloseNow()
			return
		case <-ticker.C:
			if err := server.publish(ctx, connection, subscribe, &sequence); err != nil {
				if expectedDisconnect(ctx, err) {
					return
				}
				server.sendProblem(ctx, connection, subscribe.RequestID, err)
				return
			}
		case <-ping.C:
			pingContext, pingCancel := context.WithTimeout(ctx, writeTimeout)
			err := connection.Ping(pingContext)
			pingCancel()
			if err != nil {
				return
			}
		}
	}
}

func readSubscribe(ctx context.Context, connection *websocket.Conn) (SubscribeEnvelope, error) {
	readContext, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	messageType, raw, err := connection.Read(readContext)
	if err != nil || messageType != websocket.MessageText || len(raw) == 0 || len(raw) > maximumReadBytes {
		return SubscribeEnvelope{}, errors.New("subscription frame is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input SubscribeEnvelope
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || input.Type != SubscribeMessageTypeSubscribe ||
		uuid.Validate(input.RequestID) != nil || len(input.Channels) == 0 || len(input.Channels) > 8 || len(input.ResourceKinds) > maximumResourceKinds {
		return SubscribeEnvelope{}, errors.New("subscription payload is invalid")
	}
	seenChannels := make(map[ProjectionChannel]struct{}, len(input.Channels))
	for _, value := range input.Channels {
		if _, duplicate := seenChannels[value]; duplicate {
			return SubscribeEnvelope{}, errors.New("subscription channel is duplicated")
		}
		seenChannels[value] = struct{}{}
	}
	seenKinds := make(map[ResourceKind]struct{}, len(input.ResourceKinds))
	for _, value := range input.ResourceKinds {
		if _, duplicate := seenKinds[value]; duplicate {
			return SubscribeEnvelope{}, errors.New("subscription resource kind is duplicated")
		}
		seenKinds[value] = struct{}{}
	}
	if _, needsResources := seenChannels[ProjectionChannelResources]; needsResources && len(input.ResourceKinds) == 0 {
		return SubscribeEnvelope{}, errors.New("resource subscription requires resourceKinds")
	}
	return input, nil
}

func (server *Server) publish(ctx context.Context, connection *websocket.Conn, subscribe SubscribeEnvelope, sequence *uint64) error {
	for _, channel := range subscribe.Channels {
		name := string(channel)
		items, err := server.snapshot(ctx, channel, subscribe.ResourceKinds)
		if err != nil {
			server.metrics.ObserveSnapshot(name, "failure")
			return err
		}
		*sequence++
		message := SnapshotEnvelope{Type: SnapshotMessageTypeSnapshot, RequestID: subscribe.RequestID, Channel: channel, Sequence: *sequence, SnapshotID: uuid.NewString(), Complete: true, ServerTime: time.Now().UTC().Format(time.RFC3339Nano), Items: items}
		writeContext, cancel := context.WithTimeout(ctx, writeTimeout)
		err = wsjsonWrite(writeContext, connection, message)
		cancel()
		if err != nil {
			return err
		}
		server.metrics.ObserveSnapshot(name, "success")
	}
	return nil
}

func (server *Server) snapshot(ctx context.Context, channel ProjectionChannel, kinds []ResourceKind) (SnapshotItems, error) {
	rpcContext, cancel := context.WithTimeout(ctx, server.rpcTimeout)
	defer cancel()
	switch channel {
	case ProjectionChannelRuns:
		items, err := server.allRuns(rpcContext)
		return SnapshotItems{Runs: items}, err
	case ProjectionChannelResources:
		items := make([]httpgenerated.Resource, 0)
		for _, kind := range kinds {
			protoKind := controlplanev1.ResourceKind(controlplanev1.ResourceKind_value["RESOURCE_KIND_"+string(kind)])
			if protoKind == controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED {
				return SnapshotItems{}, errors.New("resource kind is invalid")
			}
			converted, err := server.allResources(rpcContext, protoKind)
			if err != nil {
				return SnapshotItems{}, err
			}
			if len(items)+len(converted) > maximumItems {
				return SnapshotItems{}, errSnapshotLimit
			}
			items = append(items, converted...)
		}
		return SnapshotItems{Resources: items}, nil
	case ProjectionChannelIncidents:
		items, err := server.allIncidents(rpcContext)
		return SnapshotItems{Incidents: items}, err
	case ProjectionChannelConfigurationChanges:
		items, err := server.allConfigurationChanges(rpcContext)
		return SnapshotItems{ConfigurationChanges: items}, err
	case ProjectionChannelWorkspaceTeams:
		items, err := server.allMattermostTeams(rpcContext)
		return SnapshotItems{Teams: items}, err
	case ProjectionChannelProviders:
		items, err := server.allProviderConnections(rpcContext)
		return SnapshotItems{ProviderConnections: items}, err
	case ProjectionChannelIntegrations:
		items, err := server.allIntegrationConfigurations(rpcContext)
		return SnapshotItems{IntegrationConfigs: items}, err
	case ProjectionChannelApprovals:
		items, err := server.allIntegrationApprovals(rpcContext)
		return SnapshotItems{Approvals: items}, err
	case ProjectionChannelBackups:
		items, err := server.allWorkspaceBackups(rpcContext)
		return SnapshotItems{Resources: items}, err
	case ProjectionChannelHealth:
		items, err := server.currentHealth(rpcContext)
		return SnapshotItems{Health: items}, err
	default:
		return SnapshotItems{}, errors.New("subscription channel is invalid")
	}
}

func (server *Server) allMattermostTeams(ctx context.Context) ([]httpgenerated.MattermostTeam, error) {
	items := make([]httpgenerated.MattermostTeam, 0)
	token := ""
	for {
		response, err := server.interaction.ListMattermostTeams(ctx, &interactiongatewayv1.ListMattermostTeamsRequest{PageSize: rpcPageSize, Cursor: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetTeams() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			value, convertErr := httptransport.ConvertMattermostTeam(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, value)
		}
		next := response.GetNextCursor()
		if next != "" && (len(response.GetTeams()) == 0 || next == token) {
			return nil, errors.New("mattermost team pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) allProviderConnections(ctx context.Context) ([]httpgenerated.ProviderConnection, error) {
	items := make([]httpgenerated.ProviderConnection, 0)
	token := ""
	for {
		response, err := server.integration.ListProviderConnections(ctx, &integrationgatewayv1.ListProviderConnectionsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetConnections() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			value, convertErr := httptransport.ConvertProviderConnection(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, value)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetConnections()) == 0 || next == token) {
			return nil, errors.New("provider connection pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) allIntegrationConfigurations(ctx context.Context) ([]httpgenerated.IntegrationConfiguration, error) {
	items := make([]httpgenerated.IntegrationConfiguration, 0)
	token := ""
	for {
		response, err := server.integration.ListIntegrationConfigurations(ctx, &integrationgatewayv1.ListIntegrationConfigurationsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetConfigurations() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			value, convertErr := httptransport.ConvertIntegrationConfiguration(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, value)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetConfigurations()) == 0 || next == token) {
			return nil, errors.New("integration configuration pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) allIntegrationApprovals(ctx context.Context) ([]httpgenerated.IntegrationApproval, error) {
	items := make([]httpgenerated.IntegrationApproval, 0)
	token := ""
	for {
		response, err := server.integration.ListIntegrationApprovals(ctx, &integrationgatewayv1.ListIntegrationApprovalsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetApprovals() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			value, convertErr := httptransport.ConvertIntegrationApproval(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, value)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetApprovals()) == 0 || next == token) {
			return nil, errors.New("integration approval pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) allWorkspaceBackups(ctx context.Context) ([]httpgenerated.Resource, error) {
	items := make([]httpgenerated.Resource, 0)
	token := ""
	for {
		response, err := server.control.ListWorkspaceBackups(ctx, &controlplanev1.ListWorkspaceBackupsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetBackups() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			value, convertErr := httptransport.ConvertResource(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, value)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetBackups()) == 0 || next == token) {
			return nil, errors.New("workspace backup pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) currentHealth(ctx context.Context) ([]httpgenerated.HealthObservation, error) {
	observedAt := time.Now().UTC()
	control, controlErr := server.control.GetDiagnostics(ctx, &controlplanev1.GetDiagnosticsRequest{})
	interaction, interactionErr := server.interaction.CheckReadiness(ctx, &interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest{})
	integration, integrationErr := server.integration.GetManagementDiagnostics(ctx, &integrationgatewayv1.GetManagementDiagnosticsRequest{})
	if controlErr != nil {
		return nil, controlErr
	}
	if interactionErr != nil {
		return nil, interactionErr
	}
	if integrationErr != nil {
		return nil, integrationErr
	}
	if control == nil || interaction == nil || integration == nil || control.GetSchemaVersion() == 0 || interaction.GetSchemaVersion() == 0 {
		return nil, errors.New("owner health readback is unavailable")
	}
	interactionStatus := httpgenerated.HealthObservationStatusDEGRADED
	interactionValue := int64(0)
	if interaction.GetReady() {
		interactionStatus = httpgenerated.HealthObservationStatusOK
		interactionValue = 1
	}
	integrationStatus, integrationStatusOK := websocketHealthStatus(integration.GetStatus())
	if !integrationStatusOK {
		return nil, errors.New("integration health status is invalid")
	}
	items := []httpgenerated.HealthObservation{
		{Source: "CONTROL_PLANE", Component: "schema", Status: "OK", Value: int64(control.GetSchemaVersion()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "pending_outbox", Status: "UNKNOWN", Value: int64(control.GetPendingOutboxEvents()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "terminal_outbox", Status: "UNKNOWN", Value: int64(control.GetTerminalOutboxEvents()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "active_turn_leases", Status: "UNKNOWN", Value: int64(control.GetActiveTurnLeases()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "CONTROL_PLANE", Component: "queued_schedule_occurrences", Status: "UNKNOWN", Value: int64(control.GetQueuedScheduleOccurrences()), Version: int64(control.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "INTERACTION_GATEWAY", Component: "mattermost_team_working_path", Status: interactionStatus, Value: interactionValue, Version: int64(interaction.GetSchemaVersion()), ObservedAt: observedAt},
		{Source: "INTEGRATION_GATEWAY", Component: "overall", Status: integrationStatus, Value: websocketHealthValue(integrationStatus), Version: 0, ObservedAt: observedAt},
	}
	for _, item := range integration.GetDependencies() {
		status, ok := websocketHealthStatus(item.GetStatus())
		if item == nil || item.GetDependency() == "" || !ok || item.GetVersion() == 0 || item.GetCheckedAt() == nil || item.GetCheckedAt().CheckValid() != nil {
			return nil, errors.New("integration health observation is invalid")
		}
		healthValue := int64(0)
		if status == httpgenerated.HealthObservationStatusOK {
			healthValue = 1
		}
		value := httpgenerated.HealthObservation{Source: "INTEGRATION_GATEWAY", Component: item.GetDependency(), Status: status, Value: healthValue, Version: int64(item.GetVersion()), ObservedAt: item.GetCheckedAt().AsTime()}
		if digest := item.GetDigestSha256(); digest != "" {
			if len(digest) != 64 {
				return nil, errors.New("integration health digest is invalid")
			}
			if _, err := hex.DecodeString(digest); err != nil {
				return nil, errors.New("integration health digest is invalid")
			}
			normalized := httpgenerated.Sha256(strings.ToLower(digest))
			value.DigestSha256 = &normalized
		}
		items = append(items, value)
	}
	return items, nil
}

func websocketHealthValue(status httpgenerated.HealthObservationStatus) int64 {
	if status == httpgenerated.HealthObservationStatusOK {
		return 1
	}
	return 0
}

func websocketHealthStatus(value string) (httpgenerated.HealthObservationStatus, bool) {
	result, ok := map[string]httpgenerated.HealthObservationStatus{"READY": "OK", "DEGRADED": "DEGRADED", "UNAVAILABLE": "UNAVAILABLE", "UNKNOWN": "UNKNOWN"}[value]
	return result, ok
}

func (server *Server) allResources(ctx context.Context, kind controlplanev1.ResourceKind) ([]httpgenerated.Resource, error) {
	items := make([]httpgenerated.Resource, 0)
	token := ""
	for {
		response, err := server.control.ListResources(ctx, &controlplanev1.ListResourcesRequest{Kind: kind, PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetResources() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			external, err := httptransport.ConvertResource(item)
			if err != nil {
				return nil, err
			}
			items = append(items, external)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetResources()) == 0 || next == token) {
			return nil, errors.New("resource pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
		if len(items) >= maximumItems {
			return nil, errSnapshotLimit
		}
	}
}

func (server *Server) allRuns(ctx context.Context) ([]httpgenerated.RunView, error) {
	items := make([]httpgenerated.RunView, 0)
	token := ""
	for {
		response, err := server.control.ListOwnerRuns(ctx, &controlplanev1.ListOwnerRunsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetRuns() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			external, convertErr := httptransport.ConvertRunOwnerProjection(item)
			if convertErr != nil {
				return nil, convertErr
			}
			items = append(items, external)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetRuns()) == 0 || next == token) {
			return nil, errors.New("run pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
	}
}

func (server *Server) allIncidents(ctx context.Context) ([]httpgenerated.IncidentView, error) {
	items := make([]httpgenerated.IncidentView, 0)
	token := ""
	for {
		response, err := server.control.ListRuntimeIncidents(ctx, &controlplanev1.ListRuntimeIncidentsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		if len(response.GetIncidents()) != len(response.GetProjections()) {
			return nil, errors.New("incident projection page is incomplete")
		}
		for _, item := range response.GetProjections() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			external, err := httptransport.ConvertIncidentOwnerProjection(item)
			if err != nil {
				return nil, err
			}
			items = append(items, external)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetIncidents()) == 0 || next == token) {
			return nil, errors.New("incident pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
		if len(items) >= maximumItems {
			return nil, errSnapshotLimit
		}
	}
}

func (server *Server) allConfigurationChanges(ctx context.Context) ([]httpgenerated.ConfigurationChange, error) {
	items := make([]httpgenerated.ConfigurationChange, 0)
	token := ""
	scanned := 0
	for {
		response, err := server.control.ListAuditEvents(ctx, &controlplanev1.ListAuditEventsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetEvents() {
			scanned++
			if scanned > maximumItems {
				return nil, errSnapshotLimit
			}
			if !projection.IsConfigurationAction(item.GetAction()) {
				continue
			}
			external, err := httptransport.ConvertConfigurationChange(item)
			if err != nil {
				return nil, err
			}
			items = append(items, external)
		}
		next := response.GetNextPageToken()
		if next != "" && (len(response.GetEvents()) == 0 || next == token) {
			return nil, errors.New("configuration pagination did not advance")
		}
		token = next
		if token == "" {
			return items, nil
		}
		if scanned >= maximumItems {
			return nil, errSnapshotLimit
		}
	}
}

func (server *Server) Shutdown(ctx context.Context) error {
	server.connectionMu.Lock()
	server.stopping = true
	connections := make([]trackedConnection, 0, len(server.active))
	for connection := range server.active {
		connections = append(connections, connection)
	}
	server.connectionMu.Unlock()

	// Число параллельных операций ограничено глобальной квотой WebSocket.
	// coder/websocket Close не принимает context, а повторный CloseNow ждёт уже
	// начатый handshake. Поэтому первая половина budget оставлена handlers для
	// естественного выхода, затем выполняется concurrent force-close и join.
	done := make(chan struct{})
	go func() { server.connectionWG.Wait(); close(done) }()
	graceCtx := ctx
	graceCancel := func() {}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			graceCtx, graceCancel = context.WithTimeout(ctx, remaining/2)
		}
	}
	defer graceCancel()
	select {
	case <-done:
		return nil
	case <-graceCtx.Done():
	}
	for _, connection := range connections {
		connection := connection
		go func() { _ = connection.CloseNow() }()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (server *Server) sendProblem(ctx context.Context, connection *websocket.Conn, requestID string, err error) {
	code, retryable, expected := httptransport.MapRPCProblem(err)
	if errors.Is(err, errSnapshotLimit) {
		code, retryable, expected = "UNAVAILABLE", true, true
	}
	grpcStatus, isGRPC := status.FromError(err)
	if !expected && (!isGRPC || !grpcserver.IsUnexpectedCode(grpcStatus.Code())) {
		server.logger.ErrorContext(ctx, "unexpected realtime RPC outcome", "error_class", "rpc_contract")
	}
	writeContext, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = wsjsonWrite(writeContext, connection, ProblemEnvelope{Type: ProblemMessageTypeProblem, RequestID: requestID, Code: code, Retryable: retryable})
	_ = connection.Close(websocket.StatusInternalError, "projection unavailable")
}

func expectedDisconnect(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return true
	}
	if status.Code(err) == codes.Canceled {
		return true
	}
	closeStatus := websocket.CloseStatus(err)
	return closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway
}

func websocketCSRF(request *http.Request) (string, bool) {
	foundProtocol := false
	csrf := ""
	for _, header := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(header, ",") {
			value := strings.TrimSpace(raw)
			switch {
			case value == protocol:
				foundProtocol = true
			case strings.HasPrefix(value, "csrf.") && csrf == "":
				csrf = strings.TrimPrefix(value, "csrf.")
			}
		}
	}
	return csrf, foundProtocol && csrf != ""
}

func wsjsonWrite(ctx context.Context, connection *websocket.Conn, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode WebSocket message: %w", err)
	}
	return connection.Write(ctx, websocket.MessageText, raw)
}
