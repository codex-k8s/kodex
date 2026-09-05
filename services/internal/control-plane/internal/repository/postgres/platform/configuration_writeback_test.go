package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestWriteBackEffectUsesActualNarrowedPackageAndExactPins(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	raw, _ := json.Marshal(definition)
	definition, _, err = integrationpackage.NormalizeManagedRevision(raw, "UI", definitions)
	if err != nil {
		t.Fatal(err)
	}
	for i := range definition.Spec.Capabilities {
		if definition.Spec.Capabilities[i].Key != "github.repository.content.update" {
			continue
		}
		for j := range definition.Spec.Capabilities[i].InputFields {
			field := &definition.Spec.Capabilities[i].InputFields[j]
			if field.Key == "content_base64" {
				field.MaximumLength = 8
			}
		}
	}
	raw, _ = json.Marshal(definition)
	row := writeBackRow{effect: entity.WriteBackBranch, proposal: entity.ConfigurationWriteBack{Ref: "mcwb_fixture01", RepositoryRef: "example/config", ProposalBranch: "kodex/writeback/mcwb_fixture01", SourceRefName: "main", Path: "config.yaml", BaseCommitSHA: strings.Repeat("a", 40), ProposedContentSHA256: strings.Repeat("b", 64)}, snapshot: writeBackSnapshot{ProposedContent: "small", Source: entity.ManagedConfigurationSourceWork{DefinitionKey: "github", DefinitionPackage: raw}}}
	input := port.ConfigurationWriteBackEffectInput{Effect: entity.WriteBackBranch, ParentCommitSHA: row.proposal.BaseCommitSHA, ContentSHA256: row.proposal.ProposedContentSHA256, CandidateCommitSHA: strings.Repeat("c", 40), CandidateTreeSHA: strings.Repeat("d", 40), CandidateBlobSHA: strings.Repeat("e", 40), BaseBlobSHA: strings.Repeat("f", 40)}
	if err := validateWriteBackEffectInput(row, input); err != nil {
		t.Fatalf("exact narrowed package: %v", err)
	}
	row.snapshot.ProposedContent = "too long for the actual package"
	if validateWriteBackEffectInput(row, input) == nil {
		t.Fatal("shipped input bound replaced actual package")
	}
	row.snapshot.ProposedContent = "small"
	for _, field := range []string{"parent", "content", "candidate", "blob"} {
		tampered := input
		switch field {
		case "parent":
			tampered.ParentCommitSHA = strings.Repeat("0", 40)
		case "content":
			tampered.ContentSHA256 = strings.Repeat("0", 64)
		case "candidate":
			tampered.CandidateCommitSHA = "HEAD"
		case "blob":
			tampered.BaseBlobSHA = "ref/main"
		}
		if validateWriteBackEffectInput(row, tampered) == nil {
			t.Fatalf("accepted wrong %s pin", field)
		}
	}
}

func TestWriteBackPullRequestURLCannotEscapeRepository(t *testing.T) {
	row := writeBackRow{proposal: entity.ConfigurationWriteBack{RepositoryRef: "example/config"}, snapshot: writeBackSnapshot{Source: entity.ManagedConfigurationSourceWork{DefinitionKey: "github"}}}
	if !validWriteBackPullRequest(row, "12", "https://github.com/example/config/pull/12") {
		t.Fatal("exact provider receipt rejected")
	}
	for _, raw := range []string{"http://github.com/example/config/pull/12", "https://github.com.evil.invalid/example/config/pull/12", "https://github.com/other/config/pull/12", "https://github.com/example/config/pull/12?token=unsafe", "https://user@github.com/example/config/pull/12", "https://github.com/example/config/pull/12#fragment", "https://github.com/example/config/pull/13"} {
		if validWriteBackPullRequest(row, "12", raw) {
			t.Fatal("unsafe provider receipt accepted")
		}
	}
	if validWriteBackPullRequest(row, "012", "https://github.com/example/config/pull/012") {
		t.Fatal("noncanonical provider ref accepted")
	}
	row.snapshot.Source.DefinitionKey = "gitlab"
	row.snapshot.Source.PublicConfiguration = map[string]any{"base_url": "https://gitlab.example.invalid"}
	if !validWriteBackPullRequest(row, "12", "https://gitlab.example.invalid/example/config/-/merge_requests/12") {
		t.Fatal("exact GitLab receipt rejected")
	}
}

func TestWriteBackLeaseRejectsStaleGenerationAndExpiredClaim(t *testing.T) {
	now := time.Unix(1000, 0)
	lease := entity.ConfigurationWriteBackLease{ProposalRef: "mcwb_fixture01", Attempt: 1, ClaimGeneration: 2, Claimant: "worker", Fence: "fence", ExpiresAt: now.Add(time.Minute)}
	row := writeBackRow{proposal: entity.ConfigurationWriteBack{Ref: lease.ProposalRef, State: entity.WriteBackUnknown}, lease: lease}
	if !writeBackLeaseMatches(row, lease, now) {
		t.Fatal("exact recovery lease rejected")
	}
	changed := lease
	changed.ClaimGeneration--
	if writeBackLeaseMatches(row, changed, now) || writeBackLeaseMatches(row, lease, lease.ExpiresAt) {
		t.Fatal("stale recovery lease accepted")
	}
	row.proposal.State = entity.WriteBackSucceeded
	if writeBackLeaseMatches(row, lease, now) {
		t.Fatal("terminal effect remains executable")
	}
}
