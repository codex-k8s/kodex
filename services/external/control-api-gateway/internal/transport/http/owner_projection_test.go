package httptransport

import (
	"slices"
	"strings"
	"testing"
)

func TestRedactedPreviewDoesNotExposeValues(t *testing.T) {
	t.Parallel()
	secret := "private-value-that-must-not-escape"
	summary, fields, err := redactedPreview([]byte(`{"zeta":"` + secret + `","alpha":{"token":"nested"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Параметры запроса: 2" || !slices.Equal(fields, []string{"alpha", "zeta"}) {
		t.Fatalf("unexpected redacted preview: %q %#v", summary, fields)
	}
	if strings.Contains(summary+strings.Join(fields, ","), secret) {
		t.Fatal("redacted preview exposed a private value")
	}
}

func TestRedactedPreviewRejectsTrailingPayload(t *testing.T) {
	t.Parallel()
	if _, _, err := redactedPreview([]byte(`{"safe":"field"} trailing`)); err == nil {
		t.Fatal("trailing payload was accepted")
	}
}

func TestValidSHA256RejectsNonHexDigest(t *testing.T) {
	t.Parallel()
	if validSHA256(strings.Repeat("z", 64)) {
		t.Fatal("non-hex digest was accepted")
	}
	if !validSHA256(strings.Repeat("a", 64)) {
		t.Fatal("valid digest was rejected")
	}
}
