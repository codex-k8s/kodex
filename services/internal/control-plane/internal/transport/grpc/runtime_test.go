package grpc

import (
	"math"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestMapInt64AcceptsUnsignedValuesAndRejectsOverflow(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"uint32":   uint32(7),
		"uint64":   uint64(11),
		"uint":     uint(13),
		"overflow": uint64(math.MaxInt64) + 1,
	}
	for key, expected := range map[string]int64{
		"uint32": 7, "uint64": 11, "uint": 13, "overflow": 0,
	} {
		if actual := mapInt64(values, key); actual != expected {
			t.Fatalf("%s: получено %d, ожидалось %d", key, actual, expected)
		}
	}
}

func TestCastRuntimeRevisionPreservesUnsignedContractRevision(t *testing.T) {
	t.Parallel()

	revision := castRuntimeRevision(map[string]any{
		"roleRuntimeContractRevision": uint64(3),
	})
	if revision.GetRoleRuntimeContractRevision() != 3 {
		t.Fatalf("revision runtime-контракта потеряна: %d", revision.GetRoleRuntimeContractRevision())
	}
}

func TestCastRuntimeRevisionCarriesExactEnvironmentImageAndTools(t *testing.T) {
	t.Parallel()

	revision := castRuntimeRevision(map[string]any{
		"roleImageRecipeRef":        "imgrec_abcdefgh",
		"roleImageArtifactRef":      "imgart_abcdefgh",
		"roleImageRecipeGeneration": int64(4),
		"imageReference":            "registry.example/kodex/role@sha256:" + strings.Repeat("a", 64),
		"imageManifestDigest":       "sha256:" + strings.Repeat("a", 64),
		"environmentTools": []runtimecontract.RuntimeEnvironmentTool{{
			Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub", UsageHint: "Используй gh api",
		}},
	})
	if revision.GetRoleImageRecipeRef() != "imgrec_abcdefgh" ||
		revision.GetRoleImageArtifactRef() != "imgart_abcdefgh" ||
		revision.GetRoleImageRecipeGeneration() != 4 || len(revision.GetEnvironmentTools()) != 1 ||
		revision.GetEnvironmentTools()[0].GetCommand() != "gh" {
		t.Fatalf("runtime revision lost environment image/tools: %#v", revision)
	}
}

func TestCastClaimPreservesAuthoritativeProjectBinding(t *testing.T) {
	t.Parallel()

	claim := castClaim(map[string]any{
		"runRef":     "run_abcdefgh",
		"projectRef": "prj_abcdefgh",
		"sessionRef": "ses_abcdefgh",
	})
	if claim.GetRun().GetProjectRef() != "prj_abcdefgh" {
		t.Fatalf("project binding потерян: %q", claim.GetRun().GetProjectRef())
	}
}

func TestProviderCredentialRefreshTransportPreservesExactCASBinding(t *testing.T) {
	t.Parallel()

	request := &controlplanev1.CommitProviderCredentialRefreshRequest{
		LeaseRef: "lea_abcdefgh", Fence: "fnc_abcdefgh", Generation: 3,
		PreviousCredentialRevisionRef: "pcr_previous1", PreviousContentSha256: strings.Repeat("a", 64),
		SecretName: "runtime-provider-refresh-1", SecretUid: "10000000-0000-4000-8000-000000000010",
		SecretResourceVersion: "42", ContentSha256: strings.Repeat("b", 64),
	}
	payload := providerCredentialRefreshInput(request)
	if payload.LeaseRef != request.GetLeaseRef() || payload.Fence != request.GetFence() ||
		payload.Generation != request.GetGeneration() ||
		payload.PreviousCredentialRevisionRef != request.GetPreviousCredentialRevisionRef() ||
		payload.PreviousContentSHA256 != request.GetPreviousContentSha256() ||
		payload.SecretName != request.GetSecretName() || payload.SecretUID != request.GetSecretUid() ||
		payload.SecretResourceVersion != request.GetSecretResourceVersion() ||
		payload.ContentSHA256 != request.GetContentSha256() {
		t.Fatalf("transport lost provider credential CAS fields: %#v", payload)
	}

	binding := castProviderCredential(map[string]any{
		"providerAccountRef": "pacc_abcdefgh", "providerCredentialRevisionRef": "pcr_next1234",
		"providerCredentialRevisionNumber": int64(7), "providerSecretName": request.GetSecretName(),
		"providerSecretUID": request.GetSecretUid(), "providerSecretResourceVersion": request.GetSecretResourceVersion(),
		"providerCredentialSHA256": request.GetContentSha256(),
	})
	if binding.GetAccountRef() != "pacc_abcdefgh" || binding.GetCredentialRevisionRef() != "pcr_next1234" ||
		binding.GetCredentialRevision() != 7 || binding.GetSecretUid() != request.GetSecretUid() ||
		binding.GetContentSha256() != request.GetContentSha256() {
		t.Fatalf("transport returned incomplete provider credential binding: %#v", binding)
	}
}
