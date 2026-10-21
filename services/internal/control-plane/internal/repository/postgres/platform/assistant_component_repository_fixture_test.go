package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newConfiguredAssistantComponentRepository creates one repository adapter
// with the exact synthetic provider credential and image policy shared by
// assistant scope, parallel-lifecycle, warm-wire and organization-environment
// component tests. The caller selects the model; storage is not bootstrapped here so
// an adapter can be reconstructed after a restart.
// RU: newConfiguredAssistantComponentRepository создаёт adapter с точными
// синтетическими credential провайдера и image policy для component-тестов
// scope, параллельного lifecycle, warm-wire и окружения организации. Модель выбирает
// caller; хранилище здесь не инициализируется, чтобы adapter можно было
// заново создать после перезапуска.
func newConfiguredAssistantComponentRepository(t *testing.T, pool *pgxpool.Pool, model string) *Repository {
	t.Helper()
	repository, err := New(pool, "openai-codex", model, objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	return repository
}
