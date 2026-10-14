package websockettransport

import (
	"encoding/json"
	"testing"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

func TestAssistantColdPageBootstrapSkipAndRejoinStartAt25(t *testing.T) {
	for _, path := range []string{"bootstrap", "bootstrap-skipped", "new-socket-rejoin"} {
		t.Run(path, func(t *testing.T) {
			if path == "new-socket-rejoin" {
				previous, oldOwner := assistantPageHintFixture(t)
				for _, conversation := range oldOwner.conversations {
					conversation.Turns = append(conversation.Turns, conversation.Turns[0])
				}
				if _, err := previous.sendPlatformBootstrap(); err != nil || previous.assistantPageHint.pageSize != 12 {
					t.Fatal("previous socket did not learn its independent smaller page")
				}
				previous.closeSubscriptions()
			}
			m, c := assistantPageHintFixture(t)
			if path == "bootstrap" {
				if _, err := m.sendPlatformBootstrap(); err != nil {
					t.Fatal("cold bootstrap failed")
				}
			} else {
				// Тот же predicate используется initializePlatform при полном browser cache.
				if needsPlatformBootstrap(m.platformCursor, m.platformCursor, false) {
					t.Fatal("exact cached rejoin unexpectedly requires bootstrap")
				}
				if len(c.requests) != 0 || m.assistantPageHint != (assistantSnapshotPageHint{}) {
					t.Fatal("new socket borrowed a hint or authoritative payload")
				}
				if !m.applyPlatformSignal(platformSignal{Sequence: 124, Kind: "SYSTEM_ASSISTANT", EventName: "SYSTEM_ASSISTANT_CHANGED", ProjectRef: m.projectRef}) {
					t.Fatal("cold wake failed")
				}
			}
			frame := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
			encoded, err := json.Marshal(frame)
			if err != nil || len(encoded) > maximumFrameBytes || len(c.requests) != 1 ||
				c.requests[0].Page.PageSize != 25 || c.requests[0].Page.PageToken != "" ||
				c.assistantReads != 1 || c.conversationReads != 1 || c.bootstrapReads != 1 ||
				len(frame.Snapshot.Conversations.Conversations) != 25 ||
				*frame.Snapshot.Conversations.Page.NextPageToken != "owner-cursor-25" || m.assistantPageHint.pageSize != 25 {
				t.Fatal("cold path retried the oversized fifty-row page or lost full authoritative pagination")
			}
			for index, item := range frame.Snapshot.Conversations.Conversations {
				if item.Turns[0].Content != c.conversations[index].Turns[0].Content || item.Version != c.conversations[index].Version {
					t.Fatal("cold page truncated a resource or changed its pins")
				}
			}
		})
	}
}
