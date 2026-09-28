package platform

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func TestListProjectsQueryDoesNotMixNamedAndPositionalArguments(t *testing.T) {
	if regexp.MustCompile(`\$[0-9]+`).MatchString(queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId) {
		t.Fatal("project list query mixes named and positional arguments")
	}
}

func TestArtifactCatalogCursorKeepsTimeAndAuthorityScope(t *testing.T) {
	current := scope{organizationID: "organization-a", actorID: "actor-a"}
	filter := query.Filter{ProjectRef: "project-a", State: "ACTIVE"}
	position := encodeArtifactCursor(time.Date(2026, time.September, 27, 2, 0, 0, 123, time.UTC), "art_12345678")
	filter.Page.Token = encodeCatalogCursor(current, "ARTIFACT", filter, position)
	decoded, err := decodeCatalogCursor(current, "ARTIFACT", filter)
	if err != nil || decoded != position {
		t.Fatalf("artifact catalog position was lost: err=%v", err)
	}
	if _, _, err := decodeArtifactCursor(decoded); err != nil {
		t.Fatalf("artifact catalog position is not a valid time/ref cursor: %v", err)
	}
	changed := filter
	changed.ProjectRef = "project-b"
	if _, err := decodeCatalogCursor(current, "ARTIFACT", changed); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("artifact cursor crossed the project boundary")
	}
}

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
