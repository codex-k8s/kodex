package draftowner

import (
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
)

func TestOwnerScopedDraftReadbackClosed(t *testing.T) {
	for _, scope := range []cp.RuntimeResourceScopeKind{cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION} {
		t.Run(scope.String(), func(t *testing.T) {
			_, stub, owner := nativeFixture(t)
			stub.work.Draft.ScopeKind = scope
			if scope == cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION {
				stub.work.Draft.ProjectRef = ""
			}
			work, err := owner.Consume(t.Context(), "synthetic-grant")
			if err != nil {
				t.Fatal(err)
			}
			if string(work.Binding.ScopeKind) != "ORGANIZATION" && string(work.Binding.ScopeKind) != "PROJECT" || work.Binding.OrganizationRef != stub.work.Draft.OrganizationRef || work.Binding.ProjectRef != stub.work.Draft.ProjectRef {
				t.Fatal("owner scope lost before encryption")
			}
			encrypted := encryptedFixture(work)
			stub.draft = draftResult(stub, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT)
			if _, err := owner.Complete(t.Context(), work, &encrypted, nil); err != nil {
				t.Fatal(err)
			}
			for name, mutate := range map[string]func(*cp.RuntimeSecretDraft){
				"unknown scope":        func(d *cp.RuntimeSecretDraft) { d.ScopeKind = cp.RuntimeResourceScopeKind(99) },
				"empty organization":   func(d *cp.RuntimeSecretDraft) { d.OrganizationRef = "" },
				"foreign organization": func(d *cp.RuntimeSecretDraft) { d.OrganizationRef = "org_foreign" },
				"cross scope": func(d *cp.RuntimeSecretDraft) {
					if d.ProjectRef == "" {
						d.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
						d.ProjectRef = "project_fixture"
					} else {
						d.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
						d.ProjectRef = ""
					}
				},
			} {
				t.Run(name, func(t *testing.T) {
					stub.draft = draftResult(stub, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT)
					mutate(stub.draft)
					if _, err := owner.Complete(t.Context(), work, &encrypted, nil); !errors.Is(err, secretdrafts.ErrConflict) {
						t.Fatal("foreign scoped completion returned a result")
					}
					if _, err := owner.Recover(t.Context(), work, &encrypted, nil); !errors.Is(err, secretdrafts.ErrConflict) {
						t.Fatal("foreign scoped recovery returned cleanup authority")
					}
				})
			}
			stub.work.Draft.ScopeKind = cp.RuntimeResourceScopeKind(99)
			if _, err := owner.Consume(t.Context(), "synthetic-grant"); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("unknown owner scope reached native work")
			}
		})
	}
}
