package platform

import (
	"errors"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestAuditCursorRoundTrip(t *testing.T) {
	t.Parallel()
	occurredAt := time.Date(2026, 8, 31, 12, 34, 56, 789, time.FixedZone("test", 4*60*60))
	token := encodeAuditCursor(occurredAt, "aud_12345678")

	decodedOccurredAt, decodedRef, err := decodeAuditCursor(token)
	if err != nil || decodedOccurredAt == nil || !decodedOccurredAt.Equal(occurredAt) || decodedRef != "aud_12345678" {
		t.Fatalf("audit cursor round-trip failed: occurred_at=%v ref=%q err=%v", decodedOccurredAt, decodedRef, err)
	}
	if decodedOccurredAt.Location() != time.UTC {
		t.Fatalf("audit cursor timestamp is not normalized to UTC: %v", decodedOccurredAt.Location())
	}
}

func TestAuditCursorRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	for _, token := range []string{
		"broken",
		"v2.Zm9v",
		encodeMVPCursor("schedule", time.Now(), "aud_12345678"),
		encodeAuditCursor(time.Now(), "run_12345678"),
		encodeAuditCursor(time.Now(), "aud_12345678\nsecond"),
	} {
		if _, _, err := decodeAuditCursor(token); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("malformed audit cursor accepted: token=%q err=%v", token, err)
		}
	}
	occurredAt, ref, err := decodeAuditCursor("")
	if err != nil || occurredAt != nil || ref != "" {
		t.Fatalf("empty cursor must represent the first page: occurred_at=%v ref=%q err=%v", occurredAt, ref, err)
	}
}
