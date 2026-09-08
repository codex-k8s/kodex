package app

import (
	"testing"
	"time"
)

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
