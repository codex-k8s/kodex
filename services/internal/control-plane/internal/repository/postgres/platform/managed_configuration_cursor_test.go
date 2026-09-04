package platform

import (
	"errors"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestManagedHistoryCursorIsBoundToConfiguration(t *testing.T) {
	token := encodeManagedHistoryCursor("mcfg_example01", 7)
	cursor, err := decodeManagedHistoryCursor(token, "mcfg_example01")
	if err != nil || cursor.Before != 7 {
		t.Fatalf("decode managed history cursor: cursor=%#v err=%v", cursor, err)
	}
	if _, err := decodeManagedHistoryCursor(token, "mcfg_example02"); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("cursor was accepted for another configuration: %v", err)
	}
}
