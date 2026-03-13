package missioncontrol

import (
	"strings"
	"testing"
)

func TestUpdateEntityProjectionQueryGuardsProjectionVersion(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryUpdateEntityProjection, "projection_version = mission_control_entities.projection_version + 1") {
		t.Fatal("update_entity_projection query must increment projection_version")
	}
	if !strings.Contains(queryUpdateEntityProjection, "AND projection_version = $14") {
		t.Fatal("update_entity_projection query must enforce expected projection_version")
	}
}

func TestWarmupSummaryQueryCountsAllFoundationTables(t *testing.T) {
	t.Parallel()

	required := []string{
		"mission_control_entities",
		"mission_control_relations",
		"mission_control_timeline_entries",
		"mission_control_commands",
	}
	for _, item := range required {
		if !strings.Contains(queryGetWarmupSummary, item) {
			t.Fatalf("warmup summary query must reference %s", item)
		}
	}
}
