package opaqueref

import (
	"strings"
	"testing"
)

// TestValidPreservesReferenceBounds covers exact prefix matching, minimum suffix and maximum total
// byte length, invalid content, and the existing behavior of treating the supplied prefix literally.
// No normalization, resource lookup, or stronger prefix policy is introduced by the shared helper.
// TestValidPreservesReferenceBounds проверяет точное совпадение префикса, минимум суффикса и максимум
// общей длины в байтах, недопустимое содержимое и прежнюю буквальную трактовку переданного префикса.
// Общий помощник не добавляет нормализацию, поиск ресурса или усиленную политику префикса.
func TestValidPreservesReferenceBounds(t *testing.T) {
	for _, fixture := range []struct {
		name, reference, prefix string
		want                    bool
	}{
		{"minimum", "prj_abcdefgh", "prj", true},
		{"short", "prj_abcdefg", "prj", false},
		{"maximum", "prj_" + strings.Repeat("a", 92), "prj", true},
		{"over maximum", "prj_" + strings.Repeat("a", 93), "prj", false},
		{"mixed alphabet", "prj_aZ09-_ab", "prj", true},
		{"wrong prefix", "role_abcdefgh", "prj", false},
		{"prefix case", "PRJ_abcdefgh", "prj", false},
		{"separator", "prj-abcdefgh", "prj", false},
		{"empty", "", "prj", false},
		{"space", "prj_abc defgh", "prj", false},
		{"newline", "prj_abcdefgh\n", "prj", false},
		{"nul", "prj_abcdefgh\x00", "prj", false},
		{"unicode suffix", "prj_abcdefgж", "prj", false},
		{"invalid utf8", "prj_abcdefgh\xff", "prj", false},
		{"literal empty prefix", "_abcdefgh", "", true},
		{"literal unicode prefix", "п_abcdefgh", "п", true},
		{"literal underscore prefix", "x__abcdefgh", "x_", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if got := Valid(fixture.reference, fixture.prefix); got != fixture.want {
				t.Fatalf("reference validity: got %v, want %v", got, fixture.want)
			}
		})
	}
}

// TestValidRestrictsEverySuffixByteToASCII exercises all byte values inside a sufficiently long
// suffix, using an explicit accepted alphabet rather than copying the production range predicate.
// TestValidRestrictsEverySuffixByteToASCII проверяет все значения байта внутри достаточно длинного
// суффикса с явным допустимым алфавитом вместо копирования производственных сравнений диапазонов.
func TestValidRestrictsEverySuffixByteToASCII(t *testing.T) {
	for code := 0; code < 256; code++ {
		reference := "prj_abcdefgh" + string([]byte{byte(code)})
		want := strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", byte(code)) >= 0
		if got := Valid(reference, "prj"); got != want {
			t.Fatalf("suffix byte %d: got %v, want %v", code, got, want)
		}
	}
}
