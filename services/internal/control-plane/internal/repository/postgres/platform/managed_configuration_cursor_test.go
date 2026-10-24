package platform

import (
	"errors"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestManagedHistoryCursorIsBoundToConfiguration checks version-boundary roundtrip for one
// configuration and exact rejection of the same cursor for another configuration, without using
// the cursor as identity.
// TestManagedHistoryCursorIsBoundToConfiguration проверяет version-boundary roundtrip одной
// configuration и точный отказ тому же cursor для другой configuration без использования cursor
// как identity.
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
