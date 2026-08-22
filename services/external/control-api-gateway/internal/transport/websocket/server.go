// Package websockettransport реализует resumable owner run stream.
package websockettransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	"github.com/nats-io/nats.go"
)

const (
	maximumFrameBytes = 64 << 10
	writeTimeout      = 5 * time.Second
	readTimeout       = 10 * time.Second
	heartbeatInterval = 15 * time.Second
)

var safeRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

type Server struct {
	control *controlplaneclient.Client
	nats    *nats.Conn
	origins []string
}

func New(control *controlplaneclient.Client, connection *nats.Conn, origins []string) (*Server, error) {
	if control == nil || connection == nil || !connection.IsConnected() || len(origins) == 0 {
		return nil, errors.New("realtime server configuration is invalid")
	}
	return &Server{control: control, nats: connection, origins: origins}, nil
}

type resumeEnvelope struct {
	Type          string `json:"type"`
	RequestRef    string `json:"requestRef"`
	AfterSequence int64  `json:"afterSequence"`
}
type busEnvelope struct {
	RootRunRef string `json:"rootRunRef"`
	Sequence   int64  `json:"sequence"`
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	localize := func(messageID string) string { return messageID }
	if localized, ok := writer.(interface{ Localize(string) string }); ok {
		localize = localized.Localize
	}
	runRef := request.PathValue("runRef")
	if !safeRef.MatchString(runRef) {
		httptransport.WriteLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok {
		httptransport.WriteLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	protocols, csrfOK := requestedProtocols(request)
	if !csrfOK || !boundary.VerifyCSRFToken(identity, protocols.csrf) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	originPatterns := make([]string, 0, len(server.origins))
	for _, origin := range server.origins {
		originPatterns = append(originPatterns, strings.TrimPrefix(origin, "https://"))
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{Subprotocols: []string{"mattercodex.run.v1"}, OriginPatterns: originPatterns})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(maximumFrameBytes)
	streamContext, cancel := context.WithCancel(context.WithoutCancel(request.Context()))
	defer cancel()
	readContext, cancelRead := context.WithTimeout(streamContext, readTimeout)
	var resume resumeEnvelope
	err = wsjson.Read(readContext, connection, &resume)
	cancelRead()
	if err != nil || resume.Type != "RESUME" || !safeRef.MatchString(resume.RequestRef) || resume.AfterSequence < 0 {
		closeProblem(connection, "INVALID_RESUME")
		return
	}
	snapshot, err := server.control.Query.GetRunGraph(streamContext, &controlplanev1.GetRunGraphRequest{RunRef: runRef})
	if err != nil {
		closeProblem(connection, "RUN_UNAVAILABLE")
		return
	}
	rootRef := snapshot.GetRun().GetRootRunRef()
	if !safeRef.MatchString(rootRef) {
		closeProblem(connection, "INTERNAL")
		return
	}
	signals := make(chan int64, 1)
	subscription, err := server.nats.Subscribe("control_plane.run.*."+rootRef+".events", func(message *nats.Msg) {
		if len(message.Data) > maximumFrameBytes {
			return
		}
		var event busEnvelope
		if json.Unmarshal(message.Data, &event) != nil || event.RootRunRef != rootRef || event.Sequence < 1 {
			return
		}
		select {
		case signals <- event.Sequence:
		default:
		}
	})
	if err != nil {
		closeProblem(connection, "STREAM_UNAVAILABLE")
		return
	}
	defer subscription.Unsubscribe()
	if err = server.nats.FlushTimeout(2 * time.Second); err != nil {
		closeProblem(connection, "STREAM_UNAVAILABLE")
		return
	}
	// Повторное чтение после регистрации subscription закрывает окно между
	// owner eligibility read и началом live-сигналов. Данные всё равно читает
	// авторитетный control-plane; NATS только будит bounded catch-up.
	snapshot, err = server.control.Query.GetRunGraph(streamContext, &controlplanev1.GetRunGraphRequest{RunRef: rootRef})
	if err != nil {
		closeProblem(connection, "RUN_UNAVAILABLE")
		return
	}
	currentSequence := snapshot.GetGraph().GetSequence()
	latest := resume.AfterSequence
	if latest == 0 {
		if !server.writeSnapshot(streamContext, connection, resume.RequestRef, rootRef, snapshot, localize) {
			return
		}
		latest = currentSequence
	} else if latest > currentSequence {
		if !server.writeResync(streamContext, connection, resume.RequestRef, rootRef, latest, "PROJECTION_RECOVERED") || !server.writeSnapshot(streamContext, connection, resume.RequestRef, rootRef, snapshot, localize) {
			return
		}
		latest = currentSequence
	} else {
		latest, err = server.catchUp(streamContext, connection, resume.RequestRef, rootRef, latest, localize)
		if err != nil {
			if !server.writeResync(streamContext, connection, resume.RequestRef, rootRef, resume.AfterSequence, "GAP_DETECTED") || !server.writeSnapshot(streamContext, connection, resume.RequestRef, rootRef, snapshot, localize) {
				return
			}
			latest = currentSequence
		}
	}
	if !server.write(streamContext, connection, map[string]any{"type": "STREAM_READY", "requestRef": resume.RequestRef, "runRef": rootRef, "latestSequence": latest}) {
		return
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-streamContext.Done():
			return
		case <-signals:
			next, catchErr := server.catchUp(streamContext, connection, resume.RequestRef, rootRef, latest, localize)
			if catchErr != nil {
				closeProblem(connection, "GAP_UNRECOVERABLE")
				return
			}
			latest = next
		case now := <-ticker.C:
			if !server.write(streamContext, connection, map[string]any{"type": "HEARTBEAT", "serverTime": now.UTC().Format(time.RFC3339Nano), "latestSequence": latest}) {
				return
			}
		}
	}
}

func (server *Server) writeSnapshot(ctx context.Context, connection *websocket.Conn, requestRef, rootRef string, snapshot *controlplanev1.GetRunGraphResponse, localize func(string) string) bool {
	graph, err := httptransport.ProtoMap(snapshot.GetGraph())
	if err != nil {
		return false
	}
	httptransport.LocalizeSafeErrors(graph, localize)
	return server.write(ctx, connection, map[string]any{"type": "GRAPH_SNAPSHOT", "requestRef": requestRef, "runRef": rootRef, "sequence": snapshot.GetGraph().GetSequence(), "snapshot": graph})
}

func (server *Server) writeResync(ctx context.Context, connection *websocket.Conn, requestRef, rootRef string, expectedAfter int64, reason string) bool {
	return server.write(ctx, connection, map[string]any{"type": "RESYNC_REQUIRED", "requestRef": requestRef, "runRef": rootRef, "expectedAfterSequence": expectedAfter, "reason": reason})
}

type protocolSelection struct{ csrf string }

func requestedProtocols(request *http.Request) (protocolSelection, bool) {
	var result protocolSelection
	foundBase := false
	for _, header := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(header, ",") {
			value := strings.TrimSpace(raw)
			if value == "mattercodex.run.v1" {
				foundBase = true
			}
			if strings.HasPrefix(value, "csrf.") && len(value) > 5 {
				if result.csrf != "" {
					return protocolSelection{}, false
				}
				result.csrf = strings.TrimPrefix(value, "csrf.")
			}
		}
	}
	return result, foundBase && result.csrf != ""
}

