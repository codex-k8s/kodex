package resource

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func TestCopyAccessResourceStartsPausedAndPreservesLineage(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	organizationID, projectID, sourceOwnerID, copyOwnerID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	credentialID, promptID, roleID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	git := entity.ConfigurationOwnership{ManagedBy: "GIT", SourceRef: "git://configuration/access", SourceRevision: 11}
	specs := []entity.Spec{
		entity.TeamSpec{StableKey: "team", ExternalTeamRef: "team/source", RoleIDs: []string{roleID}, Ownership: git},
		entity.RoleSpec{
			StableKey: "role", Capabilities: []string{"control.read"}, PromptProfileID: promptID,
			ProviderCredentialBindingIDs: []string{credentialID}, Ownership: git,
			ProviderAccountPool: entity.ProviderAccountPool{
				Policy: "least_used", PolicyRevision: 1, ObservationMaxAge: time.Minute,
				Bindings: []entity.ProviderAccountPoolBinding{{CredentialBindingID: credentialID, Weight: 1}},
			},
		},
		entity.PromptProfileSpec{Revision: 1, ContentSHA256: strings.Repeat("a", 64), SourceRef: "git://prompt/source", Locale: "ru", Ownership: git},
	}
	for _, spec := range specs {
		t.Run(string(spec.Kind()), func(t *testing.T) {
			source, err := entity.New(uuid.NewString(), organizationID, projectID, "", sourceOwnerID, spec.Kind(), "source", spec, now)
			if err != nil {
				t.Fatalf("source: %v", err)
			}
			source.Version = 7
			copied, err := copyAccessResource(source, copyOwnerID, "copy", now.Add(time.Second))
			if err != nil {
				t.Fatalf("copy: %v", err)
			}
			ownership := copied.Spec.(entity.ConfiguredSpec).ConfigurationOwnership()
			if copied.ID == source.ID || copied.OwnerActorID != copyOwnerID || copied.OrganizationID != organizationID || copied.ProjectID != projectID ||
				copied.State != enum.StatePaused || copied.Version != 1 || ownership.ManagedBy != "UI" ||
				ownership.SourceRef != source.ID || ownership.SourceRevision != source.Version {
				t.Fatalf("copy boundary mismatch: copied=%#v ownership=%#v", copied, ownership)
			}
		})
	}
}

func TestUIUpdatePreservesServerOwnedCopyLineage(t *testing.T) {
	sourceID := uuid.NewString()
	current := entity.PromptProfileSpec{Revision: 1, ContentSHA256: strings.Repeat("a", 64), SourceRef: "ui://prompt/current", Locale: "ru", Ownership: entity.ConfigurationOwnership{ManagedBy: "UI", SourceRef: sourceID, SourceRevision: 9}}
	next := entity.PromptProfileSpec{Revision: 2, ContentSHA256: strings.Repeat("b", 64), SourceRef: "ui://prompt/next", Locale: "ru", Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}
	updated, err := configurationUpdateSpec(context.Background(), nil, value.Principal{}, current, next, false)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	ownership := updated.(entity.ConfiguredSpec).ConfigurationOwnership()
	if ownership.SourceRef != sourceID || ownership.SourceRevision != 9 {
		t.Fatalf("copy lineage lost: %#v", ownership)
	}
}
