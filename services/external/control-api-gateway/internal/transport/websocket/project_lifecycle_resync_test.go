package websockettransport

import (
	"context"
	"testing"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

// TestProjectLifecycleSignalResyncsDependentCatalogs requires project lifecycle wakes to invalidate all owner catalogs and refresh registered roots, rather than returning only a project page with stale run and gate data.
// TestProjectLifecycleSignalResyncsDependentCatalogs требует инвалидации всех каталогов владельца и обновления зарегистрированных корней при событии проекта вместо одной страницы проектов с устаревшими запусками и решениями.
func TestProjectLifecycleSignalResyncsDependentCatalogs(t *testing.T) {
	m, recorder := platformWakeRaceFixture(t, 8)
	signal := platformWakeRaceSignal(t, 8)
	signal.Kind, signal.EventName = "PROJECT", "PROJECT_CHANGED"
	refreshes, pageReads := 0, 0
	ok := m.applyPlatformSignalWithReaders(t.Context(), signal, func(context.Context) bool {
		refreshes++
		return true
	}, func(_ context.Context, envelope generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error) {
		pageReads++
		return envelope, nil
	})
	frames := platformWakeRaceFrames(recorder)
	if !ok || refreshes != 1 || pageReads != 0 || len(frames) != 1 {
		t.Fatal("project lifecycle did not refresh dependent catalogs and subscribed roots")
	}
	resync, ok := frames[0].(generated.PlatformResyncEnvelope)
	if !ok || resync.Cursor != 8 || resync.Reason != "AUTHORITATIVE_READ_REQUIRED" {
		t.Fatal("project lifecycle lost authoritative resync cursor")
	}
}
