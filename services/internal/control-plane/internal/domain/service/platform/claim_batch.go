package platform

import (
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// validatePlatformClaimBatch checks the shared integration-test, invocation and interaction-delivery claim input: a nonblank instance and a batch size from one through thirty-two. It does not trim the instance passed to the repository, resolve a principal, authorize a claim or change lease state; callers retain those responsibilities and their existing order.
// validatePlatformClaimBatch проверяет общий вход захвата проверок интеграций, вызовов и доставок взаимодействий: непустой экземпляр и размер пачки от одного до тридцати двух. Передаваемый репозиторию экземпляр не обрезается, субъект не разрешается, полномочия и состояние lease не меняются; эти обязанности и прежний порядок сохраняются у вызывающих методов.
func validatePlatformClaimBatch(instance string, limit int32) error {
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return errs.ErrInvalid
	}
	return nil
}
