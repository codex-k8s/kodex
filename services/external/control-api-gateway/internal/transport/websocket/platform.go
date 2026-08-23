package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const platformSubprotocol = "mattercodex.platform.v1"

var platformEventNames = map[string]string{
	"PROJECT_CHANGED":                "PROJECT",
	"AGENT_CHANGED":                  "AGENT",
	"ARTIFACT_CHANGED":               "ARTIFACT",
	"INSTRUCTIONS_PUBLISHED":         "INSTRUCTIONS",
	"WORKFLOW_CHANGED":               "WORKFLOW",
	"SCHEDULE_CHANGED":               "SCHEDULE",
	"INTEGRATION_CONNECTION_CHANGED": "INTEGRATION_CONNECTION",
	"INTEGRATION_GRANT_CHANGED":      "INTEGRATION_GRANT",
	"MEMBERSHIP_CHANGED":             "MEMBERSHIP",
	"PLATFORM_MEMBERSHIP_CHANGED":    "PLATFORM_MEMBERSHIP",
	"SYSTEM_ASSISTANT_CHANGED":       "SYSTEM_ASSISTANT",
	"ROLE_IMAGE_RECIPE_CHANGED":      "ROLE_IMAGE_RECIPE",
}

type platformBusEnvelope struct {
	EventID          string    `json:"eventId"`
	EventName        string    `json:"eventName"`
	EventVersion     int64     `json:"eventVersion"`
	OccurredAt       time.Time `json:"occurredAt"`
	OrganizationRef  string    `json:"organizationRef"`
	ProjectRef       string    `json:"projectRef,omitempty"`
	AggregateRef     string    `json:"aggregateRef"`
	AggregateVersion int64     `json:"aggregateVersion"`
	Sequence         int64     `json:"sequence"`
	CorrelationRef   string    `json:"correlationRef"`
	CausationRef     string    `json:"causationRef,omitempty"`
	Data             struct {
		Kind        string `json:"kind"`
		State       string `json:"state,omitempty"`
		SafeSummary string `json:"safeSummary"`
	} `json:"data"`
}

type platformSignal struct {
	Sequence  int64
	EventName string
	Kind      string
}

