package websockettransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// assistantPageHintRecorder counts fresh assistant/conversation/bootstrap reads, applies a configured pre-read callback and can replace the synthetic owner cursor without caching response payloads.
// assistantPageHintRecorder считает свежие чтения помощника, диалогов и bootstrap, выполняет настроенный callback до чтения и может заменять тестовый owner cursor без кэширования ответов.
type assistantPageHintRecorder struct {
	assistantSnapshotSizeRecorder
	assistantReads, conversationReads, bootstrapReads int
	before                                            func(context.Context) error
	ownerCursor                                       string
}

// Invoke counts the response-specific owner read, applies an optional failure/cancellation callback, delegates fixture responses and optionally substitutes only a successful conversation cursor.
// Invoke считает чтение по типу ответа, применяет необязательный callback отказа или отмены, возвращает ответы оснастки и может заменить только курсор успешного списка диалогов.
func (c *assistantPageHintRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	switch response.(type) {
	case *cp.GetSystemAssistantResponse:
		c.assistantReads++
	case *cp.ListAssistantConversationsResponse:
		c.conversationReads++
	case *cp.GetBootstrapStateResponse:
		c.bootstrapReads++
	}
	if c.before != nil {
		if err := c.before(ctx); err != nil {
			return err
		}
	}
	err := c.assistantSnapshotSizeRecorder.Invoke(ctx, method, request, response, options...)
	if out, ok := response.(*cp.ListAssistantConversationsResponse); ok && err == nil && c.ownerCursor != "" {
		out.Page.NextPageToken = c.ownerCursor
	}
	return err
}

// assistantPageHintFixture creates forty-two large synthetic conversations and a project multiplexer whose clients all use the counting recorder; no real owner service or socket is opened.
// assistantPageHintFixture создаёт сорок два больших тестовых диалога и проектный multiplexer с клиентами счётчика; настоящий сервис владельца и сокет не открываются.
func assistantPageHintFixture(t *testing.T) (*sessionMultiplexer, *assistantPageHintRecorder) {
	t.Helper()
	c := &assistantPageHintRecorder{}
	c.conversations = assistantSizeConversations(42, strings.Repeat("Я", 16000))
	m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	return m, c
}

// warmAssistantPageHint sends one synthetic bootstrap, verifies the learned whole-page size and single frame/request, then drains that frame without sharing expected assertions for later wake outcomes.
// warmAssistantPageHint отправляет один тестовый bootstrap, проверяет выученный размер целой страницы и единственные кадр и запрос, затем извлекает кадр, не подменяя проверки дальнейших обновлений.
func warmAssistantPageHint(t *testing.T, m *sessionMultiplexer, c *assistantPageHintRecorder) {
	t.Helper()
	if _, err := m.sendPlatformBootstrap(); err != nil || m.assistantPageHint.pageSize != 25 {
		t.Fatal("initial full bootstrap did not learn the last whole fitting page size")
	}
	if len(c.requests) != 1 || c.requests[0].Page.PageSize != 25 || len(c.frames) != 1 {
		t.Fatal("initial hint changed owner page adaptation or emitted a partial snapshot")
	}
	<-c.frames
}

// TestAssistantPageHintBootstrapAndWakesStillReadFreshOwnerData mutates the fake owner's version/content/cursor and proves each new wake reads every owner dependency afresh while duplicate signals do not repeat reads.
// TestAssistantPageHintBootstrapAndWakesStillReadFreshOwnerData меняет тестовые версию, содержимое и курсор владельца и доказывает свежее чтение всех зависимостей при новом событии без повторов чтения для дубликата.
func TestAssistantPageHintBootstrapAndWakesStillReadFreshOwnerData(t *testing.T) {
	m, c := assistantPageHintFixture(t)
	warmAssistantPageHint(t, m, c)
	// The new version and content become visible only through a fresh owner response.
	// Новая версия и текст появляются только в новом owner response.
	c.conversations[0].Version = 9
	c.conversations[0].Turns[0].Content = "fresh owner content"
	c.ownerCursor = "fresh-owner-cursor"
	signal := platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT", EventName: "SYSTEM_ASSISTANT_CHANGED", ProjectRef: m.projectRef}
	if !m.applyPlatformSignal(signal) {
		t.Fatal("fresh owner wake failed")
	}
	frame := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
	if len(c.requests) != 2 || c.requests[1].Page.PageSize != 25 || c.requests[1].Page.PageToken != "" ||
		c.assistantReads != 2 || c.conversationReads != 2 || c.bootstrapReads != 2 ||
		frame.Cursor != 124 || *frame.Snapshot.Conversations.Page.NextPageToken != c.ownerCursor ||
		frame.Snapshot.Conversations.Conversations[0].Version != 9 || frame.Snapshot.Conversations.Conversations[0].Turns[0].Content != "fresh owner content" {
		t.Fatal("hint reused owner payload, skipped RPCs or changed authoritative cursor/pins")
	}
	if !m.applyPlatformSignal(signal) || len(c.frames) != 0 || len(c.requests) != 2 {
		t.Fatal("hint changed duplicate event handling")
	}
	if !m.applyPlatformSignal(platformSignal{Sequence: 125, Kind: signal.Kind, EventName: signal.EventName, ProjectRef: m.projectRef}) || len(c.requests) != 3 || c.assistantReads != 3 || c.bootstrapReads != 3 {
		t.Fatal("repeated wake did not freshly read every owner dependency")
	}
}

