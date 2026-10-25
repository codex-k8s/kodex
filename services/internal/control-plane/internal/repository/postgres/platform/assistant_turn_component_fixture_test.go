package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newWarmTurnComponentFixture prepares the two existing turn-input/SYSTEM-run fixtures in their
// original order: fixed synthetic provider, caller-selected image fixture, bootstrap, observed
// warm evidence, turn owner, service, warm-report principal, assistant read and READY report.
// The image factory runs at the original configuration step, preserving its distinct references
// and fresh key allocation. Every original failure remains fatal; success leaves the caller's
// already nil pool error unchanged. The caller owns its deadline, isolated pool and Close, then
// runs all commands and assertions. No claim principal, real provider, Pod or external key is made.
// newWarmTurnComponentFixture готовит прежние оснастки turn-input/SYSTEM-run в исходном порядке:
// fixed synthetic provider, выбранная caller image fixture, bootstrap, observed warm evidence,
// turn owner, service, warm-report principal, чтение assistant и READY report. Image factory
// вызывается на прежнем шаге настройки, сохраняя отдельные ссылки и новый массив ключа. Каждая
// прежняя ошибка остаётся Fatal; при успехе уже nil pool error вызывающего не меняется. Caller
// владеет deadline, isolated pool и Close, затем выполняет все команды и проверки. Claim principal,
// настоящий provider, Pod либо внешний ключ не создаются.
func newWarmTurnComponentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, imageConfig func() RoleImageConfig) (*Repository, *platformservice.Service, value.Principal, entity.SystemAssistant) {
	t.Helper()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1",
		SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(imageConfig()); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	return r, service, owner, assistant
}
