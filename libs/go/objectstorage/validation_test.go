package objectstorage

import (
	"strings"
	"testing"
)

// TestValidKey checks ordinary tenant-scoped keys and rejects absolute, trailing, traversing, empty, and NUL-bearing paths.
// TestValidKey проверяет обычные ключи с областью арендатора и отклоняет абсолютные, незавершённые, обходящие каталог, пустые и содержащие NUL пути.
func TestValidKey(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"organizations/org/projects/prj/artifacts/art/1", "backups/2026/08/snapshot.tar.zst"} {
		if !ValidKey(value) {
			t.Fatalf("valid key rejected: %q", value)
		}
	}
	for _, value := range []string{"", "/absolute", "trailing/", "a/../b", "../escape", "a\x00b"} {
		if ValidKey(value) {
			t.Fatalf("invalid key accepted: %q", value)
		}
	}
}

// TestValidDigest checks the accepted SHA-256 textual shape and rejects non-hexadecimal payloads.
// TestValidDigest проверяет допустимую текстовую форму SHA-256 и отклоняет не шестнадцатеричное содержимое.
func TestValidDigest(t *testing.T) {
	t.Parallel()
	if !ValidDigest("sha256:" + strings.Repeat("a", 64)) {
		t.Fatal("valid digest rejected")
	}
	if ValidDigest("sha256:" + strings.Repeat("z", 64)) {
		t.Fatal("invalid digest accepted")
	}
}