// TestAssistantPageHintGrowthStillAdaptsAndFailureKeepsClosedStream grows complete conversations beyond the learned page bound, requiring fresh size reduction or the existing closed stream failure without cursor advancement or truncated payload.
// TestAssistantPageHintGrowthStillAdaptsAndFailureKeepsClosedStream увеличивает целые диалоги сверх известной границы страницы, требуя нового уменьшения либо прежнего закрытого отказа потока без продвижения курсора и усечения данных.
func TestAssistantPageHintGrowthStillAdaptsAndFailureKeepsClosedStream(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			m, c := assistantPageHintFixture(t)
			warmAssistantPageHint(t, m, c)
			for _, conversation := range c.conversations {
				conversation.Turns = append(conversation.Turns, proto.Clone(conversation.Turns[0]).(*cp.AssistantTurn))
			}
			if fail {
				c.before = func(ctx context.Context) error {
					if c.conversationReads == 3 {
						return status.Error(codes.DeadlineExceeded, "PRIVATE_SENTINEL")
					}
					return nil
				}
			}
			rows := snapshotDiagnosticLogs(t, func() {
				if !m.applyPlatformSignal(platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT", EventName: "SYSTEM_ASSISTANT_CHANGED", ProjectRef: m.projectRef}) {
					t.Fatal("hint lost the existing successful or closed stream result")
				}
			})
			wantRequests := 3
			if fail {
				wantRequests = 2
			}
			if len(c.requests) != wantRequests || c.requests[1].Page.PageSize != 25 || c.conversationReads != 3 {
				t.Fatal("growing hinted page did not start from the last fitting owner page")
			}
			frame := (<-c.frames).value
			if fail {
				problem, ok := frame.(generated.StreamProblemEnvelope)
				if !ok || problem.Code != "PLATFORM_UNAVAILABLE" || m.platformCursor != 123 || m.platformAvailable || m.assistantPageHint.pageSize != 25 || len(rows) != 1 || rows[0][platformSnapshotAttemptKey] != float64(2) || rows[0][platformSnapshotAssistantPageKey] != float64(12) {
					t.Fatal("hint masked reduced-page failure, changed cursor or logged the wrong attempt")
				}
			} else {
				delta, ok := frame.(generated.PlatformSnapshotEnvelope)
				if !ok || len(c.requests) != 3 || c.requests[2].Page.PageSize != 12 || m.assistantPageHint.pageSize != 12 || m.platformCursor != 124 || len(rows) != 0 || *delta.Snapshot.Conversations.Page.NextPageToken != "owner-cursor-12" {
					t.Fatal("growing page was truncated or lost its fresh owner cursor")
				}
			}
		})
	}
}

// TestAssistantPageHintScopeSocketAndCloseReset rejects cross-project/organization/socket reuse, resets hints on close and disallows sizes outside the fixed ladder.
// TestAssistantPageHintScopeSocketAndCloseReset запрещает перенос подсказки между проектами, организациями и сокетами, очищает её при закрытии и отклоняет размер вне фиксированной лестницы.
func TestAssistantPageHintScopeSocketAndCloseReset(t *testing.T) {
	for _, change := range []string{"project", "organization", "close", "invalid-size", "new-socket"} {
		t.Run(change, func(t *testing.T) {
			m, c := assistantPageHintFixture(t)
			warmAssistantPageHint(t, m, c)
			m.assistantPageHint.pageSize = 12
			switch change {
			case "project":
				m.projectRef = "prj_otherfixture"
			case "organization":
				m.organizationRef = "org_otherfixture"
			case "close":
				m.closeSubscriptions()
				if m.assistantPageHint != (assistantSnapshotPageHint{}) {
					t.Fatal("socket close retained its private hint")
				}
			case "invalid-size":
				m.assistantPageHint.pageSize = 24
			case "new-socket":
				other, _ := assistantPageHintFixture(t)
				m = other
			}
			if m.assistantSnapshotInitialPageSize() != 25 || m.assistantPageHint != (assistantSnapshotPageHint{}) {
				t.Fatal("page hint crossed scope/socket or accepted an unknown size")
			}
		})
	}
}

