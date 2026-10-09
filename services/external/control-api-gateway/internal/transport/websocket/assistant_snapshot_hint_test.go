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

type assistantPageHintRecorder struct {
	assistantSnapshotSizeRecorder
	assistantReads, conversationReads, bootstrapReads int
	before                                            func(context.Context) error
	ownerCursor                                       string
}

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

func assistantPageHintFixture(t *testing.T) (*sessionMultiplexer, *assistantPageHintRecorder) {
	t.Helper()
	c := &assistantPageHintRecorder{}
	c.conversations = assistantSizeConversations(42, strings.Repeat("Я", 16000))
	m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	return m, c
}

func warmAssistantPageHint(t *testing.T, m *sessionMultiplexer, c *assistantPageHintRecorder) {
	t.Helper()
	if _, err := m.sendPlatformBootstrap(); err != nil || m.assistantPageHint.pageSize != 25 {
		t.Fatal("initial full bootstrap did not learn the last whole fitting page size")
	}
	if len(c.requests) != 2 || c.requests[0].Page.PageSize != 50 || c.requests[1].Page.PageSize != 25 || len(c.frames) != 1 {
		t.Fatal("initial hint changed owner page adaptation or emitted a partial snapshot")
	}
	<-c.frames
}

func TestAssistantPageHintBootstrapAndWakesStillReadFreshOwnerData(t *testing.T) {
	m, c := assistantPageHintFixture(t)
	warmAssistantPageHint(t, m, c)
	// Новая версия и текст появляются только в новом owner response.
	c.conversations[0].Version = 9
	c.conversations[0].Turns[0].Content = "fresh owner content"
	c.ownerCursor = "fresh-owner-cursor"
	signal := platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT", EventName: "SYSTEM_ASSISTANT_CHANGED", ProjectRef: m.projectRef}
	if !m.applyPlatformSignal(signal) {
		t.Fatal("fresh owner wake failed")
	}
	frame := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
	if len(c.requests) != 3 || c.requests[2].Page.PageSize != 25 || c.requests[2].Page.PageToken != "" ||
		c.assistantReads != 3 || c.conversationReads != 3 || c.bootstrapReads != 3 ||
		frame.Cursor != 124 || *frame.Snapshot.Conversations.Page.NextPageToken != c.ownerCursor ||
		frame.Snapshot.Conversations.Conversations[0].Version != 9 || frame.Snapshot.Conversations.Conversations[0].Turns[0].Content != "fresh owner content" {
		t.Fatal("hint reused owner payload, skipped RPCs or changed authoritative cursor/pins")
	}
	if !m.applyPlatformSignal(signal) || len(c.frames) != 0 || len(c.requests) != 3 {
		t.Fatal("hint changed duplicate event handling")
	}
	if !m.applyPlatformSignal(platformSignal{Sequence: 125, Kind: signal.Kind, EventName: signal.EventName, ProjectRef: m.projectRef}) || len(c.requests) != 4 || c.assistantReads != 4 || c.bootstrapReads != 4 {
		t.Fatal("repeated wake did not freshly read every owner dependency")
	}
}

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
					if c.conversationReads == 4 {
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
			wantRequests := 4
			if fail {
				wantRequests = 3
			}
			if len(c.requests) != wantRequests || c.requests[2].Page.PageSize != 25 || c.conversationReads != 4 {
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
				if !ok || len(c.requests) != 4 || c.requests[3].Page.PageSize != 12 || m.assistantPageHint.pageSize != 12 || m.platformCursor != 124 || len(rows) != 0 || *delta.Snapshot.Conversations.Page.NextPageToken != "owner-cursor-12" {
					t.Fatal("growing page was truncated or lost its fresh owner cursor")
				}
			}
		})
	}
}

func TestAssistantPageHintScopeSocketAndCloseReset(t *testing.T) {
	for _, change := range []string{"project", "organization", "close", "invalid-size", "new-socket"} {
		t.Run(change, func(t *testing.T) {
			m, c := assistantPageHintFixture(t)
			warmAssistantPageHint(t, m, c)
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
			if m.assistantSnapshotInitialPageSize() != 50 || m.assistantPageHint != (assistantSnapshotPageHint{}) {
				t.Fatal("page hint crossed scope/socket or accepted an unknown size")
			}
		})
	}
}

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
			if condition == "permission" && (len(c.requests) != 3 || c.requests[2].Page.PageSize != 25 || c.assistantReads != 3 || c.bootstrapReads != 2) {
				t.Fatal("hint bypassed a newly denied owner RPC")
			}
			if condition == "cancel" && (m.assistantPageHint != (assistantSnapshotPageHint{}) || len(rows) != 1) {
				t.Fatal("closed socket context retained a page hint or lost its closed failure")
			}
			if condition == "other-kind" && (len(c.requests) != 2 || m.assistantPageHint.pageSize != 25) {
				t.Fatal("assistant hint changed another kind's page or reused its owner data")
			}
		})
	}
}

func TestAssistantPageHintStillChecksCurrentFullEnvelope(t *testing.T) {
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
