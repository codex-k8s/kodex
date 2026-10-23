package boundary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

// testSessionKeys creates a real cookie store with a temporary, read-only synthetic AES key and a fifteen-minute TTL. It shares only fixture setup, not expected security outcomes, and reports setup failures at the calling test.
// testSessionKeys создаёт настоящее хранилище cookies с временным синтетическим AES-ключом только для чтения и TTL пятнадцать минут. Общей является только подготовка оснастки, а не ожидаемые результаты безопасности; ошибки указывают на вызывающий тест.
func testSessionKeys(t *testing.T) *session.Store {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("44", 32)), 0400); err != nil {
		t.Fatal(err)
	}
	keys, err := session.New(session.Config{CurrentKeyFile: keyPath, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
