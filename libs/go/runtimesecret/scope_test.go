package runtimesecret

import (
	"strings"
	"testing"
)

func TestValidateScopeRequiresClosedOwnerTuple(t *testing.T) {
	for _, kind := range []ScopeKind{ScopeOrganization, ScopeProject} {
		project := ""
		if kind == ScopeProject {
			project = "prj_fixture"
		}
		if ValidateScope(kind, "org_fixture", project) != nil {
			t.Fatal("canonical resource owner tuple rejected")
		}
	}
	for _, test := range []struct {
		kind         ScopeKind
		org, project string
	}{
		{"", "org_fixture", ""}, {"UNKNOWN", "org_fixture", "prj_fixture"},
		{ScopeOrganization, "", ""}, {ScopeOrganization, "org_fixture", "prj_fixture"},
		{ScopeProject, "org_fixture", ""}, {ScopeProject, "", "prj_fixture"},
		{ScopeOrganization, "org_fixture\n", ""}, {ScopeProject, "org_fixture", "../project"},
		{ScopeOrganization, strings.Repeat("a", 129), ""},
	} {
		if ValidateScope(test.kind, test.org, test.project) == nil {
			t.Fatal("invalid resource scope tuple accepted")
		}
	}
}
