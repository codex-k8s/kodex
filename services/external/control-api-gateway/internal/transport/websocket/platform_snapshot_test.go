package websockettransport

import "testing"

func TestTypedPlatformSnapshotRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := typedPlatformSnapshot("WORKFLOW", map[string]any{
		"catalog": map[string]any{"workflows": []any{}, "unexpected": true},
	})
	if err == nil {
		t.Fatal("expected unknown snapshot field to be rejected")
	}
}

func TestTypedPlatformSnapshotBindsPayloadToResourceKind(t *testing.T) {
	t.Parallel()
	_, err := typedPlatformSnapshot("SYSTEM_ASSISTANT", map[string]any{
		"catalog": map[string]any{"projects": []any{}},
	})
	if err == nil {
		t.Fatal("expected mismatched snapshot kind to be rejected")
	}
}

func TestTypedPlatformSnapshotAcceptsClosedWorkflowCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("WORKFLOW", map[string]any{
		"catalog": map[string]any{
			"workflows": []any{},
			"page":      map[string]any{"nextPageToken": "cursor"},
		},
	})
	if err != nil {
		t.Fatalf("decode typed snapshot: %v", err)
	}
	if snapshot.Catalog == nil || snapshot.Catalog.Page == nil || snapshot.Catalog.Page.NextPageToken == nil || *snapshot.Catalog.Page.NextPageToken != "cursor" {
		t.Fatal("typed workflow cursor was not preserved")
	}
}

func TestTypedPlatformSnapshotAcceptsRuntimeSecretCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("RUNTIME_SECRET", map[string]any{
		"catalog": map[string]any{"secrets": []any{}, "page": map[string]any{}},
	})
	if err != nil || snapshot.Catalog == nil || snapshot.Catalog.Secrets == nil {
		t.Fatalf("decode runtime secret snapshot: %v", err)
	}
}

func TestTypedPlatformSnapshotAcceptsSeparateOrganizationCatalogs(t *testing.T) {
	for _, tc := range []struct{ kind, resources, cursor string }{
		{"ROLE_IMAGE_RECIPE", "organizationRecipes", "organizationRecipesPage"},
		{"RUNTIME_SECRET", "organizationSecrets", "organizationSecretsPage"},
	} {
		snapshot, err := typedPlatformSnapshot(tc.kind, map[string]any{"catalog": map[string]any{tc.resources: []any{}, tc.cursor: map[string]any{"nextPageToken": "org-cursor"}}})
		if err != nil || snapshot.Catalog == nil {
			t.Fatalf("closed organizational catalog schema rejected: %s %v", tc.kind, err)
		}
		if tc.kind == "ROLE_IMAGE_RECIPE" && (snapshot.Catalog.OrganizationRecipesPage == nil || *snapshot.Catalog.OrganizationRecipesPage.NextPageToken != "org-cursor") || tc.kind == "RUNTIME_SECRET" && (snapshot.Catalog.OrganizationSecretsPage == nil || *snapshot.Catalog.OrganizationSecretsPage.NextPageToken != "org-cursor") {
			t.Fatal("organizational cursor was merged with project cursor")
		}
	}
}

func TestTypedPlatformSnapshotRejectsPrivateFieldsInsideOrganizationCatalog(t *testing.T) {
	for _, fields := range []map[string]any{
		{"organizationSecrets": []any{map[string]any{"ref": "sec_fixture01", "scopeKind": "ORGANIZATION", "organizationRef": "org_fixture01", "value": "private-plaintext"}}},
		{"organizationRecipes": []any{map[string]any{"ref": "imgrec_fixture01", "scopeKind": "ORGANIZATION", "organizationRef": "org_fixture01", "unexpectedPrivate": "private-build-input"}}},
	} {
		if _, err := typedPlatformSnapshot("RUNTIME_SECRET", map[string]any{"catalog": fields}); err == nil {
			t.Fatal("undeclared/private organization payload escaped closed snapshot schema")
		}
	}
}

func TestTypedPlatformSnapshotAcceptsManagedConfigurationCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("MANAGED_CONFIGURATION", map[string]any{
		"catalog": map[string]any{
			"managedConfigurations": []any{
				map[string]any{
					"ref": "cfg_test", "version": 1, "kind": "SYSTEM_STT",
					"name": "Тестовая конфигурация", "managedBy": "UI",
					"source": "ui", "sourceRevision": "revision-1",
					"updatedAt": "2026-10-01T00:00:00Z", "archived": false,
					"nextActions": []any{},
				},
			},
			"managedConfigurationPages": []any{
				map[string]any{"kind": "SYSTEM_STT", "total": 1, "nextPageToken": "cursor-2"},
			},
		},
	})
	if err != nil {
		t.Fatalf("decode managed configuration snapshot: %v", err)
	}
	if snapshot.Catalog == nil || len(snapshot.Catalog.ManagedConfigurations) != 1 || len(snapshot.Catalog.ManagedConfigurationPages) != 1 {
		t.Fatal("managed configuration snapshot was not preserved")
	}
	if snapshot.Catalog.ManagedConfigurationPages[0].Kind != "SYSTEM_STT" {
		t.Fatal("managed configuration page kind was not preserved")
	}
}

func TestTypedPlatformSnapshotAcceptsProviderBootstrapCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("PROVIDER_ACCOUNT", map[string]any{
		"catalog": map[string]any{
			"accounts":                []any{},
			"providerDefinitions":     []any{},
			"providerDefinitionsPage": map[string]any{"nextPageToken": "definitions-2"},
			"runtimes":                []any{},
			"page":                    map[string]any{},
			"nextActions":             []any{},
		},
	})
	if err != nil || snapshot.Catalog == nil || snapshot.Catalog.ProviderDefinitions == nil || snapshot.Catalog.Runtimes == nil {
		t.Fatalf("decode provider bootstrap snapshot: %v", err)
	}
}

func TestTypedPlatformSnapshotAcceptsRuntimeEnvironmentBootstrapCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("RUNTIME_ENVIRONMENT", map[string]any{
		"catalog": map[string]any{
			"environments":     []any{},
			"roleEnvironments": []any{},
			"runtimes":         []any{},
			"page":             map[string]any{},
		},
	})
	if err != nil || snapshot.Catalog == nil || snapshot.Catalog.RoleEnvironments == nil {
		t.Fatalf("decode runtime environment bootstrap snapshot: %v", err)
	}
}

func TestTypedPlatformSnapshotAcceptsAccessBootstrapCatalog(t *testing.T) {
	t.Parallel()
	snapshot, err := typedPlatformSnapshot("MEMBERSHIP", map[string]any{
		"catalog": map[string]any{
			"memberships":        []any{},
			"permissions":        []any{},
			"accessSubjects":     []any{},
			"accessSubjectsPage": map[string]any{},
			"oidcGroups":         []any{},
			"oidcGroupsPage":     map[string]any{},
			"accessRoles":        []any{},
			"accessRolesPage":    map[string]any{},
			"accessBindings":     []any{},
			"accessBindingsPage": map[string]any{},
			"page":               map[string]any{},
			"nextActions":        []any{},
		},
	})
	if err != nil || snapshot.Catalog == nil || snapshot.Catalog.AccessRoles == nil || snapshot.Catalog.AccessBindings == nil {
		t.Fatalf("decode access bootstrap snapshot: %v", err)
	}
}
