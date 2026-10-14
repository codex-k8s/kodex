package platform

import (
	"strings"
	"testing"
)

// На большом каталоге повторный inline кандидатов умножал вызовы owner
// eligibility и срывал bounded claim. Барьер сохраняет тот же набор pins.
func TestRuntimeFileCaptureMaterializesCandidates(t *testing.T) {
	if !strings.Contains(queryRuntimeFilesCaptureEntries, "candidates AS MATERIALIZED (") {
		t.Fatal("runtime file candidates must have a single materialization boundary")
	}
	if !strings.Contains(queryRuntimeFilesCaptureEntries, "control_plane.runtime_file_source_visible(") {
		t.Fatal("runtime file capture must retain owner eligibility")
	}
}
