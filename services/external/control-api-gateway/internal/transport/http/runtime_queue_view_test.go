package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestRuntimeQueueHTTPProjection verifies enum/presence through the same message normalization used by Run reads and streams.
// The server field remains structured metadata, not localized user text or an empty unsupported enum.
// TestRuntimeQueueHTTPProjection проверяет enum/presence через ту же нормализацию, что чтение и stream Run.
// Серверное поле остаётся структурированными метаданными, не локализованным текстом либо пустым enum.
func TestRuntimeQueueHTTPProjection(t *testing.T) {
	for _, reason := range []string{"", "WAITING_RESOURCES", "REQUEST_EXCEEDS_CAPACITY"} {
		run := systemRunIdentityFixture()
		run.State = cp.RunState_RUN_STATE_QUEUED
		if reason != "" {
			run.QueueReason = &reason
		}
		writer := httptest.NewRecorder()
		writeMessage(writer, http.StatusOK, &cp.GetRunResponse{Run: run}, "run", "")
		if writer.Code != http.StatusOK {
			t.Fatal("queue response rejected")
		}
		var body map[string]any
		if json.Unmarshal(writer.Body.Bytes(), &body) != nil {
			t.Fatal("invalid queue response")
		}
		if body["state"] != "QUEUED" {
			t.Fatal("owner queue state lost")
		}
		value, present := body["queueReason"]
		if reason == "" && present || reason != "" && (!present || value != reason) {
			t.Fatal("queue reason lost or fabricated during HTTP normalization")
		}
	}
}
