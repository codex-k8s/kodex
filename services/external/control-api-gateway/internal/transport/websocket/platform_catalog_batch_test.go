package websockettransport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc/status"
)

// TestHeartbeatRunCatalogDoesNotRepeatOwnerBundle injects a slow synthetic catalog read and checks four contiguous RUN deltas share one fresh owner bundle/run read within a heartbeat, preserving exact cursor order.
// TestHeartbeatRunCatalogDoesNotRepeatOwnerBundle замедляет тестовое чтение каталога и проверяет один свежий owner bundle и чтение запуска для четырёх непрерывных RUN delta за heartbeat с точным порядком курсора.
func TestHeartbeatRunCatalogDoesNotRepeatOwnerBundle(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	c.beforeInvoke = func(ctx context.Context, response any) error {
		if _, catalog := response.(*cp.ListRunsResponse); !catalog {
			return nil
		}
		delay := time.NewTimer(650 * time.Millisecond)
		defer delay.Stop()
		select {
		case <-delay.C:
			return nil
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
	queueHeartbeatRunWakes(t, m, 4)
	if !m.heartbeat(time.Unix(1, 0)) {
		t.Fatal("bounded heartbeat transport failed")
	}
	frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
	deltas := 0
	for _, frame := range frames {
		switch value := frame.(type) {
		case generated.StreamProblemEnvelope:
			t.Fatalf("repeated owner catalog bundle exhausted shared budget: code=%s cursor=%d reads=%d", value.Code, value.Cursor, c.catalogReads)
		case generated.PlatformSnapshotEnvelope:
			if value.Cursor != int64(8+deltas) || value.Kind != "RUN" || value.Mode != generated.PlatformSnapshotModeDelta {
				t.Fatal("catalog batch lost exact scoped delta order")
			}
			deltas++
		}
	}
	if deltas != 4 || c.catalogReads != 1 || c.runReads != 1 || m.platformCursor != 11 || !m.platformAvailable {
		t.Fatal("contiguous RUN batch repeated owner bundle or lost cursor/readiness")
	}
}

// TestHeartbeatRunCatalogReuseBoundaries independently checks reuse stops at kind/authority/project/gap/denial boundaries, duplicate handling and exact resync counts; it never treats cached data as renewed permission.
// TestHeartbeatRunCatalogReuseBoundaries независимо проверяет прекращение переиспользования на границах вида, полномочий, проекта, разрыва и отказа, дубликаты и точное число resync; кэшированные данные не становятся новым разрешением.
func TestHeartbeatRunCatalogReuseBoundaries(t *testing.T) {
	for _, boundary := range []string{"other-kind", "authority-kind", "foreign-project", "gap", "denied", "duplicate"} {
		t.Run(boundary, func(t *testing.T) {
			m, c := heartbeatRunBatchFixture(t)
			c.denyCatalog = boundary == "denied"
			for index, sequence := range []int64{8, 9, 10, 11} {
				signal := platformWakeRaceSignal(t, sequence)
				signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
				if index == 1 {
					switch boundary {
					case "other-kind":
						signal.Kind, signal.EventName = "SYSTEM_ASSISTANT", "SYSTEM_ASSISTANT_CHANGED"
					case "authority-kind":
						signal.Kind, signal.EventName = "MEMBERSHIP", "MEMBERSHIP_CHANGED"
					case "foreign-project":
						signal.ProjectRef = "prj_foreign01"
					case "gap":
						signal.Sequence = 10
					case "duplicate":
						signal.Sequence = 8
					}
				}
				if boundary == "duplicate" && index > 1 {
					signal.Sequence--
				}
				m.platformSignals <- signal
			}
			available, ok := m.drainHeartbeatWakes()
			if !available || !ok {
				t.Fatal("catalog boundary lost closed transport behavior")
			}
			wantReads := 2
			switch boundary {
			case "gap", "duplicate", "authority-kind":
				wantReads = 1
			case "denied":
				wantReads = 4
			}
			if c.catalogReads != wantReads {
				t.Fatalf("catalog crossed boundary or reused denied result: reads=%d want=%d", c.catalogReads, wantReads)
			}
			frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
			resyncs := 0
			for _, frame := range frames {
				if _, resync := frame.(generated.PlatformResyncEnvelope); resync {
					resyncs++
				}
			}
			// An authority change also clears the financial projection through authoritative rejoin, even when later wakes are already queued in FIFO order.
			// Смена полномочий очищает и финансовую проекцию через authoritative rejoin, даже если последующие wakes уже стоят в FIFO.
			wantResync := boundary == "gap" || boundary == "authority-kind"
			if (wantResync && resyncs != 1) || (!wantResync && resyncs != 0) {
				t.Fatal("catalog reuse lost a gap or authority resync boundary")
			}
		})
	}
}

// TestHeartbeatRunCatalogCancellationAndOverflow cancels an owner overview or fills the outbound queue and verifies no later run read or additional cached frame is published beyond the already-admitted cursor.
// TestHeartbeatRunCatalogCancellationAndOverflow отменяет owner overview либо заполняет очередь и проверяет отсутствие следующего чтения запуска и дополнительных кэшированных кадров сверх уже допущенного курсора.
func TestHeartbeatRunCatalogCancellationAndOverflow(t *testing.T) {
	for _, condition := range []string{"cancel", "overflow"} {
		t.Run(condition, func(t *testing.T) {
			m, c := heartbeatRunBatchFixture(t)
			parent, cancel := context.WithCancel(m.ctx)
			defer cancel()
			m.ctx = parent
			if condition == "cancel" {
				c.beforeInvoke = func(_ context.Context, response any) error {
					if _, overview := response.(*cp.GetOverviewResponse); overview {
						cancel()
					}
					return nil
				}
			} else {
				c.frames = make(chan outboundFrame, 1)
				m.outbound = c.frames
			}
			queueHeartbeatRunWakes(t, m, 4)
			if m.heartbeat(time.Unix(1, 0)) || c.runReads != 0 || c.catalogReads != 1 {
				t.Fatal("cancel/overflow continued owner bundle or RUN read")
			}
			frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
			if (condition == "cancel" && (len(frames) != 0 || m.platformCursor != 7)) ||
				(condition == "overflow" && (len(frames) != 1 || m.platformCursor != 8)) {
				t.Fatal("cancel/overflow published additional cached frames")
			}
		})
	}
}

// TestHeartbeatRunCatalogRechecksCompleteEnvelopeByteCap measures a full synthetic RUN delta at the exact byte cap and proves the next longer cursor envelope closes with one problem instead of bypassing size validation via reuse.
// TestHeartbeatRunCatalogRechecksCompleteEnvelopeByteCap измеряет полный тестовый RUN delta на точной байтовой границе и доказывает, что следующий конверт с более длинным курсором завершается одной ошибкой, а не обходит проверку через переиспользование.
func TestHeartbeatRunCatalogRechecksCompleteEnvelopeByteCap(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	payloadSize := 1
	c.beforeInvoke = func(_ context.Context, response any) error {
		if runs, ok := response.(*cp.ListRunsResponse); ok {
			run := completeRunSnapshotFixture().Run
			run.Title = strings.Repeat("a", payloadSize)
			runs.Runs, runs.Total = []*cp.Run{run}, 1
		}
		return nil
	}
	event := generated.PlatformEventName("RUN_CHANGED")
	project := m.projectRef
	envelope, err := m.boundedPlatformSnapshotWithin(m.ctx, generated.PlatformSnapshotEnvelope{
		Type: "PLATFORM_SNAPSHOT", RequestRef: m.platformRequestRef, StreamKind: "PLATFORM", StreamRef: platformStreamRef,
		Cursor: 9, Mode: generated.PlatformSnapshotModeDelta, Kind: "RUN", EventName: &event, ProjectRef: &project,
	})
	if err != nil {
		t.Fatal("synthetic byte-cap baseline failed")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal("synthetic byte-cap baseline encoding failed")
	}
	payloadSize += maximumFrameBytes - len(encoded)
	c.catalogReads = 0
	m.platformCursor, c.current = 8, 10
	for _, sequence := range []int64{9, 10} {
		signal := platformWakeRaceSignal(t, sequence)
		signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
		m.platformSignals <- signal
	}
	if !m.heartbeat(time.Unix(1, 0)) || c.catalogReads != 1 || m.platformAvailable || m.platformCursor != 9 {
		t.Fatal("cached snapshot bypassed exact envelope byte cap")
	}
	snapshots, problems := 0, 0
	for _, frame := range platformWakeRaceFrames(&c.platformWakeRaceRecorder) {
		switch value := frame.(type) {
		case generated.PlatformSnapshotEnvelope:
			encoded, err := json.Marshal(value)
			if err != nil || len(encoded) != maximumFrameBytes {
				t.Fatal("first exact-size snapshot was changed")
			}
			snapshots++
		case generated.StreamProblemEnvelope:
			if value.Code != "PLATFORM_UNAVAILABLE" || value.Cursor != 9 {
				t.Fatal("oversized cached envelope lost closed failure")
			}
			problems++
		}
	}
	if snapshots != 1 || problems != 1 {
		t.Fatal("oversized cached envelope escaped projection boundary")
	}
}
