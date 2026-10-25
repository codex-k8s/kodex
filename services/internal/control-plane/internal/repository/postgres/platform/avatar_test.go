package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestParseAvatarArtifactURL accepts one canonical same-origin preview path and whitespace
// clearing, then rejects absolute/network-path, missing/alternate/extra query, nested path and
// fragment variants. It tests syntax only; no artifact, scan or access backend is used.
// TestParseAvatarArtifactURL принимает один canonical same-origin preview path и whitespace
// clearing, затем отклоняет absolute/network-path, missing/alternate/extra query, nested path и
// fragment variants. Проверяет только syntax без artifact, scan либо access backend.
func TestParseAvatarArtifactURL(t *testing.T) {
	t.Parallel()

	const valid = "/api/v1/artifacts/art_12345678/content?purpose=PREVIEW"
	canonical, artifactRef, err := parseAvatarArtifactURL(valid)
	if err != nil || canonical != valid || artifactRef != "art_12345678" {
		t.Fatalf("parse canonical avatar URL: canonical=%q ref=%q err=%v", canonical, artifactRef, err)
	}
	if canonical, artifactRef, err := parseAvatarArtifactURL("  "); err != nil || canonical != "" || artifactRef != "" {
		t.Fatalf("empty avatar URL must clear the avatar: canonical=%q ref=%q err=%v", canonical, artifactRef, err)
	}

	for _, value := range []string{
		"https://example.invalid/avatar.png",
		"//example.invalid/avatar.png",
		"/api/v1/artifacts/art_12345678/content",
		"/api/v1/artifacts/art_12345678/content?purpose=DOWNLOAD",
		"/api/v1/artifacts/art_12345678/content?purpose=PREVIEW&extra=1",
		"/api/v1/artifacts/art_12345678/other?purpose=PREVIEW",
		"/api/v1/artifacts/art_12345678/nested/content?purpose=PREVIEW",
		"/api/v1/artifacts/art_12345678/content?purpose=PREVIEW#fragment",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parseAvatarArtifactURL(value); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("unsafe avatar URL %q was accepted: %v", value, err)
			}
		})
	}
}
