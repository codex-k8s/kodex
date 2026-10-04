package caster

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

func TestScopedMetadataClosedWireMapping(t *testing.T) {
	now := time.Now().UTC()
	for _, scope := range []runtimesecret.ScopeKind{runtimesecret.ScopeOrganization, runtimesecret.ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			project := ""
			if scope == runtimesecret.ScopeProject {
				project = "prj_fixture"
			}
			draft := value.SecretDraft{ScopeKind: scope, OrganizationRef: "org_fixture", ProjectRef: project, Ref: "draft_fixture", SecretRef: "sec_fixture", Version: 2, Generation: 1, SecretVersion: 1,
				ValueType: "STRING", State: "DRAFT", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
			secret := &value.PublishedSecret{ScopeKind: scope, OrganizationRef: "org_fixture", ProjectRef: project, Ref: "sec_fixture", Version: 2, Revision: 1, ValueType: "STRING", Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}
			wire, err := SecretDraft(draft)
			if err != nil || wire.GetScopeKind().String() != "RUNTIME_RESOURCE_SCOPE_KIND_"+string(scope) || wire.GetOrganizationRef() != draft.OrganizationRef || wire.GetProjectRef() != project {
				t.Fatal("draft caster changed scope")
			}
			published, err := PublishedSecret(secret)
			if err != nil || published.GetScopeKind() != wire.GetScopeKind() || published.GetOrganizationRef() != secret.OrganizationRef || published.GetProjectRef() != project {
				t.Fatal("published caster changed scope")
			}
			for _, invalid := range []runtimesecret.ScopeKind{"", "UNKNOWN", "SYSTEM", "project"} {
				draft.ScopeKind = invalid
				secret.ScopeKind = invalid
				if _, err := SecretDraft(draft); err == nil {
					t.Fatal("draft caster accepted unknown scope")
				}
				if _, err := PublishedSecret(secret); err == nil {
					t.Fatal("published caster accepted unknown scope")
				}
			}
		})
	}
}
