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

// assistantSnapshotSizeRecorder serves synthetic assistant/bootstrap/conversation responses, clones requested pagination and applies a configured list failure; unrelated RPCs are denied rather than contacting a server.
// assistantSnapshotSizeRecorder выдаёт тестовые ответы помощника, bootstrap и диалогов, копирует запрошенную пагинацию и подставляет отказ списка; посторонние RPC запрещаются без обращения к серверу.
type assistantSnapshotSizeRecorder struct {
	grpc.ClientConnInterface
	conversations []*cp.AssistantConversation
	requests      []*cp.ListAssistantConversationsRequest
	failure       error
	frames        chan outboundFrame
}

// Invoke selects fixture output by response type, records an independent conversation request copy and returns a whole bounded owner page/cursor or the configured error, denying unsupported reads.
// Invoke выбирает тестовый ответ по типу, сохраняет независимую копию запроса диалогов и возвращает целую ограниченную страницу с курсором либо настроенную ошибку, запрещая неподдерживаемые чтения.
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

// assistantSizeMultiplexer wires fake generated clients and buffered frame/overflow channels with fixed request/cursor values and the test context, without opening a WebSocket connection.
// assistantSizeMultiplexer связывает тестовые generated clients и буферы кадров и переполнения с фиксированными request/cursor и контекстом теста без открытия WebSocket.
func assistantSizeMultiplexer(t *testing.T, c *assistantSnapshotSizeRecorder, project string) *sessionMultiplexer {
	t.Helper()
	server := &Server{query: cp.NewPlatformQueryServiceClient(c), assistant: cp.NewSystemAssistantServiceClient(c), access: cp.NewAccessServiceClient(c), roleImages: cp.NewRoleImageServiceClient(c)}
	c.frames = make(chan outboundFrame, 32)
	return &sessionMultiplexer{server: server, ctx: t.Context(), projectRef: project, platformCursor: 123,
		platformRequestRef: "request_fixture01", localize: func(code string) string { return code },
		outbound: c.frames, overflow: make(chan struct{}, 1)}
}

// assistantSizeConversations builds distinct synthetic active project conversations with pinned assistant/profile/version and one complete USER turn, preserving the supplied content without truncation.
// assistantSizeConversations создаёт различные тестовые активные проектные диалоги с точными assistant/profile/version и одним полным USER-ходом, сохраняя переданный текст без усечения.
func assistantSizeConversations(count int, content string) []*cp.AssistantConversation {
	values := make([]*cp.AssistantConversation, count)
	for i := range values {
		values[i] = &cp.AssistantConversation{Ref: fmt.Sprintf("cnv_fixture_%02d", i), Version: 7, ProjectRef: "prj_fixture01",
			State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE, AssistantScope: cp.AssistantScope_ASSISTANT_SCOPE_PROJECT,
			AssistantRef: "agt_fixture01", AssistantProfileRef: "aprf_fixture01",
			Turns: []*cp.AssistantTurn{{Ref: "trn_fixture01", Sequence: 1, Role: "USER", Content: content, State: "COMPLETED", AttachmentSetRef: "att_fixture01", Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY}}}}
	}
	return values
}

// TestAssistantSnapshotFitsWholeOwnerPage checks project and organization bootstrap retain an entire fitting owner page, exact pagination/content/pins and byte-identical replay without inventing scope or cursor.
// TestAssistantSnapshotFitsWholeOwnerPage проверяет, что bootstrap проекта и организации сохраняет целую помещающуюся owner-страницу, точные пагинацию, текст, привязки и побайтовый повтор без выдуманной области или курсора.
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
			if len(c.requests) != 1 || c.requests[0].Page.PageSize != 25 ||
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

// TestAssistantSnapshotRejectsOversizedWholeConversation requires an oversized single conversation to exhaust only the bounded page ladder and fail before queueing, never truncating turns.
// TestAssistantSnapshotRejectsOversizedWholeConversation требует для одного чрезмерного диалога исчерпать только ограниченную лестницу страниц и отказать до очереди без усечения ходов.
func TestAssistantSnapshotRejectsOversizedWholeConversation(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{conversations: assistantSizeConversations(1, strings.Repeat("😀", 32768))}
	for range 8 {
		c.conversations[0].Turns = append(c.conversations[0].Turns, proto.Clone(c.conversations[0].Turns[0]).(*cp.AssistantTurn))
	}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	if _, err := m.sendPlatformBootstrap(); err == nil || len(m.outbound) != 0 {
		t.Fatal("oversized singleton was silently queued or truncated")
	}
	if len(c.requests) != 5 || c.requests[4].Page.PageSize != 1 {
		t.Fatal("oversized singleton did not exhaust only the bounded page-size ladder")
	}
}

// TestAssistantSnapshotDoesNotMaskOwnerDenial preserves fake owner permission denial without retrying list reads, publishing frames or manufacturing a resource kind.
// TestAssistantSnapshotDoesNotMaskOwnerDenial сохраняет тестовый отказ владельца без повторов списка, публикации кадров и выдумывания вида ресурса.
func TestAssistantSnapshotDoesNotMaskOwnerDenial(t *testing.T) {
	c := &assistantSnapshotSizeRecorder{failure: status.Error(codes.PermissionDenied, "private synthetic denied input")}
	m := assistantSizeMultiplexer(t, c, "prj_fixture01")
	kinds, err := m.sendPlatformBootstrap()
	if err != nil || len(kinds) != 0 || len(m.outbound) != 0 || len(c.requests) != 1 {
		t.Fatal("owner denial was retried, bypassed or projected")
	}
}

// TestAssistantSnapshotDeltaPreservesSequenceAndWholeOwnerPage validates delta scope/event/cursor and whole-page content within the frame bound, then ensures duplicate signals do not reread or publish.
// TestAssistantSnapshotDeltaPreservesSequenceAndWholeOwnerPage проверяет scope/event/cursor delta и целую страницу в границе кадра, затем запрещает повтор чтения или публикации для дубликата события.
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
	if !m.applyPlatformSignal(signal) || len(c.frames) != 0 || len(c.requests) != 1 {
		t.Fatal("duplicate delta replay repeated owner reads or advanced state")
	}
}

// TestAssistantSnapshotSingletonFailureIsTypedAndPrivate captures logs and queued terminal frames for an oversized singleton, requiring redacted diagnostics and existing typed session/stream failures without advancing the cursor. No live socket is used.
// TestAssistantSnapshotSingletonFailureIsTypedAndPrivate перехватывает логи и терминальные кадры чрезмерного одиночного диалога, требуя скрытых подробностей и прежних типизированных отказов сессии и потока без продвижения курсора. Настоящий сокет не используется.
func TestAssistantSnapshotSingletonFailureIsTypedAndPrivate(t *testing.T) {
	// privateMarker is synthetic oversized content that must never appear in diagnostic logs; it is not a real conversation or credential.
	// privateMarker — тестовое чрезмерное содержимое, запрещённое в диагностических логах, а не настоящий диалог или credential.
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

// TestAssistantSnapshotByteBoundaryIncludesEnvelope independently measures complete serialized metadata plus conversations, accepts exactly maximumFrameBytes and rejects one additional byte without truncation.
// TestAssistantSnapshotByteBoundaryIncludesEnvelope независимо измеряет полные сериализованные metadata с диалогами, принимает ровно maximumFrameBytes и отвергает дополнительный байт без усечения.
func TestAssistantSnapshotByteBoundaryIncludesEnvelope(t *testing.T) {
	c, m, envelope := assistantSizeBoundaryFixture(t)
	conversation := c.conversations[0]
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
