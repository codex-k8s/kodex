package platform

import (
	"strings"
	"testing"
)

// TestRuntimeFileCaptureMaterializesCandidates checks capture SQL retains the materialized
// candidate CTE and owner visibility predicate; this string guard does not measure execution
// count.
// TestRuntimeFileCaptureMaterializesCandidates проверяет сохранение материализованного CTE
// кандидатов и предиката видимости владельца в SQL захвата; строковая проверка не измеряет число
// исполнений.
func TestRuntimeFileCaptureMaterializesCandidates(t *testing.T) {
	if !strings.Contains(queryRuntimeFilesCaptureEntries, "candidates AS MATERIALIZED (") {
		t.Fatal("runtime file candidates must have a single materialization boundary")
	}
	if !strings.Contains(queryRuntimeFilesCaptureEntries, "control_plane.runtime_file_source_visible(") {
		t.Fatal("runtime file capture must retain owner eligibility")
	}
}