// TestAssistantPageHintDenialCancelAndOtherKindsDoNotReuseData proves a size hint never bypasses fresh owner denial or cancellation, emits no partial data and does not change unrelated resource-kind reads.
// TestAssistantPageHintDenialCancelAndOtherKindsDoNotReuseData доказывает, что подсказка размера не обходит свежий отказ владельца или отмену, не выдаёт частичные данные и не меняет чтения посторонних видов ресурсов.
func TestAssistantPageHintDenialCancelAndOtherKindsDoNotReuseData(t *testing.T) {
	for _, condition := range []string{"permission", "cancel", "other-kind"} {
		t.Run(condition, func(t *testing.T) {
			m, c := assistantPageHintFixture(t)
			warmAssistantPageHint(t, m, c)
			kind := generated.PlatformResourceKindSystemAssistant
			wantCode := codes.PermissionDenied
			switch condition {
			case "permission":
				c.failure = status.Error(codes.PermissionDenied, "PRIVATE_SENTINEL")
			case "cancel":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				m.ctx = ctx
				c.before = func(ctx context.Context) error { return status.FromContextError(ctx.Err()).Err() }
				wantCode = codes.Canceled
			case "other-kind":
				kind = generated.PlatformResourceKindRun
			}
			rows := snapshotDiagnosticLogs(t, func() {
				_, err := m.boundedPlatformSnapshot(generated.PlatformSnapshotEnvelope{Kind: kind})
				if status.Code(err) != wantCode {
					t.Fatal("hint changed the fresh authoritative denial or cancellation")
				}
			})
			if len(c.frames) != 0 || m.platformCursor != 123 || (condition != "cancel" && len(rows) != 0) {
				t.Fatal("hint fabricated access, published partial data or advanced a failed read cursor")
			}
			if condition == "permission" && (len(c.requests) != 2 || c.requests[1].Page.PageSize != 25 || c.assistantReads != 2 || c.bootstrapReads != 1) {
				t.Fatal("hint bypassed a newly denied owner RPC")
			}
			if condition == "cancel" && (m.assistantPageHint != (assistantSnapshotPageHint{}) || len(rows) != 1) {
				t.Fatal("closed socket context retained a page hint or lost its closed failure")
			}
			if condition == "other-kind" && (len(c.requests) != 1 || m.assistantPageHint.pageSize != 25) {
				t.Fatal("assistant hint changed another kind's page or reused its owner data")
			}
		})
	}
}

// TestAssistantPageHintStillChecksCurrentFullEnvelope fits the exact bootstrap byte bound, then adds delta metadata and requires a size error without frames or cursor advancement despite the learned page hint.
// TestAssistantPageHintStillChecksCurrentFullEnvelope заполняет точную байтовую границу bootstrap, затем добавляет delta metadata и требует ошибку размера без кадров и продвижения курсора несмотря на подсказку страницы.
func TestAssistantPageHintStillChecksCurrentFullEnvelope(t *testing.T) {
	c, m, envelope := assistantSizeBoundaryFixture(t)
	conversation := c.conversations[0]
	initial, err := m.boundedPlatformSnapshot(envelope)
	if err != nil {
		t.Fatal("whole-envelope boundary fixture failed")
	}
	encoded, _ := json.Marshal(initial)
	conversation.Turns[0].Content = strings.Repeat("x", maximumFrameBytes-len(encoded))
	if _, err := m.boundedPlatformSnapshot(envelope); err != nil {
		t.Fatal("exact full envelope was rejected")
	}
	event := generated.PlatformEventName("SYSTEM_ASSISTANT_CHANGED")
	envelope.Mode, envelope.EventName = generated.PlatformSnapshotModeDelta, &event
	envelope.Cursor++
	if _, err := m.boundedPlatformSnapshot(envelope); !errors.Is(err, errPlatformSnapshotSize) || len(c.frames) != 0 || m.platformCursor != 123 {
		t.Fatal("page hint skipped current frame overhead or emitted an oversized snapshot")
	}
}
