package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestAtlassianServerContextRequiresExplicitBoundedPath rejects traversal, authority and query injection before credentials.
// TestAtlassianServerContextRequiresExplicitBoundedPath отклоняет traversal, authority и query injection до credentials.
func TestAtlassianServerContextRequiresExplicitBoundedPath(t *testing.T) {
	for input, want := range map[string]string{"/": "", "/tracker": "/tracker", "/docs/v1": "/docs/v1"} {
		got, err := atlassianServerContext(map[string]string{"context_path": input})
		if err != nil || got != want {
			t.Fatalf("accepted path = %q, %v", got, err)
		}
	}
	for _, input := range []string{"", "//other.example.invalid", "/..", "/a/../b", "/a/./b", "/a/", "/a?x=1", "/a#x", "/a%2fb", "/a\\b", "/a\nb"} {
		if _, err := atlassianServerContext(map[string]string{"context_path": input}); err == nil {
			t.Fatalf("unsafe path admitted: %q", input)
		}
	}
}

// TestAtlassianServerOffsetKeepsPaginationBounded verifies canonical offsets and rejects contradictory or truncated pages.
// TestAtlassianServerOffsetKeepsPaginationBounded проверяет canonical offsets и отклоняет противоречивые или усечённые страницы.
func TestAtlassianServerOffsetKeepsPaginationBounded(t *testing.T) {
	for _, input := range []string{"", "0", "5", "10000"} {
		if _, err := atlassianServerOffset(input); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{"-1", "+1", "00", "10001", "opaque-cloud-cursor", "1?scope=other"} {
		if _, err := atlassianServerOffset(input); err == nil {
			t.Fatalf("invalid cursor admitted: %q", input)
		}
	}
	for _, page := range [][4]int{{0, 0, 20, 0}, {5, 1, 20, 6}, {7, 0, 20, 5}} {
		if next, err := atlassianServerNext(page[0], page[1], page[2], page[3]); err != nil || next != "" {
			t.Fatalf("terminal page = %q, %v", next, err)
		}
	}
	if next, err := atlassianServerNext(2, 1, 2, 4); err != nil || next != "3" {
		t.Fatalf("next page = %q, %v", next, err)
	}
	for _, page := range [][4]int{{0, 0, 20, 1}, {0, 2, 1, 2}, {2, 2, 2, 3}, {10000, 1, 1, 10002}} {
		if _, err := atlassianServerNext(page[0], page[1], page[2], page[3]); err == nil {
			t.Fatalf("invalid page admitted: %v", page)
		}
	}
}

// serverDefinitionFixture reads the authored typed package, without publishing it or constructing live owner authority.
// serverDefinitionFixture читает typed package, не публикуя его и не создавая live owner authority.
func serverDefinitionFixture(t *testing.T, key string) integrationpackage.Package {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "integrations", "v1", "definitions", key+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

// TestAtlassianServerPackagesKeepContextInScope requires explicit new definitions and rejects Cloud configuration substitution.
// TestAtlassianServerPackagesKeepContextInScope требует отдельные definitions и отклоняет подмену Cloud-конфигурацией.
func TestAtlassianServerPackagesKeepContextInScope(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		definition := serverDefinitionFixture(t, key)
		configuration := map[string]string{"base_url": "https://api.example.invalid", "context_path": "/", "auth_scheme": "BEARER"}
		if key == jiraServerDefinition {
			configuration["project_key"], configuration["issue_type"] = "DEMO", "Task"
		} else {
			configuration["space_key"] = "DEMO"
		}
		if err := definition.ValidateConfiguration(configuration); err != nil {
			t.Fatal(err)
		}
		for _, capability := range definition.Spec.Capabilities {
			scope, err := capability.ResourceScopeValues(configuration)
			if err != nil || scope["context_path"] != "/" {
				t.Fatalf("context omitted from scope: %v, %v", scope, err)
			}
		}
		delete(configuration, "context_path")
		if err := definition.ValidateConfiguration(configuration); err == nil {
			t.Fatal("implicit context admitted")
		}
	}
}