func (server *Server) catchUp(ctx context.Context, connection *websocket.Conn, requestRef, rootRef string, after int64, localize func(string) string) (int64, error) {
	return readCatchUp(ctx, server.control.Query, rootRef, after, func(event *controlplanev1.RunEvent) error {
		value, encodeErr := httptransport.ProtoMap(event)
		if encodeErr != nil {
			return encodeErr
		}
		httptransport.LocalizeSafeErrors(value, localize)
		if !server.write(ctx, connection, map[string]any{"type": "RUN_EVENT", "requestRef": requestRef, "runRef": rootRef, "sequence": event.GetSequence(), "event": value}) {
			return errors.New("websocket write failed")
		}
		return nil
	})
}

func readCatchUp(ctx context.Context, client controlplanev1.PlatformQueryServiceClient, rootRef string, after int64, consume func(*controlplanev1.RunEvent) error) (int64, error) {
	latest := after
	for page := 0; page < 20; page++ {
		response, err := client.ListRunEvents(ctx, &controlplanev1.ListRunEventsRequest{RunRef: rootRef, AfterSequence: latest, Limit: 200})
		if err != nil {
			return latest, err
		}
		if len(response.GetEvents()) == 0 {
			if response.GetCurrentSequence() != latest {
				return latest, errors.New("run event gap")
			}
			return latest, nil
		}
		for _, event := range response.GetEvents() {
			if event.GetSequence() <= latest {
				continue
			}
			if event.GetSequence() != latest+1 {
				return latest, errors.New("run event gap")
			}
			if err := consume(event); err != nil {
				return latest, err
			}
			latest = event.GetSequence()
		}
		if response.GetComplete() {
			if latest != response.GetCurrentSequence() {
				return latest, errors.New("incomplete run event catch-up")
			}
			return latest, nil
		}
	}
	return latest, errors.New("run catch-up bound exceeded")
}
func (server *Server) write(ctx context.Context, connection *websocket.Conn, value any) bool {
	bounded, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(bounded, connection, value) == nil
}
func closeProblem(connection *websocket.Conn, code string) {
	_ = connection.Close(websocket.StatusTryAgainLater, code)
}

func (server *Server) Check(ctx context.Context) error {
	if server == nil || server.nats == nil || !server.nats.IsConnected() {
		return errors.New("realtime NATS consumer is unavailable")
	}
	return server.nats.FlushWithContext(ctx)
}
