// Package websockettransport реализует единый resumable owner session stream.
package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const (
	maximumFrameBytes       = 64 << 10
	maximumRunSubscriptions = 32
	maximumOutboundFrames   = 256
	maximumInboundCommands  = 64
	writeTimeout            = 5 * time.Second
	readTimeout             = 10 * time.Second
	heartbeatInterval       = 15 * time.Second
	pingInterval            = 30 * time.Second
	sessionSubprotocol      = "kodex.session.v1"
	platformStreamRef       = "PLATFORM"
)

var (
	safeRef             = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)
	errOutboundOverflow = errors.New("realtime outbound queue overflow")
	errPeerClosed       = errors.New("realtime peer closed")
)

type queryClient interface {
	GetRunGraph(context.Context, *controlplanev1.GetRunGraphRequest, ...grpc.CallOption) (*controlplanev1.GetRunGraphResponse, error)
	ListRunEvents(context.Context, *controlplanev1.ListRunEventsRequest, ...grpc.CallOption) (*controlplanev1.ListRunEventsResponse, error)
	GetPlatformEventCursor(context.Context, *controlplanev1.GetPlatformEventCursorRequest, ...grpc.CallOption) (*controlplanev1.GetPlatformEventCursorResponse, error)
}

type Server struct {
	query   queryClient
	nats    *nats.Conn
	origins []string
}

func New(control *controlplaneclient.Client, connection *nats.Conn, origins []string) (*Server, error) {
	if control == nil || control.Query == nil || connection == nil || !connection.IsConnected() || len(origins) == 0 {
		return nil, errors.New("realtime server configuration is invalid")
	}
	return &Server{query: control.Query, nats: connection, origins: origins}, nil
}

type busEnvelope struct {
	RootRunRef string `json:"rootRunRef"`
	Sequence   int64  `json:"sequence"`
}

type streamProblemSpec struct {
	status    int
	retryable bool
}

var streamProblemSpecs = map[string]streamProblemSpec{
	"INTERNAL":              {status: http.StatusInternalServerError},
	"INVALID_COMMAND":       {status: http.StatusBadRequest},
	"SESSION_EXPIRED":       {status: http.StatusUnauthorized},
	"SESSION_REPLACED":      {status: http.StatusConflict, retryable: true},
	"PLATFORM_UNAVAILABLE":  {status: http.StatusServiceUnavailable, retryable: true},
	"RUN_UNAVAILABLE":       {status: http.StatusServiceUnavailable, retryable: true},
	"STREAM_UNAVAILABLE":    {status: http.StatusServiceUnavailable, retryable: true},
	"BACKPRESSURE_EXCEEDED": {status: http.StatusServiceUnavailable, retryable: true},
}

type protocolSelection struct{ csrf string }

type outboundFrame struct {
	value       generated.SessionStream
	closeCode   websocket.StatusCode
	closeReason string
	written     chan struct{}
}

type sessionCommand interface{ isSessionCommand() }

type subscribeRunCommand struct{ generated.SubscribeRunEnvelope }
type unsubscribeRunCommand struct {
	generated.UnsubscribeRunEnvelope
}

func (subscribeRunCommand) isSessionCommand()   {}
func (unsubscribeRunCommand) isSessionCommand() {}

type runSubscription struct {
	requestRef   string
	rootRef      string
	cursor       int64
	subscription *nats.Subscription
	pending      atomic.Bool
	available    bool
}

type sessionMultiplexer struct {
	server             *Server
	ctx                context.Context
	requestRef         string
	localize           func(string) string
	outbound           chan<- outboundFrame
	commands           <-chan sessionCommand
	readErrors         <-chan error
	platformSignals    chan platformSignal
	runSignals         chan string
	overflow           chan struct{}
	platformRequestRef string
	organizationRef    string
	platformCursor     int64
	platformAvailable  bool
	platformSub        *nats.Subscription
	runs               map[string]*runSubscription
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.ServeSessionHTTP(writer, request)
}

