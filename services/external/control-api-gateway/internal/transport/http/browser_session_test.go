package httptransport

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
)

func TestSessionMetadataDistinguishesLegacyBearerAndBackendRefresh(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	metadata := boundary.SessionMetadata{Generation: "546a9142-c442-4879-90c2-9e8b075b6a8d", Version: 7, SessionRevision: 1,
		ServerTime: now, ExpiresAt: now.Add(15 * time.Minute), AccessExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(8 * time.Hour), RenewAfter: now.Add(10 * time.Minute), BackendRefresh: true}
	mapped, ok := sessionMetadataMap(metadata)
	if !ok || mapped.RenewalMode != "BACKEND_REFRESH" || mapped.Version != 7 || !mapped.AbsoluteExpiresAt.Equal(metadata.AbsoluteExpiresAt) {
		t.Fatal("backend session metadata lost authoritative bounds")
	}
	metadata.BackendRefresh = false
	metadata.AbsoluteExpiresAt = metadata.AccessExpiresAt
	mapped, ok = sessionMetadataMap(metadata)
	if !ok || mapped.RenewalMode != "REAUTHENTICATION" {
		t.Fatal("legacy bearer session advertised refresh authority")
	}
	for name, mutate := range map[string]func(*boundary.SessionMetadata){
		"generation":   func(v *boundary.SessionMetadata) { v.Generation = "caller" },
		"version":      func(v *boundary.SessionMetadata) { v.Version = 1 << 53 },
		"expired":      func(v *boundary.SessionMetadata) { v.ExpiresAt = now.Add(-time.Second) },
		"absolute":     func(v *boundary.SessionMetadata) { v.AbsoluteExpiresAt = now },
		"late-renewal": func(v *boundary.SessionMetadata) { v.RenewAfter = v.ExpiresAt.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := metadata
			mutate(&candidate)
			if _, ok := sessionMetadataMap(candidate); ok {
				t.Fatal("inconsistent metadata accepted")
			}
		})
	}
}
