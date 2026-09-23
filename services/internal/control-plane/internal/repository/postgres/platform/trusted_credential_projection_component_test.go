package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// Использует настоящую lease из claim fixture и возвращает terminal readback.
func testTrustedRuntimeProjection(t *testing.T, ctx context.Context, repository *Repository, lease map[string]any) func() {
	t.Helper()
	var input platformrepo.RuntimeCredentialProjectionInput
	err := repository.pool.QueryRow(ctx, queryProviderActiveProjectionPins, stringMap(lease, "leaseRef")).Scan(
		&input.Authority.TenantID, &input.Authority.ActorID, &input.Authority.ProjectID,
		&input.LeaseRef, &input.WorkloadInstance, &input.Generation, &input.RuntimeRevisionRef, &input.RuntimeRevisionDigest,
		&input.Attempt, &input.InputDigest, &input.SessionRef, &input.TurnRef)
	if err != nil {
		t.Fatal(err)
	}
	expected := input.Authority
	input.Authority = platformrepo.CredentialProjectionAuthority{RPCProfile: transportprofile.TrustedCluster,
		CallerWorkloadID: "runtime-controller", CallerFullMethod: runtimeProjectionMethod}
	input.Fence = stringMap(lease, "fence")
	broker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "secret-broker",
		Operation: "platform.credential-projections.runtime.resolve",
	}, "secret-broker")
	broker, err = repository.ResolvePrincipal(ctx, broker)
	if err != nil {
		t.Fatal(err)
	}
	// Копия конфигурации не переключает профиль общей fixture между сценариями.
	trusted := *repository
	if err := trusted.ConfigureRPCProfile(transportprofile.TrustedCluster); err != nil {
		t.Fatal(err)
	}
	result, err := trusted.ResolveRuntimeCredentialProjection(ctx, broker, input)
	if err != nil {
		t.Fatalf("trusted exact lease projection: %v", err)
	}
	if result.Authority.ActorID != expected.ActorID || result.Authority.TenantID != expected.TenantID ||
		result.Authority.ProjectID != expected.ProjectID || result.Authority.ProofJTI != "" ||
		result.Authority.SourceRevision != uint64(input.Generation) || result.Authority.SourceDigestSHA256 != input.RuntimeRevisionDigest {
		t.Fatal("owner did not derive the exact lease authority")
	}
	repeated, err := trusted.ResolveRuntimeCredentialProjection(ctx, broker, input)
	if err != nil || repeated.Authority != result.Authority || !sameProviderBinding(repeated.ProviderCredential, result.ProviderCredential) {
		t.Fatal("unchanged materialization changed the authority snapshot")
	}
	for name, mutate := range map[string]func(*platformrepo.RuntimeCredentialProjectionInput){
		"fence":    func(i *platformrepo.RuntimeCredentialProjectionInput) { i.Fence = "wrong-fence" },
		"attempt":  func(i *platformrepo.RuntimeCredentialProjectionInput) { i.Attempt++ },
		"workload": func(i *platformrepo.RuntimeCredentialProjectionInput) { i.WorkloadInstance += "-other" },
		"scope":    func(i *platformrepo.RuntimeCredentialProjectionInput) { i.Authority.ActorID = expected.ActorID },
	} {
		t.Run("trusted projection rejects "+name, func(t *testing.T) {
			candidate := input
			mutate(&candidate)
			if _, err := trusted.ResolveRuntimeCredentialProjection(ctx, broker, candidate); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("changed binding was not rejected: %v", err)
			}
		})
	}
	validate := broker
	validate.Permission = "platform.credential-projections.runtime.validate"
	input.Fence, input.Authority = "", result.Authority
	input.ProviderCredential, input.RuntimeSecrets = result.ProviderCredential, result.RuntimeSecrets
	if valid, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, input); err != nil || !valid {
		t.Fatalf("current projection recovery: %v", err)
	}
	changed := input
	changed.Authority.CallerCredentialRevision++
	if valid, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, changed); err != nil || valid {
		t.Fatalf("changed credential generation accepted: %v", err)
	}
	return func() {
		t.Helper()
		if valid, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, input); err != nil || valid {
			t.Fatalf("terminal lease kept projection valid: %v", err)
		}
	}
}