func (server *Server) ServeSessionHTTP(writer http.ResponseWriter, request *http.Request) {
	localize := streamLocalizer(writer, request)
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok {
		httptransport.WriteLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	protocols, csrfOK := requestedProtocols(request, sessionSubprotocol)
	if !csrfOK || !boundary.VerifyCSRFToken(identity, protocols.csrf) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	originPatterns := make([]string, 0, len(server.origins))
	for _, origin := range server.origins {
		originPatterns = append(originPatterns, strings.TrimPrefix(origin, "https://"))
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:   []string{sessionSubprotocol},
		OriginPatterns: originPatterns,
	})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	if connection.Subprotocol() != sessionSubprotocol {
		return
	}
	connection.SetReadLimit(maximumFrameBytes)
	streamContext, cancel := context.WithCancel(context.WithoutCancel(request.Context()))
	defer cancel()

	resume, err := readInitialResume(streamContext, connection)
	if err != nil {
		writeInitialProblem(streamContext, connection, localize, "INVALID_COMMAND")
		return
	}
	if identity.ExpiresAt.IsZero() || !identity.ExpiresAt.After(time.Now()) {
		writeInitialProblem(streamContext, connection, localize, "SESSION_EXPIRED")
		return
	}

	outbound := make(chan outboundFrame, maximumOutboundFrames)
	commands := make(chan sessionCommand, maximumInboundCommands)
	readErrors := make(chan error, 1)
	writerErrors := make(chan error, 1)
	go sessionWriter(streamContext, connection, outbound, writerErrors)
	go sessionReader(streamContext, connection, commands, readErrors)

	multiplexer := &sessionMultiplexer{
		server: server, ctx: streamContext, requestRef: resume.RequestRef,
		localize: localize, outbound: outbound, commands: commands, readErrors: readErrors,
		platformSignals: make(chan platformSignal, 128), runSignals: make(chan string, maximumRunSubscriptions),
		overflow: make(chan struct{}, 1), platformRequestRef: resume.RequestRef,
		runs: make(map[string]*runSubscription, maximumRunSubscriptions),
	}
	defer multiplexer.closeSubscriptions()
	if err := multiplexer.initialize(resume); err != nil {
		multiplexer.terminate("PLATFORM_UNAVAILABLE", websocket.StatusTryAgainLater)
		return
	}

	expiry := time.NewTimer(time.Until(identity.ExpiresAt))
	heartbeat := time.NewTicker(heartbeatInterval)
	defer expiry.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-streamContext.Done():
			return
		case <-expiry.C:
			multiplexer.terminate("SESSION_EXPIRED", websocket.StatusPolicyViolation)
			return
		case <-writerErrors:
			return
		case err := <-readErrors:
			if errors.Is(err, errOutboundOverflow) {
				multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			} else if !errors.Is(err, errPeerClosed) {
				multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
			}
			return
		case command := <-commands:
			if !multiplexer.applyCommand(command) {
				return
			}
		case signal := <-multiplexer.platformSignals:
			if !multiplexer.applyPlatformSignal(signal) {
				return
			}
		case runRef := <-multiplexer.runSignals:
			subscription := multiplexer.runs[runRef]
			if subscription == nil {
				continue
			}
			subscription.pending.Store(false)
			if !multiplexer.synchronizeRun(subscription) {
				return
			}
		case <-multiplexer.overflow:
			multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			return
		case now := <-heartbeat.C:
			if !multiplexer.heartbeat(now) {
				return
			}
		}
	}
}

func (multiplexer *sessionMultiplexer) initialize(resume generated.SessionResumeEnvelope) error {
	if err := multiplexer.initializePlatform(resume.PlatformAfterSequence); err != nil {
		return err
	}
	for _, run := range resume.Runs {
		if !multiplexer.subscribeRun(run.RunRef, run.AfterSequence, resume.RequestRef) {
			return errOutboundOverflow
		}
	}
	streams := []generated.StreamCursor{{StreamKind: generated.StreamKindPlatform, StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor}}
	for _, runRef := range multiplexer.sortedRunRefs() {
		run := multiplexer.runs[runRef]
		streams = append(streams, generated.StreamCursor{StreamKind: generated.StreamKindRun, StreamRef: runRef, Cursor: run.cursor})
	}
	if !multiplexer.send(generated.SessionReadyEnvelope{Type: "SESSION_READY", RequestRef: resume.RequestRef, Streams: streams}) {
		return errOutboundOverflow
	}
	return nil
}

