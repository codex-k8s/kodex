package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type artifactTransferRepository struct {
	platformrepo.Repository
	resolved value.Principal
	failure  error
	reads    int
}

func (repository *artifactTransferRepository) ResolvePrincipal(context.Context, value.Principal) (value.Principal, error) {
	return repository.resolved, repository.failure
}

func (repository *artifactTransferRepository) ReadExecutionArtifact(_ context.Context, p value.Principal, lease, fence string, generation int64, ref string) (platformrepo.ArtifactDownload, error) {
	repository.reads++
	if !reflect.DeepEqual(p, repository.resolved) || lease != "lease_exact" || fence != "fence_exact" || generation != 7 || ref != "art_exact" {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	return platformrepo.ArtifactDownload{Artifact: entity.Artifact{Ref: ref}}, nil
}

func TestArtifactTransferRequiresResolvedExactOperationAndWorkload(t *testing.T) {
	for _, scenario := range []string{"exact", "unary permission", "other workload", "identity failure", "empty lease", "empty fence", "zero generation", "empty artifact", "owner mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			repository := &artifactTransferRepository{resolved: value.Principal{ActorID: "usr_transfer", AuthorityTenant: "org_transfer",
				Permission: "platform.runtime.execution.artifact.stream", CallerWorkload: "runtime-controller", CorrelationRef: "cor_transfer", CredentialRevision: 1}}
			incoming := repository.resolved
			incoming.ActorID = "transport_actor"
			lease, fence, generation, ref := "lease_exact", "fence_exact", int64(7), "art_exact"
			switch scenario {
			case "unary permission":
				repository.resolved.Permission = "platform.runtime.execution.artifact.read"
			case "other workload":
				repository.resolved.CallerWorkload = "agent-runner"
			case "identity failure":
				repository.failure = errs.ErrForbidden
			case "empty lease":
				lease = ""
			case "empty fence":
				fence = ""
			case "zero generation":
				generation = 0
			case "empty artifact":
				ref = ""
			case "owner mismatch":
				ref = "art_other"
			}
			service, err := New(repository)
			if err != nil {
				t.Fatal(err)
			}
			// Валидный transport principal разрешается в доменную identity;
			// owner получает именно результат ResolvePrincipal.
			result, err := service.OpenExecutionArtifactTransfer(t.Context(), incoming, lease, fence, generation, ref)
			if scenario == "exact" {
				if err != nil || result.Artifact.Ref != ref || repository.reads != 1 {
					t.Fatalf("exact transfer rejected: %v", err)
				}
			} else if scenario == "owner mismatch" {
				if !errors.Is(err, errs.ErrConflict) || repository.reads != 1 {
					t.Fatal("owner mismatch bypassed")
				}
			} else if err == nil || repository.reads != 0 {
				t.Fatal("invalid authority reached owner read")
			}
		})
	}
}
