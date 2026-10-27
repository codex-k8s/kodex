package command

import (
	"encoding/json"
	"testing"
)

// TestLegacyArchiveCompletionKeepsHistoricalDigestInput preserves exact legacy JSON when optional work metadata is absent.
// Historical idempotency inputs must not change merely because a new reader supports V2 checkpoints.
// TestLegacyArchiveCompletionKeepsHistoricalDigestInput сохраняет exact legacy JSON без optional work metadata.
// Historical idempotency inputs не меняются только из-за поддержки V2 checkpoints новым reader.
func TestLegacyArchiveCompletionKeepsHistoricalDigestInput(t *testing.T) {
	input := SessionArchiveTaskInput{TaskRef: "sat_fixture", LeaseRef: "lea_fixture", Fence: "fixture-fence", Generation: 1,
		FormatVersion: 1, ObjectKey: "fixture", ObjectVersion: "v1", ObjectETag: "etag", ObjectDigest: "digest", ObjectSizeBytes: 4096, SourceSizeBytes: 12}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	wanted := `{"TaskRef":"sat_fixture","LeaseRef":"lea_fixture","Fence":"fixture-fence","SafeErrorCode":"","ObjectKey":"fixture","ObjectVersion":"v1","ObjectETag":"etag","ObjectDigest":"digest","RestoredSourceSHA256":"","PVCName":"","Generation":1,"ObjectSizeBytes":4096,"SourceSizeBytes":12,"FormatVersion":1}`
	if string(raw) != wanted {
		t.Fatal("optional workspace metadata changed the historical idempotency input")
	}
	input.SnapshotSourceSHA256, input.SourceEntryCount = "tree", 1
	raw, err = json.Marshal(input)
	var decoded map[string]any
	if err != nil || json.Unmarshal(raw, &decoded) != nil || decoded["SnapshotSourceSHA256"] != "tree" || decoded["SourceEntryCount"] != float64(1) {
		t.Fatal("workspace completion dropped immutable source metadata")
	}
}