func (multiplexer *sessionMultiplexer) initializePlatform(after int64) error {
	cursor, err := multiplexer.server.query.GetPlatformEventCursor(multiplexer.ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || !safeRef.MatchString(cursor.GetOrganizationRef()) || cursor.GetCurrentSequence() < 0 {
		return errors.New("platform cursor is unavailable")
	}
	multiplexer.organizationRef = cursor.GetOrganizationRef()
	multiplexer.platformSub, err = multiplexer.server.nats.Subscribe("control_plane.platform."+multiplexer.organizationRef+".events", func(message *nats.Msg) {
		signal, valid := decodePlatformSignal(message.Data, multiplexer.organizationRef)
		if !valid {
			return
		}
		select {
		case multiplexer.platformSignals <- signal:
		default:
			multiplexer.signalOverflow()
		}
	})
	if err != nil || multiplexer.server.nats.FlushTimeout(2*time.Second) != nil {
		return errors.New("platform wake subscription is unavailable")
	}
	cursor, err = multiplexer.server.query.GetPlatformEventCursor(multiplexer.ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != multiplexer.organizationRef || cursor.GetCurrentSequence() < 0 {
		return errors.New("platform cursor changed during subscription")
	}
	multiplexer.platformCursor = cursor.GetCurrentSequence()
	multiplexer.platformAvailable = true
	if after != multiplexer.platformCursor && !multiplexer.send(generated.PlatformResyncEnvelope{
		Type: "PLATFORM_RESYNC_REQUIRED", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor,
		Reason: "AUTHORITATIVE_READ_REQUIRED",
	}) {
		return errOutboundOverflow
	}
	if !multiplexer.send(generated.PlatformReadyEnvelope{
		Type: "PLATFORM_READY", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor,
	}) {
		return errOutboundOverflow
	}
	return nil
}

func (multiplexer *sessionMultiplexer) applyCommand(command sessionCommand) bool {
	switch value := command.(type) {
	case subscribeRunCommand:
		if _, exists := multiplexer.runs[value.RunRef]; !exists && len(multiplexer.runs) >= maximumRunSubscriptions {
			multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
			return false
		}
		return multiplexer.subscribeRun(value.RunRef, value.AfterSequence, value.RequestRef)
	case unsubscribeRunCommand:
		cursor := int64(0)
		if existing := multiplexer.runs[value.RunRef]; existing != nil {
			cursor = existing.cursor
			_ = existing.subscription.Unsubscribe()
			delete(multiplexer.runs, value.RunRef)
		}
		return multiplexer.send(generated.RunUnsubscribedEnvelope{
			Type: "RUN_UNSUBSCRIBED", RequestRef: value.RequestRef, StreamKind: "RUN",
			StreamRef: value.RunRef, Cursor: cursor,
		})
	default:
		multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
		return false
	}
}

func (multiplexer *sessionMultiplexer) subscribeRun(runRef string, after int64, requestRef string) bool {
	snapshot, err := multiplexer.server.query.GetRunGraph(multiplexer.ctx, &controlplanev1.GetRunGraphRequest{RunRef: runRef})
	if err != nil || snapshot.GetRun().GetRootRunRef() != runRef || !safeRef.MatchString(runRef) {
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "RUN_UNAVAILABLE")
	}
	if existing := multiplexer.runs[runRef]; existing != nil {
		_ = existing.subscription.Unsubscribe()
		delete(multiplexer.runs, runRef)
	}
	subscription := &runSubscription{requestRef: requestRef, rootRef: runRef, cursor: after}
	var subscribeErr error
	subscription.subscription, subscribeErr = multiplexer.server.nats.Subscribe("control_plane.run.*."+runRef+".events", func(message *nats.Msg) {
		if !validRunSignal(message.Data, runRef) || !subscription.pending.CompareAndSwap(false, true) {
			return
		}
		select {
		case multiplexer.runSignals <- runRef:
		default:
			subscription.pending.Store(false)
			multiplexer.signalOverflow()
		}
	})
	if subscribeErr != nil || multiplexer.server.nats.FlushTimeout(2*time.Second) != nil {
		if subscription.subscription != nil {
			_ = subscription.subscription.Unsubscribe()
		}
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "STREAM_UNAVAILABLE")
	}
	snapshot, err = multiplexer.server.query.GetRunGraph(multiplexer.ctx, &controlplanev1.GetRunGraphRequest{RunRef: runRef})
	if err != nil || snapshot.GetRun().GetRootRunRef() != runRef {
		_ = subscription.subscription.Unsubscribe()
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "RUN_UNAVAILABLE")
	}
	multiplexer.runs[runRef] = subscription
	return multiplexer.recoverRun(subscription, snapshot, after)
}

