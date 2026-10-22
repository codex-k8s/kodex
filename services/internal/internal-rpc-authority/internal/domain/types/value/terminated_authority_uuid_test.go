package value

import "testing"

// TestTerminatedAuthorityUUIDPreservesEncoding pins empty tuples versus empty
// parts, UTF-8, NUL ambiguity and client namespaces with independent SHA-256
// vectors. It also guards the caller's spare slice capacity against mutation;
// no credentials, network exchanges or replay claims are exercised here.
// TestTerminatedAuthorityUUIDPreservesEncoding закрепляет различие пустого
// кортежа и пустой части, UTF-8, неоднозначность NUL и пространства клиентов
// независимыми векторами SHA-256. Проверяется и неизменность запаса среза
// вызывающего; credentials, сетевые обмены и claims повторов не проверяются.
func TestTerminatedAuthorityUUIDPreservesEncoding(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		parts []string
		want  string
	}{
		{"no parts", nil, "e3b0c442-98fc-5c14-9afb-f4c8996fb924"},
		{"one empty part", []string{""}, "6e340b9c-ffb3-5a98-9ca5-44e6bb780a2c"},
		{"two parts", []string{"a", "b"}, "8fb20ef6-3ced-5145-bc2e-983ffe597d1d"},
		{"embedded NUL", []string{"a\x00b"}, "8fb20ef6-3ced-5145-bc2e-983ffe597d1d"},
		{"trailing empty part", []string{"a", ""}, "ea6fde9e-840d-540a-8f5d-f3d85b5ad618"},
		{"UTF8", []string{"имя", "роль"}, "d54e69d2-c723-51b2-a089-16c9dd60ea7d"},
		{"readback evidence", []string{"readback-evidence", "challenge-1"},
			"b9ce5e85-6b03-51cd-9af2-21a0957b4af1"},
		{"readback submission", []string{"readback-attestation", "challenge-1", "evidence-1"},
			"8fb73735-061e-5191-b553-d41e26cc82c7"},
		{"restore ACK", []string{"restore-ack", "directive-1", "digest-1"},
			"5adcf466-ec81-5f12-8cba-f26c21e9dfd2"},
		{"NUL part", []string{"\x00"}, "96a296d2-24f2-55c6-bbee-93c30f8a3091"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := TerminatedAuthorityUUID(testCase.parts...); got != testCase.want {
				t.Fatalf("terminated authority ID = %q, want %q", got, testCase.want)
			}
		})
	}
	parts := [3]string{"a", "b", "sentinel"}
	if got := TerminatedAuthorityUUID(parts[:2]...); got != "8fb20ef6-3ced-5145-bc2e-983ffe597d1d" {
		t.Fatalf("spare-capacity authority ID = %q", got)
	}
	if parts != [3]string{"a", "b", "sentinel"} {
		t.Fatalf("identifier derivation changed caller input")
	}
}