func (server *Server) ServePlatformHTTP(writer http.ResponseWriter, request *http.Request) {
	localize := streamLocalizer(writer, request)
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok {
		httptransport.WriteLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	protocols, csrfOK := requestedProtocols(request, platformSubprotocol)
	if !csrfOK || !boundary.VerifyCSRFToken(identity, protocols.csrf) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	originPatterns := make([]string, 0, len(server.origins))
	for _, origin := range server.origins {
		originPatterns = append(originPatterns, strings.TrimPrefix(origin, "https://"))
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:   []string{platformSubprotocol},
		OriginPatterns: originPatterns,
	})
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
		server.closeProblem(streamContext, connection, "INVALID_RESUME", localize)
		return
	}
	cursor, err := server.control.Query.GetPlatformEventCursor(streamContext, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || !safeRef.MatchString(cursor.GetOrganizationRef()) || cursor.GetCurrentSequence() < 0 {
		server.closeProblem(streamContext, connection, "PLATFORM_UNAVAILABLE", localize)
		return
	}
	organizationRef := cursor.GetOrganizationRef()
	signals := make(chan platformSignal, 128)
	overflow := make(chan struct{}, 1)
	subscription, err := server.nats.Subscribe("control_plane.platform."+organizationRef+".events", func(message *nats.Msg) {
		signal, valid := decodePlatformSignal(message.Data, organizationRef)
		if !valid {
			return
		}
		select {
		case signals <- signal:
		default:
			select {
			case overflow <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		server.closeProblem(streamContext, connection, "STREAM_UNAVAILABLE", localize)
		return
	}
	defer subscription.Unsubscribe()
	if err = server.nats.FlushTimeout(2 * time.Second); err != nil {
		server.closeProblem(streamContext, connection, "STREAM_UNAVAILABLE", localize)
		return
	}
	// Повторный owner-scoped cursor read закрывает окно между eligibility read
	// и live subscription. Platform events служат только invalidation-сигналами:
	// browser никогда не получает project/aggregate refs из org-wide subject.
	cursor, err = server.control.Query.GetPlatformEventCursor(streamContext, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != organizationRef || cursor.GetCurrentSequence() < 0 {
		server.closeProblem(streamContext, connection, "PLATFORM_UNAVAILABLE", localize)
		return
	}
	latest := cursor.GetCurrentSequence()
	if resume.AfterSequence != latest {
		if !server.write(streamContext, connection, map[string]any{
			"type":            "PLATFORM_RESYNC_REQUIRED",
			"requestRef":      resume.RequestRef,
			"currentSequence": latest,
			"reason":          "AUTHORITATIVE_READ_REQUIRED",
		}) {
			return
		}
	}
	if !server.write(streamContext, connection, map[string]any{
		"type":           "PLATFORM_STREAM_READY",
		"requestRef":     resume.RequestRef,
		"latestSequence": latest,
	}) {
		return
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-streamContext.Done():
			return
		case <-overflow:
			var synchronized bool
			latest, synchronized = server.synchronizePlatformCursor(streamContext, connection, resume.RequestRef, organizationRef, latest, localize)
			if !synchronized {
				return
			}
		case signal := <-signals:
			if signal.Sequence <= latest {
				continue
			}
			if signal.Sequence != latest+1 {
				var synchronized bool
				latest, synchronized = server.synchronizePlatformCursor(streamContext, connection, resume.RequestRef, organizationRef, latest, localize)
				if !synchronized {
					return
				}
				continue
			}
			if !server.write(streamContext, connection, map[string]any{
				"type":       "PLATFORM_INVALIDATED",
				"requestRef": resume.RequestRef,
				"sequence":   signal.Sequence,
				"eventName":  signal.EventName,
				"kind":       signal.Kind,
			}) {
				return
			}
			latest = signal.Sequence
		case now := <-ticker.C:
			// Сверка с server-owned cursor закрывает тихое окно потери Core NATS
			// сигналов при broker reconnect без разрыва browser WebSocket.
			var synchronized bool
			latest, synchronized = server.synchronizePlatformCursor(streamContext, connection, resume.RequestRef, organizationRef, latest, localize)
			if !synchronized {
				return
			}
			if !server.write(streamContext, connection, map[string]any{
				"type":           "PLATFORM_HEARTBEAT",
				"serverTime":     now.UTC().Format(time.RFC3339Nano),
				"latestSequence": latest,
			}) {
				return
			}
		}
	}
}

func (server *Server) synchronizePlatformCursor(ctx context.Context, connection *websocket.Conn, requestRef, organizationRef string, latest int64, localize func(string) string) (int64, bool) {
	cursor, err := server.control.Query.GetPlatformEventCursor(ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != organizationRef || cursor.GetCurrentSequence() < latest {
		server.closeProblem(ctx, connection, "PLATFORM_UNAVAILABLE", localize)
		return latest, false
	}
	current := cursor.GetCurrentSequence()
	if current == latest {
		return latest, true
	}
	if !server.write(ctx, connection, map[string]any{
		"type":            "PLATFORM_RESYNC_REQUIRED",
		"requestRef":      requestRef,
		"currentSequence": current,
		"reason":          "AUTHORITATIVE_READ_REQUIRED",
	}) {
		return latest, false
	}
	return current, true
}

func decodePlatformSignal(payload []byte, organizationRef string) (platformSignal, bool) {
	if len(payload) == 0 || len(payload) > maximumFrameBytes {
		return platformSignal{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope platformBusEnvelope
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return platformSignal{}, false
	}
	kind, known := platformEventNames[envelope.EventName]
	if !known || envelope.Data.Kind != kind || envelope.OrganizationRef != organizationRef ||
		envelope.EventVersion != 1 || envelope.AggregateVersion < 1 || envelope.Sequence < 1 ||
		envelope.OccurredAt.IsZero() || uuid.Validate(envelope.EventID) != nil ||
		!safeRef.MatchString(envelope.AggregateRef) || !safeRef.MatchString(envelope.CorrelationRef) ||
		len(envelope.Data.SafeSummary) > 2000 {
		return platformSignal{}, false
	}
	if envelope.ProjectRef != "" && !safeRef.MatchString(envelope.ProjectRef) {
		return platformSignal{}, false
	}
	return platformSignal{Sequence: envelope.Sequence, EventName: envelope.EventName, Kind: kind}, true
}
