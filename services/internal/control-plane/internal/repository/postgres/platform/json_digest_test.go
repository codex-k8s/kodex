package platform

import "testing"

// TestJSONValueDigestPreservesHistoricalBytes checks literal goldens for nil/empty distinctions,
// sorted object keys, HTML escaping and the historical unsupported-value fallback through both
// the shared primitive and the existing role-image caller.
// TestJSONValueDigestPreservesHistoricalBytes проверяет буквальные goldens для nil/empty,
// сортировки ключей, HTML escaping и исторического fallback неподдерживаемого значения через
// общий примитив и существующий role-image caller.
func TestJSONValueDigestPreservesHistoricalBytes(t *testing.T) {
	for _, test := range []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b"},
		{"nil slice", []string(nil), "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b"},
		{"empty slice", []string{}, "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"},
		{"object", map[string]string{"b": "<>&", "a": "quoted\""}, "fa8880014e5468bd4b057f144bb2ac9c6b2c4e7faebaf8f0924a68c17150df86"},
		{"unsupported", make(chan int), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if jsonValueDigest(test.input) != test.want || roleImageDigest(test.input) != test.want {
				t.Fatal("JSON commitment bytes or historical marshal-error fallback changed")
			}
		})
	}
}
