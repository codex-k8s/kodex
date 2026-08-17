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

func TestMemorySearchTreatsEmptyEmbeddingAsAbsentTextParameter(t *testing.T) {
	t.Parallel()

	guard := "NULLIF(@query_embedding::text, '')"
	if !strings.Contains(sqlMemorySearch, guard+" IS NULL") ||
		!strings.Contains(sqlMemorySearch, guard+"::public.vector") {
		t.Fatal("memory search does not guard an empty embedding before the vector cast")
	}
	if strings.Contains(sqlMemorySearch, "@query_embedding::public.vector") ||
		strings.Contains(sqlMemorySearch, "@query_embedding = ''") {
		t.Fatal("memory search lets PostgreSQL infer the empty embedding parameter as vector")
	}
}
