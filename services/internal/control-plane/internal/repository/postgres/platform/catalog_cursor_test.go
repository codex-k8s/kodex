package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func TestCatalogCursorBindsTenantActorKindAndFilter(t *testing.T) {
	current := scope{organizationID: "organization-a", actorID: "actor-a"}
	filter := query.Filter{ProjectRef: "project-a", Query: "needle", State: "READY", Page: query.Page{Size: 20}}
	filter.Page.Token = encodeCatalogCursor(current, "AGENT", filter, "agt_last")
	if ref, err := decodeCatalogCursor(current, "AGENT", filter); err != nil || ref != "agt_last" {
		t.Fatalf("cursor round trip: ref=%q err=%v", ref, err)
	}
	for _, field := range []string{"tenant", "actor", "kind", "project", "query", "state", "malformed", "oversize"} {
		t.Run(field, func(t *testing.T) {
			changed, changedFilter, kind := current, filter, "AGENT"
			switch field {
			case "tenant":
				changed.organizationID = "organization-b"
			case "actor":
				changed.actorID = "actor-b"
			case "kind":
				kind = "WORKFLOW"
			case "project":
				changedFilter.ProjectRef = ""
			case "query":
				changedFilter.Query = "other"
			case "state":
				changedFilter.State = "DRAFT"
			case "malformed":
				changedFilter.Page.Token = "invalid!"
			case "oversize":
				changedFilter.Page.Token = strings.Repeat("a", 513)
			}
			if _, err := decodeCatalogCursor(changed, kind, changedFilter); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("mismatched cursor accepted: %v", err)
			}
		})
	}
	filter.Page.Size = 50
	if _, err := decodeCatalogCursor(current, "AGENT", filter); err != nil {
		t.Fatalf("page size is not a filter: %v", err)
	}
}
