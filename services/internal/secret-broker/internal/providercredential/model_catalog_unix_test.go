package providercredential

import (
	"syscall"
	"testing"
)

// createCatalogFIFO creates a private-mode local FIFO fixture to prove cache opening rejects non-regular
// files without blocking; it creates no network or Kubernetes resource.
// createCatalogFIFO создаёт локальный FIFO fixture с приватным mode для проверки отказа cache чтения
// не-regular файлам без блокировки; сетевых и Kubernetes ресурсов нет.
func createCatalogFIFO(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
