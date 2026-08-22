package credentialfs

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestStoreReadsBoundedPrivateFile(t *testing.T) {
	root := t.TempDir()
	connection := filepath.Join(root, "connection_01")
	if err := os.Mkdir(connection, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(connection, "token"), []byte("value"), 0o600); err != nil {
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
