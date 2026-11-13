package platform

import (
	"context"
	"testing"
)

// TestContextWindowUnsetDoesNotRequireInventory proves legacy inputs never read inventory, including older bootstrap images and native/cache preferences.
// TestContextWindowUnsetDoesNotRequireInventory доказывает отсутствие чтения inventory для прежних входов, включая старый bootstrap и native/cache preferences.
func TestContextWindowUnsetDoesNotRequireInventory(t *testing.T) {
	for _, overlay := range []string{"", "[context]\nauto_compaction = true\nprune = false", "[cache]\noptimization = \"NO_HINTS\""} {
		if err := requireRuntimeOverlayImageFeature(context.Background(), nil, "tenant", "legacy-image", "legacy-digest", overlay); err != nil {
			t.Fatal("legacy input required image evidence", err)
		}
		if err := requireCurrentRuntimeOverlayImageFeature(context.Background(), nil, "tenant", "legacy-agent", overlay); err != nil {
			t.Fatal("legacy input required binding inventory", err)
		}
	}
}
