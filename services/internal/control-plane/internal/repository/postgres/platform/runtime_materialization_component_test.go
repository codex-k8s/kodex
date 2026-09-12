package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

//go:embed testdata/sql/runtime_materialization_expected_authority.sql
var runtimeMaterializationExpectedAuthoritySQL string

func testClaimedRuntimeMaterializationProof(t *testing.T, ctx context.Context, repository *Repository, lease map[string]any, workloadInstance string, allowed bool) {
	t.Helper()
	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatal("encode synthetic runtime fixture")
	}
	var execution runtimeMaterializationInput
	if err := json.Unmarshal(raw, &execution); err != nil {
		t.Fatal("decode synthetic runtime fixture")
	}
	execution.WorkloadInstance = workloadInstance
	execution.RuntimeRevisionDigest = stringMap(lease, "revisionDigest")
	execution.SystemAssistant = stringMap(lease, "projectRef") == ""
	operation, digest, err := runtimeMaterializationDigest(execution)
	if err != nil {
		t.Fatalf("bind synthetic runtime request: %v", err)
	}
	input := platformrepo.ProofPrincipalInput{
		CallerWorkload: "runtime-controller", Operation: operation,
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		ProjectRef: stringMap(lease, "projectRef"), RequestDigestSHA256: digest,
	}
	proof, err := repository.ResolveProofAuthority(ctx, input)
	if !allowed {
		if !errors.Is(err, errs.ErrForbidden) || proof != (platformrepo.ProofAuthority{}) {
			t.Fatalf("closed execution retained materialization authority: %v", err)
		}
		return
	}
	if err != nil || proof.RuntimeExecution == nil {
		t.Fatalf("live execution materialization authority unavailable: %v", err)
	}
	var expectedActor, expectedOrganization, expectedProject, expectedRevision string
	if err := repository.pool.QueryRow(ctx, runtimeMaterializationExpectedAuthoritySQL, execution.LeaseRef).
		Scan(&expectedActor, &expectedOrganization, &expectedProject, &expectedRevision); err != nil {
		t.Fatalf("resolve synthetic owner authority: %v", err)
	}
	if proof.ActorID != expectedActor || proof.OrganizationID != expectedOrganization ||
		proof.ProjectID != expectedProject ||
		proof.RuntimeExecution.RevisionID != expectedRevision ||
		proof.RuntimeExecution.Generation != uint64(execution.Generation) ||
		proof.RuntimeExecution.RevisionDigest != execution.RuntimeRevisionDigest {
		t.Fatal("runtime proof diverged from server-owned root actor or revision")
	}
	foreignProject := input
	foreignProject.ProjectRef = "prj_unowned_fixture"
	if _, err := repository.ResolveProofAuthority(ctx, foreignProject); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign project accepted by execution proof: %v", err)
	}
	execution.Fence += "-other"
	_, changedDigest, err := runtimeMaterializationDigest(execution)
	if err != nil {
		t.Fatal("bind altered synthetic fence")
	}
	changed := input
	changed.RequestDigestSHA256 = changedDigest
	if _, err := repository.ResolveProofAuthority(ctx, changed); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("altered fence accepted by execution proof: %v", err)
	}
}
