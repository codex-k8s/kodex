package websockettransport

import (
	"fmt"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/protobuf/proto"
)

// assistantSizeBoundaryFixture prepares a synthetic conversation with thirty-one full-sized added turns, its multiplexer and exact bootstrap envelope. It shares only input construction; callers independently measure, fill and assert their byte-boundary outcomes.
// assistantSizeBoundaryFixture подготавливает тестовый диалог с тридцать одним добавленным полноразмерным ходом, multiplexer и точный bootstrap-конверт. Общим является только создание входа; вызывающие независимо измеряют, заполняют и проверяют свои байтовые границы.
func assistantSizeBoundaryFixture(t *testing.T) (*assistantSnapshotSizeRecorder, *sessionMultiplexer, generated.PlatformSnapshotEnvelope) {
	t.Helper()
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
	return c, m, envelope
}