func (multiplexer *sessionMultiplexer) recoverRun(subscription *runSubscription, snapshot *controlplanev1.GetRunGraphResponse, requestedAfter int64) bool {
	subscription.available = false
	current := snapshot.GetGraph().GetSequence()
	if requestedAfter == 0 || requestedAfter > current {
		if requestedAfter > current && !multiplexer.send(generated.RunResyncEnvelope{
			Type: "RUN_RESYNC_REQUIRED", RequestRef: subscription.requestRef, StreamKind: "RUN",
			StreamRef: subscription.rootRef, Cursor: current, RequestedAfterCursor: requestedAfter,
			Reason: generated.ResyncReasonProjectionRecovered,
		}) {
			return false
		}
		if !multiplexer.sendRunSnapshot(subscription, snapshot) {
			return false
		}
		subscription.cursor = current
	} else {
		latest, err := multiplexer.catchUp(subscription, requestedAfter)
		if err != nil {
			if errors.Is(err, errOutboundOverflow) {
				return false
			}
			if !multiplexer.send(generated.RunResyncEnvelope{
				Type: "RUN_RESYNC_REQUIRED", RequestRef: subscription.requestRef, StreamKind: "RUN",
				StreamRef: subscription.rootRef, Cursor: current, RequestedAfterCursor: requestedAfter,
				Reason: generated.ResyncReasonGapDetected,
			}) || !multiplexer.sendRunSnapshot(subscription, snapshot) {
				return false
			}
			latest = current
		}
		subscription.cursor = latest
	}
	if !multiplexer.send(generated.RunReadyEnvelope{
		Type: "RUN_READY", RequestRef: subscription.requestRef, StreamKind: "RUN",
		StreamRef: subscription.rootRef, Cursor: subscription.cursor,
	}) {
		return false
	}
	subscription.available = true
	return true
}

func (multiplexer *sessionMultiplexer) synchronizeRun(subscription *runSubscription) bool {
	latest, err := multiplexer.catchUp(subscription, subscription.cursor)
	if err == nil {
		subscription.cursor = latest
		subscription.available = true
		return true
	}
	if errors.Is(err, errOutboundOverflow) {
		return false
	}
	snapshot, snapshotErr := multiplexer.server.query.GetRunGraph(multiplexer.ctx, &controlplanev1.GetRunGraphRequest{RunRef: subscription.rootRef})
	if snapshotErr != nil {
		subscription.available = false
		return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
	}
	return multiplexer.recoverRun(subscription, snapshot, subscription.cursor)
}

func (multiplexer *sessionMultiplexer) catchUp(subscription *runSubscription, after int64) (int64, error) {
	return readCatchUp(multiplexer.ctx, multiplexer.server.query, subscription.rootRef, after, func(event *controlplanev1.RunEvent) error {
		projected, err := projectRunEvent(event, multiplexer.localize)
		if err != nil {
			return err
		}
		if !multiplexer.send(generated.RunEventEnvelope{
			Type: "RUN_EVENT", RequestRef: subscription.requestRef, StreamKind: "RUN",
			StreamRef: subscription.rootRef, Cursor: event.GetSequence(), Event: projected,
		}) {
			return errOutboundOverflow
		}
		return nil
	})
}

func (multiplexer *sessionMultiplexer) sendRunSnapshot(subscription *runSubscription, snapshot *controlplanev1.GetRunGraphResponse) bool {
	projected, err := projectRunGraph(snapshot.GetGraph(), multiplexer.localize)
	if err != nil {
		return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "INTERNAL")
	}
	return multiplexer.send(generated.RunSnapshotEnvelope{
		Type: "RUN_GRAPH_SNAPSHOT", RequestRef: subscription.requestRef, StreamKind: "RUN",
		StreamRef: subscription.rootRef, Cursor: snapshot.GetGraph().GetSequence(), Snapshot: projected,
	})
}

