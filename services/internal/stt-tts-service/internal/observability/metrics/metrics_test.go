package metrics

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestLabelsUseClosedSets checks that unknown stage/class values collapse to fixed fallbacks
// while approved labels retain their distinct values.
// RU: TestLabelsUseClosedSets проверяет сведение неизвестных меток к фиксированным значениям
// при сохранении отдельных значений для утверждённых этапов и классов.
func TestLabelsUseClosedSets(t *testing.T) {
	observer := New()
	observer.Observe(value.Stage("audio-content"), value.ErrorClass("provider-detail"))
	if count := testutil.ToFloat64(observer.completed.WithLabelValues(string(value.StageUnknown), string(value.ErrorUnknown))); count != 1 {
		t.Fatalf("unknown labels count=%v", count)
	}
	if normalizeStage(value.StageProvider) != "provider" || normalizeClass(value.ErrorTimeout) != "timeout" {
		t.Fatal("утверждённые bounded labels потеряны")
	}
}
