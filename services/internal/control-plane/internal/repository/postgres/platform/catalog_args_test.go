package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// TestScopedCatalogPageArgs verifies exact SQL keys, authority fields, query trimming, cursor,
// limit and independent maps. It does not evaluate SQL eligibility or grant access.
// TestScopedCatalogPageArgs проверяет точные SQL keys, поля authority, обрезку query, cursor,
// limit и независимость map. Не выполняет SQL eligibility и не выдаёт доступ.
func TestScopedCatalogPageArgs(t *testing.T) {
	current := scope{actorID: "actor", organizationID: "organization", authorityProjectID: "authority-project"}
	filter := query.Filter{ProjectRef: "project", Query: "  поиск  "}
	want := pgx.StrictNamedArgs{"actor_id": "actor", "organization_id": "organization", "authority_project": "authority-project", "project_ref": "project", "query": "поиск", "cursor_ref": "cursor", "page_size": int32(23)}
	first := scopedCatalogPageArgs(current, filter, "cursor", 23)
	second := scopedCatalogPageArgs(current, filter, "cursor", 23)
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatal("scoped page arguments changed")
	}
	first["actor_id"] = "changed"
	if !reflect.DeepEqual(second, want) {
		t.Fatal("scoped page argument maps are shared")
	}
}