func (multiplexer *sessionMultiplexer) applyPlatformSignal(signal platformSignal) bool {
	if signal.Sequence <= multiplexer.platformCursor {
		return true
	}
	if signal.Sequence != multiplexer.platformCursor+1 {
		return multiplexer.synchronizePlatform()
	}
	if !multiplexer.send(generated.PlatformInvalidatedEnvelope{
		Type: "PLATFORM_INVALIDATED", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: signal.Sequence,
		EventName: generated.PlatformEventName(signal.EventName), Kind: generated.PlatformResourceKind(signal.Kind),
	}) {
		return false
	}
	multiplexer.platformCursor = signal.Sequence
	return true
}

func (multiplexer *sessionMultiplexer) synchronizePlatform() bool {
	cursor, err := multiplexer.server.query.GetPlatformEventCursor(multiplexer.ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != multiplexer.organizationRef || cursor.GetCurrentSequence() < multiplexer.platformCursor {
		multiplexer.platformAvailable = false
		return multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, "PLATFORM_UNAVAILABLE")
	}
	multiplexer.platformAvailable = true
	current := cursor.GetCurrentSequence()
	if current == multiplexer.platformCursor {
		return true
	}
	if !multiplexer.send(generated.PlatformResyncEnvelope{
		Type: "PLATFORM_RESYNC_REQUIRED", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: current,
		Reason: "AUTHORITATIVE_READ_REQUIRED",
	}) {
		return false
	}
	multiplexer.platformCursor = current
	return true
}

func (multiplexer *sessionMultiplexer) heartbeat(now time.Time) bool {
	if !multiplexer.synchronizePlatform() {
		return false
	}
	if multiplexer.platformAvailable && !multiplexer.send(generated.StreamHeartbeatEnvelope{
		Type: "STREAM_HEARTBEAT", StreamKind: generated.StreamKindPlatform,
		StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor, ServerTime: now.UTC().Format(time.RFC3339Nano),
	}) {
		return false
	}
	for _, runRef := range multiplexer.sortedRunRefs() {
		run := multiplexer.runs[runRef]
		if !multiplexer.synchronizeRun(run) {
			return false
		}
		if run.available && !multiplexer.send(generated.StreamHeartbeatEnvelope{
			Type: "STREAM_HEARTBEAT", StreamKind: generated.StreamKindRun,
			StreamRef: runRef, Cursor: run.cursor, ServerTime: now.UTC().Format(time.RFC3339Nano),
		}) {
			return false
		}
	}
	return true
}

func (multiplexer *sessionMultiplexer) sendStreamProblem(requestRef, kind, ref string, cursor int64, code string) bool {
	spec := problemSpec(code)
	return multiplexer.send(generated.StreamProblemEnvelope{
		Type: "STREAM_PROBLEM", RequestRef: requestRef, StreamKind: generated.StreamKind(kind),
		StreamRef: ref, Cursor: cursor, Status: spec.status, Code: generated.ProblemCode(code),
		Title: multiplexer.localize(code), Retryable: spec.retryable,
	})
}

func (multiplexer *sessionMultiplexer) send(value generated.SessionStream) bool {
	select {
	case multiplexer.outbound <- outboundFrame{value: value}:
		return true
	default:
		multiplexer.signalOverflow()
		return false
	}
}

func (multiplexer *sessionMultiplexer) terminate(code string, status websocket.StatusCode) {
	spec := problemSpec(code)
	written := make(chan struct{})
	select {
	case multiplexer.outbound <- outboundFrame{value: generated.SessionProblemEnvelope{
		Type: "SESSION_PROBLEM", RequestRef: multiplexer.requestRef, Status: spec.status,
		Code: generated.ProblemCode(code), Title: multiplexer.localize(code), Retryable: spec.retryable,
	}, closeCode: status, closeReason: code, written: written}:
	default:
		return
	}
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	select {
	case <-written:
	case <-timer.C:
	case <-multiplexer.ctx.Done():
	}
}

func (multiplexer *sessionMultiplexer) signalOverflow() {
	select {
	case multiplexer.overflow <- struct{}{}:
	default:
	}
}

