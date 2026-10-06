package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type assistantSnapshotSizeRecorder struct {
	grpc.ClientConnInterface
	conversations []*cp.AssistantConversation
	requests      []*cp.ListAssistantConversationsRequest
	failure       error
	frames        chan outboundFrame
}

func (c *assistantSnapshotSizeRecorder) Invoke(_ context.Context, _ string, request, response any, _ ...grpc.CallOption) error {
	switch out := response.(type) {
	case *cp.GetSystemAssistantResponse:
		out.Assistant = &cp.SystemAssistant{Ref: "agt_fixture01", Version: 1, RuntimeState: cp.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY}
	case *cp.GetBootstrapStateResponse:
		out.State = &cp.BootstrapState{Initialized: true}
	case *cp.ListAssistantConversationsResponse:
		input := request.(*cp.ListAssistantConversationsRequest)
		c.requests = append(c.requests, proto.Clone(input).(*cp.ListAssistantConversationsRequest))
		if c.failure != nil {
			return c.failure
		}
		n := min(len(c.conversations), int(input.Page.PageSize))
		out.Conversations = c.conversations[:n]
		out.Page = &cp.PageInfo{}
		if n < len(c.conversations) {
			out.Page.NextPageToken = fmt.Sprintf("owner-cursor-%d", n)
		}
	default:
		return status.Error(codes.PermissionDenied, "synthetic unrelated catalog denied")
	}
	return nil
}

func assistantSizeMultiplexer(t *testing.T, c *assistantSnapshotSizeRecorder, project string) *sessionMultiplexer {
	t.Helper()
	server := &Server{query: cp.NewPlatformQueryServiceClient(c), assistant: cp.NewSystemAssistantServiceClient(c), access: cp.NewAccessServiceClient(c), roleImages: cp.NewRoleImageServiceClient(c)}
	c.frames = make(chan outboundFrame, 32)
	return &sessionMultiplexer{server: server, ctx: t.Context(), projectRef: project, platformCursor: 123,
		platformRequestRef: "request_fixture01", localize: func(code string) string { return code },
		outbound: c.frames, overflow: make(chan struct{}, 1)}
}

func assistantSizeConversations(count int, content string) []*cp.AssistantConversation {
	values := make([]*cp.AssistantConversation, count)
	for i := range values {
		values[i] = &cp.AssistantConversation{Ref: fmt.Sprintf("cnv_fixture_%02d", i), Version: 7, ProjectRef: "prj_fixture01",
			State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE, AssistantScope: cp.AssistantScope_ASSISTANT_SCOPE_PROJECT,
			AssistantRef: "agt_fixture01", AssistantProfileRef: "aprf_fixture01",
			Turns: []*cp.AssistantTurn{{Ref: "trn_fixture01", Sequence: 1, Role: "USER", Content: content, State: "COMPLETED", AttachmentSetRef: "att_fixture01"}}}
	}
	return values
}

func TestAssistantSnapshotFitsWholeOwnerPage(t *testing.T) {
	for _, project := range []string{"", "prj_fixture01"} {
		t.Run(project, func(t *testing.T) {
			c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(42, strings.Repeat("Я", 16000))}
			m := assistantSizeMultiplexer(t, c, project)
			kinds, err := m.sendPlatformBootstrap()
			if err != nil || !reflect.DeepEqual(kinds, []generated.PlatformResourceKind{"SYSTEM_ASSISTANT"}) {
				t.Fatal("whole assistant page bootstrap failed")
			}
			envelope := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
			encoded, err := json.Marshal(envelope)
			if err != nil || len(encoded) > maximumFrameBytes {
				t.Fatal("assistant bootstrap frame exceeded the unchanged byte bound")
			}
			page := envelope.Snapshot.Conversations
			if len(c.requests) != 2 || c.requests[0].Page.PageSize != 50 || c.requests[1].Page.PageSize != 25 ||
				len(page.Conversations) != 25 || *page.Page.NextPageToken != "owner-cursor-25" || envelope.Cursor != 123 {
				t.Fatal("adaptive page lost owner cardinality, cursor or platform sequence")
			}
			for _, request := range c.requests {
				if request.ProjectRef != project || request.Page.PageToken != "" || request.AssistantScope != cp.AssistantScope_ASSISTANT_SCOPE_UNSPECIFIED {
					t.Fatal("page adaptation changed owner scope or invented a pagination token")
				}
			}
			for i, conversation := range page.Conversations {
				if conversation.Ref != c.conversations[i].Ref || conversation.Version != 7 || len(conversation.Turns) != 1 ||
					conversation.Turns[0].Content != c.conversations[i].Turns[0].Content || *conversation.Turns[0].AttachmentSetRef != "att_fixture01" {
					t.Fatal("whole conversation content or pins were truncated")
				}
			}
			if _, err := m.sendPlatformBootstrap(); err != nil {
				t.Fatal("exact bootstrap replay failed")
			}
			replayed, _ := json.Marshal((<-c.frames).value)
			if string(replayed) != string(encoded) {
				t.Fatal("rejoin changed the exact owner page")
			}
		})
	}
}

func TestAssistantSnapshotRejectsOversizedWholeConversation(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(1, strings.Repeat("😀", 32768))}
	for range 8 {
		c.conversations[0].Turns = append(c.conversations[0].Turns, proto.Clone(c.conversations[0].Turns[0]).(*cp.AssistantTurn))
	}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	if _, err := m.sendPlatformBootstrap(); err == nil || len(m.outbound) != 0 {
		t.Fatal("oversized singleton was silently queued or truncated")
	}
	if len(c.requests) != 6 || c.requests[5].Page.PageSize != 1 {
		t.Fatal("oversized singleton did not exhaust only the bounded page-size ladder")
	}
}

