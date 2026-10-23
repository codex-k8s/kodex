package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// assistantConfigurationDecodeFixture is a deliberately small local JSON target
// for decoder-boundary tests; it has no owner identity or executable permissions.
// assistantConfigurationDecodeFixture — небольшой локальный JSON target проверки
// decoder boundary без owner identity и executable permissions.
type assistantConfigurationDecodeFixture struct {
	Name string `json:"name"`
}

// TestDecodeAssistantConfigurationSnapshotBindsExactBytesBeforeMutation checks
// matching canonical lowercase hashes permit typed decode, while malformed,
// uppercase or foreign digests reject before touching the supplied target.
// TestDecodeAssistantConfigurationSnapshotBindsExactBytesBeforeMutation проверяет
// typed decode при точном canonical lowercase hash и отказ malformed,uppercase,
// foreign digests до изменения переданного target.
func TestDecodeAssistantConfigurationSnapshotBindsExactBytesBeforeMutation(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"name":"Пример"}`)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	for _, invalid := range []string{"", strings.Repeat("0", 64), strings.ToUpper(digest), "sha256:" + digest} {
		target := assistantConfigurationDecodeFixture{Name: "unchanged"}
		if decodeAssistantConfigurationSnapshot(raw, invalid, &target) || target.Name != "unchanged" {
			t.Fatal("digest mismatch mutated or accepted configuration target")
		}
	}
	var target assistantConfigurationDecodeFixture
	if !decodeAssistantConfigurationSnapshot(raw, digest, &target) || target.Name != "Пример" {
		t.Fatal("exact digest failed to decode configuration")
	}
	raw[0] = '['
	if decodeAssistantConfigurationSnapshot(raw, digest, &target) || target.Name != "Пример" {
		t.Fatal("changed bytes bypassed digest binding")
	}
}

// TestDecodeAssistantConfigurationSnapshotKeepsCallerCanonicalChecks proves the
// helper preserves single-value decoder semantics: duplicate/case aliases, null,
// trailing values and replacement UTF-8 are not canonicality checks here. Unknown
// fields/type/syntax errors fail, but decoder partial writes remain observable.
// TestDecodeAssistantConfigurationSnapshotKeepsCallerCanonicalChecks доказывает
// прежнюю single-value decoder semantics: duplicate/case aliases,null,trailing
// values и replacement UTF-8 не проверяются здесь на canonicality. Unknown fields,
// type/syntax errors отвергаются, но partial writes decoder остаются видимыми.
func TestDecodeAssistantConfigurationSnapshotKeepsCallerCanonicalChecks(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name, raw, expected string
		accepted            bool
	}{
		{"valid", `{"name":"ready"}`, "ready", true},
		{"unknown", `{"name":"partial","private":true}`, "partial", false},
		{"wrong_type", `{"name":12}`, "original", false},
		{"syntax", `{"name":`, "original", false},
		{"duplicate", `{"name":"first","name":"last"}`, "last", true},
		{"case_alias", `{"Name":"alias"}`, "alias", true},
		{"null", `null`, "original", true},
		{"trailing", `{"name":"first"} {"name":"second"}`, "first", true},
		{"replacement_utf8", "{\"name\":\"\xff\"}", "\ufffd", true},
	} {
		t.Run(item.name, func(t *testing.T) {
			raw := []byte(item.raw)
			sum := sha256.Sum256(raw)
			target := assistantConfigurationDecodeFixture{Name: "original"}
			if got := decodeAssistantConfigurationSnapshot(raw, hex.EncodeToString(sum[:]), &target); got != item.accepted || target.Name != item.expected {
				t.Fatalf("configuration decoder boundary changed: accepted=%t name=%q", got, target.Name)
			}
		})
	}
}
