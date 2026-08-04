package controlplane

import (
	"strings"
	"testing"
)

func TestOwnerEligibilityPrecedesPageLimitInNamedSQL(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		statement string
		predicate string
	}{
		"list resources":   {statement: sqlResourceList, predicate: "owner_actor_id = @actor_id::uuid"},
		"search resources": {statement: sqlResourceSearch, predicate: "owner_actor_id = @actor_id::uuid"},
		"list incidents":   {statement: sqlRuntimeIncidentList, predicate: "process.owner_actor_id = @actor_id::uuid"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			predicate := strings.Index(test.statement, test.predicate)
			order := strings.Index(test.statement, "ORDER BY")
			limit := strings.Index(test.statement, "LIMIT")
			if predicate < 0 || order < 0 || limit < 0 || predicate > order || predicate > limit {
				t.Fatalf("authoritative owner predicate must be applied before ORDER BY/LIMIT in %s", name)
			}
		})
	}
}