func TestAssistantSnapshotDoesNotMaskOwnerDenial(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{failure: status.Error(codes.PermissionDenied, "private synthetic denied input")}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	kinds, err := m.sendPlatformBootstrap()
	if err != nil || len(kinds) != 0 || len(m.outbound) != 0 || len(c.requests) != 1 {
		t.Fatal("owner denial was retried, bypassed or projected")
	}
}

func TestAssistantSnapshotDeltaPreservesSequenceAndWholeOwnerPage(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(42, strings.Repeat("Я", 16000))}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	signal := platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT", EventName: "SYSTEM_ASSISTANT_CHANGED", ProjectRef: "prj_fixture01"}
	if !m.applyPlatformSignal(signal) || m.platformCursor != 124 {
		t.Fatal("bounded delta did not advance its exact platform cursor")
	}
	envelope := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > maximumFrameBytes || envelope.Cursor != 124 || envelope.Mode != generated.PlatformSnapshotModeDelta ||
		envelope.EventName == nil || string(*envelope.EventName) != signal.EventName || *envelope.ProjectRef != signal.ProjectRef ||
		len(envelope.Snapshot.Conversations.Conversations) != 25 || *envelope.Snapshot.Conversations.Page.NextPageToken != "owner-cursor-25" {
		t.Fatal("delta altered envelope pins, owner page or byte bound")
	}
	if !m.applyPlatformSignal(signal) || len(c.frames) != 0 || len(c.requests) != 2 {
		t.Fatal("duplicate delta replay repeated owner reads or advanced state")
	}
}

func TestAssistantSnapshotSingletonFailureIsTypedAndPrivate(t *testing.T) {
	const privateMarker = "PRIVATE_SNAPSHOT_SENTINEL"
	c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(1, strings.Repeat(privateMarker, maximumFrameBytes/len(privateMarker)+1))}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	if _, err := m.sendPlatformBootstrap(); !errors.Is(err, errPlatformSnapshotSize) || len(c.frames) != 0 {
		t.Fatal("oversized singleton did not fail before writer admission")
	}
	closed := make(chan outboundFrame, 1)
	go func() {
		frame := <-c.frames
		closed <- frame
		close(frame.written)
	}()
	m.terminate("PLATFORM_UNAVAILABLE", websocket.StatusTryAgainLater)
	frame := <-closed
	problem, ok := frame.value.(generated.SessionProblemEnvelope)
	if !ok || problem.Code != "PLATFORM_UNAVAILABLE" || !problem.Retryable || frame.closeCode != websocket.StatusTryAgainLater {
		t.Fatal("bootstrap overflow did not use the existing typed terminal stream path")
	}
	if !strings.Contains(logs.String(), platformSnapshotSizeFailure) || strings.Contains(logs.String(), privateMarker) ||
		strings.Contains(logs.String(), "prj_fixture01") || strings.Contains(logs.String(), "cnv_fixture") {
		t.Fatal("snapshot size diagnosis leaked body or owner identifiers")
	}
	if !m.applyPlatformSignal(platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT"}) || m.platformCursor != 123 || m.platformAvailable {
		t.Fatal("oversized delta advanced the authoritative platform cursor")
	}
	stream, ok := (<-c.frames).value.(generated.StreamProblemEnvelope)
	if !ok || stream.Code != "PLATFORM_UNAVAILABLE" || stream.Cursor != 123 || !stream.Retryable {
		t.Fatal("oversized delta did not return the existing typed stream failure")
	}
}

func TestAssistantSnapshotByteBoundaryIncludesEnvelope(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(1, "")}
	conversation := c.conversations[0]
	for i := range 31 {
		turn := proto.Clone(conversation.Turns[0]).(*cp.AssistantTurn)
		turn.Ref, turn.Sequence, turn.Content = fmt.Sprintf("trn_fixture_%02d", i), int64(i+2), strings.Repeat("x", 32768)
		conversation.Turns = append(conversation.Turns, turn)
	}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	envelope := generated.PlatformSnapshotEnvelope{Type: "PLATFORM_SNAPSHOT", RequestRef: m.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: m.platformCursor,
		Mode: generated.PlatformSnapshotModeBootstrap, Kind: generated.PlatformResourceKindSystemAssistant, ProjectRef: &m.projectRef}
	initial, err := m.boundedPlatformSnapshot(envelope)
	if err != nil {
		t.Fatal("whole boundary fixture is invalid")
	}
	encoded, _ := json.Marshal(initial)
	remaining := maximumFrameBytes - len(encoded)
	if remaining < 0 || remaining > 32768 {
		t.Fatal("boundary fixture does not fit individual USER input limits")
	}
	conversation.Turns[0].Content = strings.Repeat("x", remaining)
	exact, err := m.boundedPlatformSnapshot(envelope)
	if err != nil {
		t.Fatal("exact full-envelope byte bound was rejected")
	}
	encoded, _ = json.Marshal(exact)
	if len(encoded) != maximumFrameBytes {
		t.Fatal("byte boundary omitted envelope metadata")
	}
	conversation.Turns[0].Content += "x"
	if _, err := m.boundedPlatformSnapshot(envelope); !errors.Is(err, errPlatformSnapshotSize) {
		t.Fatal("full-envelope boundary plus one byte was not closed")
	}
}
