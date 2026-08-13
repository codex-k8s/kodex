package controlplane

import (
	"strings"
	"testing"
)

func TestReceiptSaveConvertsOptionalJSONNullToSQLNull(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"result", "payload"} {
		expected := "nullif(@" + field + ", 'null')::jsonb"
		if !strings.Contains(sqlReceiptSave, expected) {
			t.Fatalf("receipt save SQL does not normalize optional %s to SQL NULL", field)
		}
	}
}
