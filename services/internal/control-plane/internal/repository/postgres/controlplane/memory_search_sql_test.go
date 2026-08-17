package controlplane

import (
	"strings"
	"testing"
)

func TestMemorySearchTreatsEmptyUUIDFiltersAsAbsent(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"parent_id", "after_id"} {
		guard := "NULLIF(@" + argument + "::text, '')"
		if !strings.Contains(sqlMemorySearch, guard+" IS NULL") ||
			!strings.Contains(sqlMemorySearch, guard+"::uuid") {
			t.Fatalf("memory search does not guard empty %s before UUID cast", argument)
		}
		if strings.Contains(sqlMemorySearch, "@"+argument+"::uuid") {
			t.Fatalf("memory search contains an unguarded %s UUID cast", argument)
		}
	}
}
