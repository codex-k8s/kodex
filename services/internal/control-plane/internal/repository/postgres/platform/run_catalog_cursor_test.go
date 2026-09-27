package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRunCatalogPosition(t *testing.T) {
	created := time.Date(2026, time.September, 27, 9, 45, 7, 123456000, time.UTC)
	position := runCatalogPosition(entity.Run{Ref: "run_newer", CreatedAt: created})
	at, ref, err := parseRunCatalogPosition(position)
	if err != nil || at == nil || !at.Equal(created) || ref != "run_newer" {
		t.Fatalf("run cursor roundtrip: at=%v ref=%q err=%v", at, ref, err)
	}
	if at, ref, err := parseRunCatalogPosition(""); err != nil || at != nil || ref != "" {
		t.Fatalf("empty run cursor: at=%v ref=%q err=%v", at, ref, err)
	}
	for _, invalid := range []string{
		"run_legacy_ref_only",
		"not-a-time|run_example",
		created.Format(time.RFC3339Nano) + "|",
		created.Format(time.RFC3339Nano) + "|run_example|extra",
	} {
		if _, _, err := parseRunCatalogPosition(invalid); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("invalid run cursor %q: %v", invalid, err)
		}
	}
}
