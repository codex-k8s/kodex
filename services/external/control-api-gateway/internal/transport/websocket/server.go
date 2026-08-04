package websockettransport

import (
	"bytes"
	"context"
	"crypto/subtle"
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
	internalobservability "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/projection"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	httpgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const (
	protocol         = "mattercodex.control.v1"
	maximumReadBytes = 16 << 10
	maximumItems     = 500
	rpcPageSize      = 100
	readTimeout      = 10 * time.Second
	writeTimeout     = 5 * time.Second
)

var errSnapshotLimit = errors.New("snapshot limit exceeded")

type ControlPlane interface {
	ListResources(context.Context, *controlplanev1.ListResourcesRequest, ...grpc.CallOption) (*controlplanev1.ListResourcesResponse, error)
	ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error)
	ListRuntimeIncidents(context.Context, *controlplanev1.ListRuntimeIncidentsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeIncidentsResponse, error)
}

type trackedConnection interface {
	CloseNow() error
}

type Server struct {
	control        ControlPlane
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

func New(control ControlPlane, security *boundary.Boundary, metrics *internalobservability.Metrics, logger *slog.Logger, origins []string, pollInterval, rpcTimeout time.Duration) (*Server, error) {
	if control == nil || security == nil || metrics == nil || logger == nil || len(origins) == 0 ||
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
	return &Server{control: control, security: security, metrics: metrics, logger: logger, originPatterns: patterns, pollInterval: pollInterval, rpcTimeout: rpcTimeout, active: make(map[trackedConnection]struct{})}, nil
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
		uuid.Validate(input.RequestID) != nil || len(input.Channels) == 0 || len(input.Channels) > 4 || len(input.ResourceKinds) > 8 {
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
		items, err := server.allResources(rpcContext, controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN)
		return SnapshotItems{Resources: items}, err
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
	default:
		return SnapshotItems{}, errors.New("subscription channel is invalid")
	}
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

func (server *Server) allIncidents(ctx context.Context) ([]httpgenerated.RuntimeIncident, error) {
	items := make([]httpgenerated.RuntimeIncident, 0)
	token := ""
	for {
		response, err := server.control.ListRuntimeIncidents(ctx, &controlplanev1.ListRuntimeIncidentsRequest{PageSize: rpcPageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, item := range response.GetIncidents() {
			if len(items) >= maximumItems {
				return nil, errSnapshotLimit
			}
			external, err := httptransport.ConvertRuntimeIncident(item)
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
