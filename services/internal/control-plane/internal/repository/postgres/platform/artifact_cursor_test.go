package platform

import (
	"errors"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestArtifactCursorRoundTrip round-trips a synthetic timestamp/artifact ref and requires equal
// time plus UTC normalization on decode. It tests cursor representation only, without signature,
// actor access or resource lookup.
// TestArtifactCursorRoundTrip проверяет round-trip synthetic timestamp/artifact ref с равным
// временем и UTC normalization при decode. Проверяет только cursor representation без signature,
// actor access либо resource lookup.
func TestArtifactCursorRoundTrip(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 29, 12, 34, 56, 789, time.FixedZone("test", 4*60*60))
	token := encodeArtifactCursor(createdAt, "art_12345678")
	decodedCreatedAt, decodedRef, err := decodeArtifactCursor(token)
	if err != nil || decodedCreatedAt == nil || !decodedCreatedAt.Equal(createdAt) || decodedRef != "art_12345678" {
		t.Fatalf("artifact cursor round-trip failed: created_at=%v ref=%q err=%v", decodedCreatedAt, decodedRef, err)
	}
	if decodedCreatedAt.Location() != time.UTC {
		t.Fatalf("artifact cursor timestamp is not normalized to UTC: %v", decodedCreatedAt.Location())
	}
}

// TestArtifactCursorRejectsMalformedValues rejects broken encoding/version, wrong resource prefix
// and newline-bearing ref while preserving empty-token first-page semantics. The local assertions
// do not establish source freshness or pagination eligibility.
// TestArtifactCursorRejectsMalformedValues отклоняет broken encoding/version, wrong resource
// prefix и newline-bearing ref, сохраняя empty-token first-page semantics. Local assertions не
// доказывают source freshness либо pagination eligibility.
func TestArtifactCursorRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	for _, token := range []string{
		"broken",
		"v2.Zm9v",
		encodeArtifactCursor(time.Now(), "project_12345678"),
		encodeArtifactCursor(time.Now(), "art_12345678\nsecond"),
	} {
		if _, _, err := decodeArtifactCursor(token); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("malformed artifact cursor accepted: token=%q err=%v", token, err)
		}
	}
	createdAt, ref, err := decodeArtifactCursor("")
	if err != nil || createdAt != nil || ref != "" {
		t.Fatalf("empty cursor must represent the first page: created_at=%v ref=%q err=%v", createdAt, ref, err)
	}
}
