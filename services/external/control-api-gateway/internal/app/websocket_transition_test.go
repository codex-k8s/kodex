package app

import (
	"testing"
	"time"
)

// TestWebSocketLegacyCutoffIsAbsoluteBoundedAndExpiredIsSafe covers absent/past/exact-bound UTC cutoffs and rejects invalid, overlong-future and offset-form text using a fixed clock; it does not open a WebSocket.
// TestWebSocketLegacyCutoffIsAbsoluteBoundedAndExpiredIsSafe на фиксированных часах охватывает отсутствующий, прошлый и граничный UTC cutoff и отклоняет invalid, overlong-future и offset-form текст без открытия WebSocket.
func TestWebSocketLegacyCutoffIsAbsoluteBoundedAndExpiredIsSafe(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	for _, value := range []string{"", "2026-09-08T11:00:00Z", "2026-09-09T12:00:00Z"} {
		if _, err := (Config{WebSocketLegacyUntil: value}).legacyWebSocketDeadline(now); err != nil {
			t.Fatal("valid cutoff rejected", err)
		}
	}
	for _, value := range []string{"invalid", "2026-09-09T12:00:01Z", "2026-09-08T12:00:00+00:00"} {
		if _, err := (Config{WebSocketLegacyUntil: value}).legacyWebSocketDeadline(now); err == nil {
			t.Fatal("unsafe cutoff accepted")
		}
	}
}
