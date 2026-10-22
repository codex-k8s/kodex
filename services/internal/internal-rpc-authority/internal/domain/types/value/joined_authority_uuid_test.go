package value

import "testing"

// TestJoinedAuthorityUUIDPreservesEncoding pins empty, separator, UTF-8 and
// real namespace vectors. It also records deliberate NUL-join equivalence,
// preventing an encoding change disguised as a duplicate-code extraction.
// TestJoinedAuthorityUUIDPreservesEncoding закрепляет пустой ввод, разделитель,
// UTF-8 и настоящее пространство имён. Проверяется и намеренная эквивалентность
// NUL-соединения, чтобы вынос дубликатов не скрывал изменение кодирования.
func TestJoinedAuthorityUUIDPreservesEncoding(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		parts []string
		want  string
	}{
		{"empty tuple", nil, "e3b0c442-98fc-5c14-9afb-f4c8996fb924"},
		{"empty part", []string{""}, "e3b0c442-98fc-5c14-9afb-f4c8996fb924"},
		{"two parts", []string{"a", "b"}, "59b271ae-1bbc-51d3-9d41-929817f4b16f"},
		{"embedded separator", []string{"a\x00b"}, "59b271ae-1bbc-51d3-9d41-929817f4b16f"},
		{"trailing empty part", []string{"a", ""}, "ffe9aaea-a2a2-5504-8174-df0b80599ef0"},
		{"UTF8", []string{"имя", "роль"}, "5e7c8587-b0e1-51b8-90e7-7d5668e647fe"},
		{"rotation namespace", []string{"authority-normal-rotation-v3", "8",
			"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
			"13657d5a-4e35-52fc-b82d-0529ee0914ca"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := JoinedAuthorityUUID(testCase.parts...); got != testCase.want {
				t.Fatalf("authority ID = %q, want %q", got, testCase.want)
			}
		})
	}
}
