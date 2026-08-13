package readback

import "testing"

func TestNewRequestUUIDReturnsDistinctVersionFourIdentifiers(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 32)
	for range 32 {
		value, err := newRequestUUID()
		if err != nil {
			t.Fatalf("new request UUID: %v", err)
		}
		if len(value) != 36 || value[8] != '-' || value[13] != '-' ||
			value[14] != '4' || value[18] != '-' || value[23] != '-' ||
			(value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
			t.Fatalf("request UUID has invalid RFC 4122 version 4 shape: %q", value)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("request UUID repeated: %q", value)
		}
		seen[value] = struct{}{}
	}
}
