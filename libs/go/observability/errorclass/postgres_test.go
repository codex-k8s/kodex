package errorclass

import (
	"strings"
	"testing"
)

// TestPostgresClosedClasses pins every supported SQLSTATE mapping and requires arbitrary/private-looking input to collapse to one fixed fallback.
// TestPostgresClosedClasses закрепляет каждое поддержанное соответствие SQLSTATE и требует свести произвольный либо похожий на закрытые данные вход к одному fallback.
func TestPostgresClosedClasses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ code, class string }{
		{"28P01", "database_authentication"},
		{"28000", "database_authentication"},
		{"42501", "database_permission"},
		{"3D000", "database_missing"},
		{"53300", "database_unavailable"},
		{"57P03", "database_unavailable"},
		{"", "database"},
		{"42P01", "database"},
		{"28p01", "database"},
		{" 28P01 ", "database"},
		{"SENTINEL_PRIVATE_DETAIL", "database"},
		{strings.Repeat("x", 65536), "database"},
	} {
		if got := Postgres(test.code); got != test.class {
			t.Fatal("unexpected PostgreSQL diagnostic class")
		}
	}
}
