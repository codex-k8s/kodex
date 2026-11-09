package integrationpackage

import "testing"

// TestGitLabResourceScopeSelection checks explicit repository, group and instance choices without deriving provider permissions from input text.
// TestGitLabResourceScopeSelection проверяет выбор repository/group/instance без вывода provider permissions из входного текста.
func TestGitLabResourceScopeSelection(t *testing.T) {
	cases := []struct {
		name          string
		configuration map[string]string
		project       string
		want          bool
	}{
		{"legacy project", map[string]string{"project_path": "team/alpha"}, "team/alpha", true},
		{"project defaults selection", map[string]string{"project_path": "team/alpha"}, "", true},
		{"different project", map[string]string{"project_path": "team/alpha"}, "team/bravo", false},
		{"group requires selection", map[string]string{"scope_kind": "GROUP", "group_path": "team/services"}, "", false},
		{"group selected path", map[string]string{"scope_kind": "GROUP", "group_path": "team/services"}, "team/services/alpha", true},
		{"group rejects outside selector", map[string]string{"scope_kind": "GROUP", "group_path": "team/services"}, "other/alpha", false},
		{"group rejects misleading prefix", map[string]string{"scope_kind": "GROUP", "group_path": "team/services"}, "team/services-other/alpha", false},
		{"group rejects unselected subgroup", map[string]string{"scope_kind": "GROUP", "group_path": "team/services"}, "team/services/nested/alpha", false},
		{"group explicitly includes subgroup", map[string]string{"scope_kind": "GROUP", "group_path": "team/services", "include_subgroups": "true"}, "team/services/nested/alpha", true},
		{"instance selected path", map[string]string{"scope_kind": "INSTANCE"}, "other/alpha", true},
		{"instance requires selection", map[string]string{"scope_kind": "INSTANCE"}, "", false},
		{"encoded selector", map[string]string{"scope_kind": "INSTANCE"}, "team%2Falpha", false},
		{"traversal selector", map[string]string{"scope_kind": "INSTANCE"}, "team/../alpha", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scope, err := ParseGitLabResourceScope(c.configuration)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := scope.SelectProject(c.project)
			if (err == nil) != c.want {
				t.Fatalf("selection=%q error=%v, want allowed=%v", selected, err, c.want)
			}
			if c.want && selected == "" {
				t.Fatal("accepted an empty project")
			}
		})
	}
}

// TestGitLabResourceScopeProviderMembership requires canonical provider namespace metadata and segment boundaries for every scoped repository.
// TestGitLabResourceScopeProviderMembership требует канонические provider namespace metadata и границы сегментов для каждого repository.
func TestGitLabResourceScopeProviderMembership(t *testing.T) {
	for _, nested := range []bool{false, true} {
		scope, err := ParseGitLabResourceScope(map[string]string{"scope_kind": "GROUP", "group_path": "team/services", "include_subgroups": map[bool]string{false: "false", true: "true"}[nested]})
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			project, namespace string
			allowed            bool
		}{
			{"team/services/alpha", "team/services", true},
			{"team/services/nested/bravo", "team/services/nested", nested},
			{"team/services-other/alpha", "team/services-other", false},
			{"other/shared", "other", false},
			{"other/shared", "team/services", false},
			{"team/services/alpha", "team/services/../outside", false},
			{"team/services/alpha", "", false},
		}
		for _, c := range cases {
			if got := scope.AllowsProviderProject(c.project, c.namespace); got != c.allowed {
				t.Fatalf("nested=%v project=%q namespace=%q got=%v", nested, c.project, c.namespace, got)
			}
		}
	}
	exact, err := ParseGitLabResourceScope(map[string]string{"project_path": "team/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !exact.AllowsProviderProject("team/alpha", "team") || exact.AllowsProviderProject("team/bravo", "team") {
		t.Fatal("project scope changed")
	}
	all, err := ParseGitLabResourceScope(map[string]string{"scope_kind": "INSTANCE"})
	if err != nil {
		t.Fatal(err)
	}
	if !all.AllowsProviderProject("other/alpha", "other") || all.AllowsProviderProject("other/alpha", "foreign") {
		t.Fatal("instance bypassed canonical namespace identity")
	}
}

// TestGitLabResourceScopeRejectsAmbiguousConfiguration ensures unknown modes and mixed selectors cannot silently widen legacy access.
// TestGitLabResourceScopeRejectsAmbiguousConfiguration запрещает неизвестные режимы и смешанные selectors, которые могли бы расширить legacy-доступ.
func TestGitLabResourceScopeRejectsAmbiguousConfiguration(t *testing.T) {
	cases := []map[string]string{
		{}, {"scope_kind": "UNKNOWN", "project_path": "team/alpha"},
		{"scope_kind": "PROJECT"}, {"scope_kind": "PROJECT", "project_path": "team/alpha", "group_path": "team"},
		{"scope_kind": "PROJECT", "project_path": "team/alpha", "include_subgroups": "true"},
		{"scope_kind": "GROUP"}, {"scope_kind": "GROUP", "group_path": "team", "project_path": "team/alpha"},
		{"scope_kind": "GROUP", "group_path": "team", "include_subgroups": "yes"},
		{"scope_kind": "INSTANCE", "project_path": "team/alpha"}, {"scope_kind": "INSTANCE", "group_path": "team"},
		{"scope_kind": "INSTANCE", "include_subgroups": "true"},
		{"scope_kind": "GROUP", "group_path": "team//services"},
		{"scope_kind": "GROUP", "group_path": "https://example.invalid/team"},
	}
	for _, c := range cases {
		if _, err := ParseGitLabResourceScope(c); err == nil {
			t.Fatalf("ambiguous configuration admitted: %v", c)
		}
	}
}
