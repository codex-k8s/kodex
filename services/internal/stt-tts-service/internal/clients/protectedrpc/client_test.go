package protectedrpc

import "testing"

func TestWithProjectReferenceRequiresCanonicalLocator(t *testing.T) {
	ctx, err := WithProjectReference(t.Context(), "prj_abcdefgh")
	if err != nil || projectReference(ctx) != "prj_abcdefgh" {
		t.Fatalf("canonical project locator отклонён: %v", err)
	}
	for _, reference := range []string{"project", "prj_short", "prj_abcdef/", " prj_abcdefgh"} {
		if _, err := WithProjectReference(t.Context(), reference); err == nil {
			t.Fatalf("небезопасный project locator принят: %q", reference)
		}
	}
}
