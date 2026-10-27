package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateKeepsDatabaseCharacterLimit checks trimming and ellipsis respect character limits
// for ASCII/Unicode, including zero and marker-only boundaries.
// TestTruncateKeepsDatabaseCharacterLimit проверяет соблюдение ограничения символов после удаления
// краевых пробелов и добавления многоточия для ASCII и Unicode, включая нулевую границу и место
// только для маркера.
func TestTruncateKeepsDatabaseCharacterLimit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		value   string
		maximum int
		want    string
	}{
		{name: "unchanged", value: "  готово  ", maximum: 10, want: "готово"},
		{name: "ascii", value: strings.Repeat("a", 2001), maximum: 2000, want: strings.Repeat("a", 1999) + "…"},
		{name: "unicode", value: "абвг", maximum: 3, want: "аб…"},
		{name: "marker only", value: "long", maximum: 1, want: "…"},
		{name: "empty boundary", value: "long", maximum: 0, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual := truncate(test.value, test.maximum)
			if actual != test.want {
				t.Fatalf("truncate() = %q, want %q", actual, test.want)
			}
			if utf8.RuneCountInString(actual) > test.maximum && test.maximum >= 0 {
				t.Fatalf("truncate() returned %d characters for maximum %d", utf8.RuneCountInString(actual), test.maximum)
			}
		})
	}
}

// TestBoundedRunTitleUsesUTF8ByteLimit checks titles stay valid UTF-8 within240 bytes, preserve
// already bounded input and mark omitted content with an ellipsis.
// TestBoundedRunTitleUsesUTF8ByteLimit проверяет корректный UTF-8 заголовков в пределах240 байт,
// сохранение уже допустимого входа и обозначение пропущенного текста многоточием.
func TestBoundedRunTitleUsesUTF8ByteLimit(t *testing.T) {
	t.Parallel()
	for _, title := range []string{
		"Короткое название",
		"Процесс: " + strings.Repeat("Проверка окружения ", 12),
		strings.Repeat("a", 241),
	} {
		actual := boundedRunTitle(title)
		if len(actual) > 240 || !utf8.ValidString(actual) {
			t.Fatalf("run title is not bounded UTF-8: bytes=%d", len(actual))
		}
		if len(title) <= 240 && actual != title {
			t.Fatal("bounded run title changed an already valid title")
		}
		if len(title) > 240 && !strings.HasSuffix(actual, "…") {
			t.Fatal("truncated run title lost the omission marker")
		}
	}
}
