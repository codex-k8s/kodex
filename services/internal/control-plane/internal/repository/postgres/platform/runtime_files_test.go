package platform

import (
	"testing"
	"unicode/utf8"
)

func TestExecutionPreviewPreservesInvalidUTF8AndBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		input []byte
		want  string
		valid bool
	}{
		{"complete", []byte("раз"), "раз", true},
		{"split rune", []byte{'a', 0xd1}, "a", true},
		{"split long rune", []byte{'a', 0xf0, 0x9f, 0x92}, "a", true},
		{"invalid suffix", []byte{'a', 0xff}, string([]byte{'a', 0xff}), false},
		{"invalid internal", []byte{0xff, 'a', 0xd1}, string([]byte{0xff, 'a', 0xd1}), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := executionPreviewPrefix(test.input)
			if string(got) != test.want || utf8.Valid(got) != test.valid {
				t.Fatal("preview changed invalid bytes or split UTF-8 incorrectly")
			}
		})
	}
	for _, media := range []string{"text/plain; charset=utf-8", "text/markdown", "application/json", "application/yaml"} {
		if !executionPreviewMediaType(media) {
			t.Fatalf("supported preview media rejected: %s", media)
		}
	}
	for _, media := range []string{"application/octet-stream", "audio/mpeg", "image/svg+xml", "text/plain; malformed"} {
		if executionPreviewMediaType(media) {
			t.Fatalf("unsupported preview media accepted: %s", media)
		}
	}
}
