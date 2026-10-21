package credentialfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreRejectsPathEscape places a credential symlink outside the root and
// requires Read to reject it without returning the target material.
// RU: TestStoreRejectsPathEscape помещает ссылку credential за пределы корня
// и требует отказа Read без выдачи содержимого цели.
func TestStoreRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	connection := filepath.Join(root, "connection_01")
	if err := os.Mkdir(connection, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(connection, "token")); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("connection_01", "token"); err == nil {
		t.Fatal("ожидался закрытый отказ для symlink за пределы credential root")
	}
}

// TestStoreReadsBoundedPrivateFile checks a read-only file inside one valid
// connection reference and clears the returned test buffer afterwards.
// RU: TestStoreReadsBoundedPrivateFile проверяет чтение read-only файла внутри
// допустимого подключения и затем очищает полученный тестовый буфер.
func TestStoreReadsBoundedPrivateFile(t *testing.T) {
	root := t.TempDir()
	connection := filepath.Join(root, "connection_01")
	if err := os.Mkdir(connection, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(connection, "token"), []byte("value"), 0o400); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.Read("connection_01", "token")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(value)
	if string(value) != "value" {
		t.Fatalf("получено неожиданное значение длиной %d", len(value))
	}
}

// TestStoreReadsProjectedExactSecretKey models Kubernetes ..data symlinks
// whose final read-only file remains within the selected Secret root.
// RU: TestStoreReadsProjectedExactSecretKey воспроизводит ссылки Kubernetes
// ..data, у которых конечный read-only файл остаётся внутри корня Secret.
func TestStoreReadsProjectedExactSecretKey(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "..2026_08_28")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "github-token"), []byte("value"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..2026_08_28", filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "github-token"), filepath.Join(root, "github-token")); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.ReadKey("github-token")
	if err != nil || string(value) != "value" {
		t.Fatalf("ReadKey() = %q, %v", value, err)
	}
}

// TestStoreRejectsProjectedKeyOutsideRoot points a projected data key beyond
// the mounted root and requires the same closed rejection as a normal file.
// RU: TestStoreRejectsProjectedKeyOutsideRoot направляет projected-ключ за
// пределы mount и требует закрытого отказа, как для обычного файла.
func TestStoreRejectsProjectedKeyOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "github-token")
	if err := os.WriteFile(outside, []byte("secret"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "github-token")); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadKey("github-token"); err == nil {
		t.Fatal("ожидался закрытый отказ для projected key за пределами credential root")
	}
}
