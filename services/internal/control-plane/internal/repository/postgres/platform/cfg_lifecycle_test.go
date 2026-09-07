package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"slices"
	"testing"
)

func TestCFGActionAvailabilityDoesNotGrantSourceOrReviveArchive(t *testing.T) {
	set := entity.ManagedConfigurationSet{Kind: "ROLE_IMAGE", ManagedBy: "UI", CurrentRevision: &entity.ManagedConfigurationRevision{Ref: "mrev_fixture"}}
	for _, test := range []struct {
		name                                     string
		manage, source, build, shipped, archived bool
		want                                     []string
	}{
		{"owner", true, true, true, false, false, []string{"COPY", "ARCHIVE"}},
		{"no-source-read", true, false, true, false, false, []string{"ARCHIVE"}},
		{"no-build", true, true, false, false, false, []string{"COPY"}},
		{"no-manage", false, true, true, false, false, []string{}},
		{"shipped", true, true, true, true, false, []string{"COPY"}},
		{"archived", true, true, true, false, true, []string{"COPY"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := set
			candidate.Archived = test.archived
			if got := cfgNextActions(candidate, test.manage, test.source, test.build, test.shipped); !slices.Equal(got, test.want) {
				t.Fatalf("actions %v", got)
			}
		})
	}
	set.ManagedBy = "GIT"
	if got := cfgNextActions(set, true, true, true, false); !slices.Equal(got, []string{"COPY"}) {
		t.Fatalf("Git archive availability: %v", got)
	}
}