func (multiplexer *sessionMultiplexer) sortedRunRefs() []string {
	refs := make([]string, 0, len(multiplexer.runs))
	for ref := range multiplexer.runs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func (multiplexer *sessionMultiplexer) closeSubscriptions() {
	if multiplexer.platformSub != nil {
		_ = multiplexer.platformSub.Unsubscribe()
	}
	for _, run := range multiplexer.runs {
		_ = run.subscription.Unsubscribe()
	}
}

func sessionReader(ctx context.Context, connection *websocket.Conn, commands chan<- sessionCommand, result chan<- error) {
	for {
		command, err := readSessionCommand(ctx, connection)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || errors.Is(err, context.Canceled) {
				err = errPeerClosed
			}
			select {
			case result <- err:
			default:
			}
			return
		}
		select {
		case commands <- command:
		case <-ctx.Done():
			return
		default:
			select {
			case result <- errOutboundOverflow:
			default:
			}
			return
		}
	}
}

func sessionWriter(ctx context.Context, connection *websocket.Conn, frames <-chan outboundFrame, result chan<- error) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-frames:
			payload, err := json.Marshal(frame.value)
			if err == nil && len(payload) <= maximumFrameBytes {
				bounded, cancel := context.WithTimeout(ctx, writeTimeout)
				err = connection.Write(bounded, websocket.MessageText, payload)
				cancel()
			} else if err == nil {
				err = errors.New("realtime frame exceeds maximum size")
			}
			if err != nil {
				if frame.written != nil {
					close(frame.written)
				}
				select {
				case result <- err:
				default:
				}
				return
			}
			if frame.closeCode != 0 {
				_ = connection.Close(frame.closeCode, frame.closeReason)
				if frame.written != nil {
					close(frame.written)
				}
				return
			}
			if frame.written != nil {
				close(frame.written)
			}
		case <-ticker.C:
			bounded, cancel := context.WithTimeout(ctx, writeTimeout)
			err := connection.Ping(bounded)
			cancel()
			if err != nil {
				select {
				case result <- err:
				default:
				}
				return
			}
		}
	}
}

func readInitialResume(ctx context.Context, connection *websocket.Conn) (generated.SessionResumeEnvelope, error) {
	readContext, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	payload, err := readTextFrame(readContext, connection)
	if err != nil {
		return generated.SessionResumeEnvelope{}, err
	}
	resume, err := decodeClosed[generated.SessionResumeEnvelope](payload)
	if err != nil || validateSessionResume(resume) != nil {
		return generated.SessionResumeEnvelope{}, errors.New("session resume is invalid")
	}
	return resume, nil
}

func validateSessionResume(resume generated.SessionResumeEnvelope) error {
	if resume.Type != "SESSION_RESUME" || !safeRef.MatchString(resume.RequestRef) ||
		resume.PlatformAfterSequence < 0 || len(resume.Runs) > maximumRunSubscriptions {
		return errors.New("session resume is invalid")
	}
	seen := make(map[string]struct{}, len(resume.Runs))
	for _, run := range resume.Runs {
		if !safeRef.MatchString(run.RunRef) || run.AfterSequence < 0 {
			return errors.New("session run cursor is invalid")
		}
		if _, duplicated := seen[run.RunRef]; duplicated {
			return errors.New("session run cursor is duplicated")
		}
		seen[run.RunRef] = struct{}{}
	}
	return nil
}

func readSessionCommand(ctx context.Context, connection *websocket.Conn) (sessionCommand, error) {
	payload, err := readTextFrame(ctx, connection)
	if err != nil {
		return nil, err
	}
	return decodeSessionCommand(payload)
}

func decodeSessionCommand(payload []byte) (sessionCommand, error) {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &header) != nil {
		return nil, errors.New("session command is invalid")
	}
	switch header.Type {
	case "SUBSCRIBE_RUN":
		value, decodeErr := decodeClosed[generated.SubscribeRunEnvelope](payload)
		if decodeErr != nil || !safeRef.MatchString(value.RequestRef) || !safeRef.MatchString(value.RunRef) || value.AfterSequence < 0 {
			return nil, errors.New("subscribe run command is invalid")
		}
		return subscribeRunCommand{value}, nil
	case "UNSUBSCRIBE_RUN":
		value, decodeErr := decodeClosed[generated.UnsubscribeRunEnvelope](payload)
		if decodeErr != nil || !safeRef.MatchString(value.RequestRef) || !safeRef.MatchString(value.RunRef) {
			return nil, errors.New("unsubscribe run command is invalid")
		}
		return unsubscribeRunCommand{value}, nil
	default:
		return nil, errors.New("unknown session command")
	}
}

