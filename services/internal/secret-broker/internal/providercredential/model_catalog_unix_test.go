package providercredential

import (
	"syscall"
	"testing"
)

func createCatalogFIFO(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
