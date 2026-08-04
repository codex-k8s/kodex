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
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/projection"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	protocol         = "mattercodex.control.v1"
	maximumReadBytes = 16 << 10
	maximumItems     = 100
	incidentAction   = "record_runtime_incident"
	readTimeout      = 10 * time.Second
	writeTimeout     = 5 * time.Second
)

type ControlPlane interface {
	ListResources(context.Context, *controlplanev1.ListResourcesRequest, ...grpc.CallOption) (*controlplanev1.ListResourcesResponse, error)
	ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error)
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
	return &Server{control: control, security: security, metrics: metrics, logger: logger, originPatterns: patterns, pollInterval: pollInterval, rpcTimeout: rpcTimeout}, nil
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
		server.sendProblem(ctx, connection, subscribe.RequestId, err)
		return
	}
	ticker := time.NewTicker(server.pollInterval)
	defer ticker.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = connection.Close(websocket.StatusNormalClosure, "session ended")
			return
		case <-ticker.C:
			if err := server.publish(ctx, connection, subscribe, &sequence); err != nil {
				server.sendProblem(ctx, connection, subscribe.RequestId, err)
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

type rawSubscribe struct {
	Type          string   `json:"type"`
	RequestID     string   `json:"requestId"`
	Channels      []string `json:"channels"`
	ResourceKinds []string `json:"resourceKinds,omitempty"`
}

func readSubscribe(ctx context.Context, connection *websocket.Conn) (generated.Subscribe, error) {
	readContext, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	messageType, raw, err := connection.Read(readContext)
	if err != nil || messageType != websocket.MessageText || len(raw) == 0 || len(raw) > maximumReadBytes {
		return generated.Subscribe{}, errors.New("subscription frame is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input rawSubscribe
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || input.Type != "SUBSCRIBE" ||
		uuid.Validate(input.RequestID) != nil || len(input.Channels) == 0 || len(input.Channels) > 4 || len(input.ResourceKinds) > 8 {
		return generated.Subscribe{}, errors.New("subscription payload is invalid")
	}
	channels := make([]generated.AnonymousSchema_4, 0, len(input.Channels))
	seenChannels := make(map[string]struct{}, len(input.Channels))
	for _, value := range input.Channels {
		if _, duplicate := seenChannels[value]; duplicate {
			return generated.Subscribe{}, errors.New("subscription channel is duplicated")
		}
		seenChannels[value] = struct{}{}
		converted, ok := generated.ValuesToAnonymousSchema_4[value]
		if !ok {
			return generated.Subscribe{}, errors.New("subscription channel is invalid")
		}
		channels = append(channels, converted)
	}
	kinds := make([]generated.AnonymousSchema_6, 0, len(input.ResourceKinds))
	seenKinds := make(map[string]struct{}, len(input.ResourceKinds))
	for _, value := range input.ResourceKinds {
		if _, duplicate := seenKinds[value]; duplicate {
			return generated.Subscribe{}, errors.New("subscription resource kind is duplicated")
		}
		seenKinds[value] = struct{}{}
		converted, ok := generated.ValuesToAnonymousSchema_6[value]
		if !ok {
			return generated.Subscribe{}, errors.New("subscription resource kind is invalid")
		}
		kinds = append(kinds, converted)
	}
	if _, needsResources := seenChannels["RESOURCES"]; needsResources && len(kinds) == 0 {
		return generated.Subscribe{}, errors.New("resource subscription requires resourceKinds")
	}
	return generated.Subscribe{ReservedType: input.Type, RequestId: input.RequestID, Channels: channels, ResourceKinds: kinds}, nil
}

func (server *Server) publish(ctx context.Context, connection *websocket.Conn, subscribe generated.Subscribe, sequence *uint64) error {
	for _, channel := range subscribe.Channels {
		name, _ := channel.Value().(string)
		items, err := server.snapshot(ctx, name, subscribe.ResourceKinds)
		if err != nil {
			server.metrics.ObserveSnapshot(name, "failure")
			return err
		}
		*sequence++
		outChannel := generated.ValuesToAnonymousSchema_9[name]
		message := generated.Snapshot{ReservedType: "SNAPSHOT", RequestId: subscribe.RequestId, Channel: &outChannel, Sequence: int(*sequence), ServerTime: time.Now().UTC().Format(time.RFC3339Nano), Items: items}
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

func (server *Server) snapshot(ctx context.Context, channel string, kinds []generated.AnonymousSchema_6) ([]map[string]any, error) {
	rpcContext, cancel := context.WithTimeout(ctx, server.rpcTimeout)
	defer cancel()
	switch channel {
	case "RUNS":
		response, err := server.control.ListResources(rpcContext, &controlplanev1.ListResourcesRequest{Kind: controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN, PageSize: maximumItems})
		if err != nil {
			return nil, err
		}
		return protoItems(response.GetResources())
	case "RESOURCES":
		items := make([]map[string]any, 0)
		for _, kind := range kinds {
			name, _ := kind.Value().(string)
			protoKind := controlplanev1.ResourceKind(controlplanev1.ResourceKind_value["RESOURCE_KIND_"+name])
			if protoKind == controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED {
				return nil, errors.New("resource kind is invalid")
			}
			response, err := server.control.ListResources(rpcContext, &controlplanev1.ListResourcesRequest{Kind: protoKind, PageSize: maximumItems})
			if err != nil {
				return nil, err
			}
			converted, err := protoItems(response.GetResources())
			if err != nil {
				return nil, err
			}
			items = append(items, converted...)
			if len(items) >= maximumItems {
				return items[:maximumItems], nil
			}
		}
		return items, nil
	case "INCIDENTS":
		response, err := server.control.ListAuditEvents(rpcContext, &controlplanev1.ListAuditEventsRequest{Action: incidentAction, PageSize: maximumItems})
		if err != nil {
			return nil, err
		}
		return protoItems(response.GetEvents())
	case "CONFIGURATION_CHANGES":
		response, err := server.control.ListAuditEvents(rpcContext, &controlplanev1.ListAuditEventsRequest{PageSize: maximumItems})
		if err != nil {
			return nil, err
		}
		items := make([]proto.Message, 0, len(response.GetEvents()))
		for _, event := range response.GetEvents() {
			if projection.IsConfigurationAction(event.GetAction()) {
				items = append(items, event)
			}
		}
		return protoItems(items)
	default:
		return nil, errors.New("subscription channel is invalid")
	}
}

func protoItems[T proto.Message](values []T) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		raw, err := protojson.MarshalOptions{UseProtoNames: false}.Marshal(value)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var item map[string]any
		if decoder.Decode(&item) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return nil, errors.New("snapshot item is invalid")
		}
		items = append(items, item)
	}
	return items, nil
}

func (server *Server) sendProblem(ctx context.Context, connection *websocket.Conn, requestID string, err error) {
	code, retryable, expected := httptransport.MapRPCProblem(err)
	if !expected {
		server.logger.ErrorContext(ctx, "unexpected realtime RPC outcome", "error_class", "rpc_contract")
	}
	writeContext, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = wsjsonWrite(writeContext, connection, generated.Problem{ReservedType: "PROBLEM", RequestId: requestID, Code: code, Retryable: retryable})
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
