package grpc

import (
	"math"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

// TestRuntimePromptProvenanceRequiresVersionedOwnerSnapshot checks historical prompt fields stay
// unbound and versioned snapshots reject unknown or incomplete provenance.
// TestRuntimePromptProvenanceRequiresVersionedOwnerSnapshot проверяет отсутствие привязки прежних
// полей промпта и отказ снимков с неизвестной версией либо неполным происхождением.
func TestRuntimePromptProvenanceRequiresVersionedOwnerSnapshot(t *testing.T) {
	values := map[string]any{"promptServiceTemplateRevision": runtimecontract.PromptServiceRevision,
		"promptServiceTemplateDigest": strings.Repeat("a", 64), "promptTargetKind": "AGENT"}
	if got := castRuntimeRevision(values); got == nil || got.GetPromptServiceTemplateRevision() != "" {
		t.Fatal("historical snapshot binding changed")
	}
	values["promptRuntimeContractVersion"] = 1
	if got := castRuntimeRevision(values); got == nil || got.GetPromptServiceTemplateRevision() != runtimecontract.PromptServiceRevision || got.GetPromptTargetKind() != "AGENT" || got.GetPromptServiceTemplateDigest() != strings.Repeat("a", 64) {
		t.Fatal("new owner provenance lost")
	}
	values["promptRuntimeContractVersion"] = 2
	if castRuntimeRevision(values) != nil {
		t.Fatal("unknown snapshot version accepted")
	}
	values["promptRuntimeContractVersion"] = 1
	delete(values, "promptServiceTemplateRevision")
	if castRuntimeRevision(values) != nil {
		t.Fatal("incomplete owner provenance accepted")
	}
}

// TestCastRuntimeRevisionPreservesAbsentDesiredRevision checks that an absent desired revision
// remains nil rather than becoming an empty snapshot.
// TestCastRuntimeRevisionPreservesAbsentDesiredRevision проверяет сохранение nil желаемой ревизии
// вместо создания пустого снимка.
func TestCastRuntimeRevisionPreservesAbsentDesiredRevision(t *testing.T) {
	t.Parallel()

	if castRuntimeRevision(nil) != nil {
		t.Fatal("absent desired runtime revision became an empty snapshot")
	}
}

// TestMapInt64AcceptsUnsignedValuesAndRejectsOverflow checks supported unsigned integer values and
// the zero fallback for a uint64 overflow.
// TestMapInt64AcceptsUnsignedValuesAndRejectsOverflow проверяет поддерживаемые беззнаковые целые и
// нулевую замену при переполнении uint64.
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

// TestCastRuntimeRevisionPreservesUnsignedContractRevision checks that an unsigned runtime
// contract revision survives snapshot projection.
// TestCastRuntimeRevisionPreservesUnsignedContractRevision проверяет сохранение беззнаковой
// ревизии runtime-контракта при преобразовании снимка.
func TestCastRuntimeRevisionPreservesUnsignedContractRevision(t *testing.T) {
	t.Parallel()

	revision := castRuntimeRevision(map[string]any{
		"roleRuntimeContractRevision": uint64(3),
	})
	if revision.GetRoleRuntimeContractRevision() != 3 {
		t.Fatalf("revision runtime-контракта потеряна: %d", revision.GetRoleRuntimeContractRevision())
	}
}

// TestCastRuntimeRevisionCarriesExactEnvironmentImageAndTools checks preservation of image recipe,
// artifact, generation and tool command bindings in the runtime snapshot.
// TestCastRuntimeRevisionCarriesExactEnvironmentImageAndTools проверяет сохранение привязок
// рецепта образа, артефакта, поколения и команды инструмента в снимке runtime.
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

// TestCastRuntimeRevisionCarriesProviderSessionForContinuation checks that the provider session
// identifier survives runtime projection for continuation.
// TestCastRuntimeRevisionCarriesProviderSessionForContinuation проверяет сохранение идентификатора
// сессии провайдера при преобразовании runtime для продолжения.
func TestCastRuntimeRevisionCarriesProviderSessionForContinuation(t *testing.T) {
	t.Parallel()

	const sessionID = "00000000-0000-4000-8000-000000000003"
	revision := castRuntimeRevision(map[string]any{"codexSessionID": sessionID})
	if revision.GetCodexSessionId() != sessionID {
		t.Fatalf("provider session binding потерян: %q", revision.GetCodexSessionId())
	}
}

// TestCastClaimPreservesAuthoritativeProjectBinding checks that the owner project reference is
// retained in the projected execution claim.
// TestCastClaimPreservesAuthoritativeProjectBinding проверяет сохранение ссылки проекта владельца
// в преобразованном claim исполнения.
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

// TestProviderCredentialRefreshTransportPreservesExactCASBinding checks every refresh CAS input
// and exact credential metadata shared by refresh and runtime projections, including empty
// metadata.
// TestProviderCredentialRefreshTransportPreservesExactCASBinding проверяет каждый CAS-вход
// обновления и точные метаданные credential в ответе обновления и runtime, включая пустые
// метаданные.
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

	values := map[string]any{
		"providerAccountRef": "pacc_abcdefgh", "providerCredentialRevisionRef": "pcr_next1234",
		"providerCredentialRevisionNumber": int64(7), "providerSecretName": request.GetSecretName(),
		"providerSecretUID": request.GetSecretUid(), "providerSecretResourceVersion": request.GetSecretResourceVersion(),
		"providerCredentialSHA256": request.GetContentSha256(),
	}
	binding := castProviderCredential(values)
	if binding.GetAccountRef() != "pacc_abcdefgh" || binding.GetCredentialRevisionRef() != "pcr_next1234" ||
		binding.GetCredentialRevision() != 7 || binding.GetSecretName() != request.GetSecretName() ||
		binding.GetSecretResourceVersion() != request.GetSecretResourceVersion() || binding.GetSecretUid() != request.GetSecretUid() ||
		binding.GetContentSha256() != request.GetContentSha256() {
		t.Fatalf("transport returned incomplete provider credential binding: %#v", binding)
	}
	if revision := castRuntimeRevision(values); revision == nil || !proto.Equal(revision.GetProviderCredential(), binding) {
		t.Fatal("runtime snapshot lost exact provider credential binding")
	}
	for _, empty := range []map[string]any{{}, {"providerAccountRef": 7, "providerCredentialRevisionNumber": "invalid"}} {
		revision := castRuntimeRevision(empty)
		if revision == nil || revision.ProviderCredential == nil || !proto.Equal(revision.ProviderCredential, &controlplanev1.ProviderCredentialBinding{}) {
			t.Fatal("empty or mistyped credential metadata changed runtime snapshot shape")
		}
		if got := castProviderCredential(empty); got == nil || !proto.Equal(got, revision.ProviderCredential) {
			t.Fatal("empty credential projection differs between refresh and runtime")
		}
	}
}
