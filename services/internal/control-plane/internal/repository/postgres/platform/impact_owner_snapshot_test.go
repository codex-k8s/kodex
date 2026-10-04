package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

func TestImpactOwnerSnapshotClosed(t *testing.T) {
	for _, test := range []struct {
		name, scope, organization, project, expectedScope, expectedOrganization, expectedProject string
		want                                                                                     error
	}{
		{"organization", "ORGANIZATION", "org_current", "", "ORGANIZATION", "org_current", "", nil},
		{"project", "PROJECT", "org_current", "prj_current", "PROJECT", "org_current", "prj_current", nil},
		{"missing-scope", "ORGANIZATION", "org_current", "", "", "org_current", "", errs.ErrInvalid},
		{"missing-organization", "ORGANIZATION", "org_current", "", "ORGANIZATION", "", "", errs.ErrInvalid},
		{"unknown-scope", "ORGANIZATION", "org_current", "", "UNKNOWN", "org_current", "", errs.ErrInvalid},
		{"organization-project-alias", "ORGANIZATION", "org_current", "", "ORGANIZATION", "org_current", "prj_current", errs.ErrInvalid},
		{"project-missing-locator", "PROJECT", "org_current", "prj_current", "PROJECT", "org_current", "", errs.ErrInvalid},
		{"foreign-organization", "ORGANIZATION", "org_current", "", "ORGANIZATION", "org_foreign", "", errs.ErrNotFound},
		{"foreign-project", "PROJECT", "org_current", "prj_current", "PROJECT", "org_current", "prj_foreign", errs.ErrNotFound},
		{"cross-scope", "ORGANIZATION", "org_current", "", "PROJECT", "org_current", "prj_current", errs.ErrNotFound},
		{"unknown-persisted-scope", "", "org_current", "", "ORGANIZATION", "org_current", "", errs.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := matchRuntimeOwnerSnapshot(test.scope, test.organization, test.project, test.expectedScope, test.expectedOrganization, test.expectedProject); !errors.Is(err, test.want) {
				t.Fatalf("owner snapshot boundary: %v", err)
			}
		})
	}
}