func readTextFrame(ctx context.Context, connection *websocket.Conn) ([]byte, error) {
	typ, payload, err := connection.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText || len(payload) == 0 || len(payload) > maximumFrameBytes {
		return nil, errors.New("realtime frame is invalid")
	}
	return payload, nil
}

func decodeClosed[T any](payload []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, errors.New("multiple JSON values are forbidden")
	}
	return value, nil
}

func projectRunGraph(value *controlplanev1.RunGraph, localize func(string) string) (generated.RunGraph, error) {
	return projectProto[generated.RunGraph](value, localize)
}

func projectRunEvent(value *controlplanev1.RunEvent, localize func(string) string) (generated.RunEvent, error) {
	return projectProto[generated.RunEvent](value, localize)
}

func projectProto[T any](value proto.Message, localize func(string) string) (T, error) {
	var zero T
	projection, err := httptransport.ProtoMap(value)
	if err != nil {
		return zero, err
	}
	httptransport.LocalizeSafeErrors(projection, localize)
	payload, err := json.Marshal(projection)
	if err != nil {
		return zero, err
	}
	result, err := decodeClosed[T](payload)
	if err != nil {
		return zero, fmt.Errorf("project realtime model: %w", err)
	}
	return result, nil
}

func validRunSignal(payload []byte, rootRef string) bool {
	if len(payload) == 0 || len(payload) > maximumFrameBytes {
		return false
	}
	event, err := decodeClosed[busEnvelope](payload)
	return err == nil && event.RootRunRef == rootRef && event.Sequence > 0
}

func readCatchUp(ctx context.Context, client queryClient, rootRef string, after int64, consume func(*controlplanev1.RunEvent) error) (int64, error) {
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

func streamLocalizer(writer http.ResponseWriter, request *http.Request) func(string) string {
	localize := func(messageID string) string { return messageID }
	if localized, ok := writer.(interface{ Localize(string) string }); ok {
		localize = localized.Localize
	}
	locale := request.URL.Query().Get("locale")
	if locale != "ru" && locale != "en" {
		return localize
	}
	if localized, ok := writer.(interface{ LocalizeFor(string, string) string }); ok {
		return func(messageID string) string { return localized.LocalizeFor(locale, messageID) }
	}
	return localize
}

func requestedProtocols(request *http.Request, baseProtocol string) (protocolSelection, bool) {
	var result protocolSelection
	foundBase := false
	for _, header := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(header, ",") {
			value := strings.TrimSpace(raw)
			if value == baseProtocol {
				if foundBase {
					return protocolSelection{}, false
				}
				foundBase = true
				continue
			}
			if strings.HasPrefix(value, "csrf.") && len(value) > 5 {
				if result.csrf != "" {
					return protocolSelection{}, false
				}
				result.csrf = strings.TrimPrefix(value, "csrf.")
				continue
			}
			return protocolSelection{}, false
		}
	}
	return result, foundBase && result.csrf != ""
}

func problemSpec(code string) streamProblemSpec {
	if spec, ok := streamProblemSpecs[code]; ok {
		return spec
	}
	return streamProblemSpecs["INTERNAL"]
}

func writeInitialProblem(ctx context.Context, connection *websocket.Conn, localize func(string) string, code string) {
	spec := problemSpec(code)
	bounded, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	payload, _ := json.Marshal(generated.SessionProblemEnvelope{
		Type: "SESSION_PROBLEM", RequestRef: "request_invalid", Status: spec.status,
		Code: generated.ProblemCode(code), Title: localize(code), Retryable: spec.retryable,
	})
	_ = connection.Write(bounded, websocket.MessageText, payload)
	_ = connection.Close(websocket.StatusPolicyViolation, code)
}

func (server *Server) Check(ctx context.Context) error {
	if server == nil || server.nats == nil || !server.nats.IsConnected() {
		return errors.New("realtime NATS consumer is unavailable")
	}
	return server.nats.FlushWithContext(ctx)
}
