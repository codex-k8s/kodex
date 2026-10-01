package websockettransport

import "testing"

func TestTypedPlatformSnapshotRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := typedPlatformSnapshot("WORKFLOW", map[string]any{
		"catalog": map[string]any{"workflows": []any{}, "unexpected": true},
	})
	if err == nil {
		t.Fatal("expected unknown snapshot field to be rejected")
	}
}

func TestTypedPlatformSnapshotBindsPayloadToResourceKind(t *testing.T) {
	t.Parallel()
	_, err := typedPlatformSnapshot("SYSTEM_ASSISTANT", map[string]any{
		"catalog": map[string]any{"projects": []any{}},
	})
	if err == nil {
		t.Fatal("expected mismatched snapshot kind to be rejected")
	}
}

func TestTypedPlatformSnapshotAcceptsClosedWorkflowCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("WORKFLOW", map[string]any{
		"catalog": map[string]any{
			"workflows": []any{},
			"page":      map[string]any{"nextPageToken": "cursor"},
		},
	})
	if err != nil {
		t.Fatalf("decode typed snapshot: %v", err)
	}
	if snapshot.Catalog == nil || snapshot.Catalog.Page == nil || snapshot.Catalog.Page.NextPageToken == nil || *snapshot.Catalog.Page.NextPageToken != "cursor" {
		t.Fatal("typed workflow cursor was not preserved")
	}
}
