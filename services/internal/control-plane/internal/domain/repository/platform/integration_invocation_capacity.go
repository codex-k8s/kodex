package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// IntegrationInvocationCapacityRepository admits all supported delivery classes in one owner transaction.
// Missing implementations cannot silently ignore mixed capacity and issue an ordinary legacy claim.
// IntegrationInvocationCapacityRepository допускает supported delivery classes в одной owner transaction.
// Missing implementation не игнорирует mixed capacity и не выдаёт ordinary legacy claim.
type IntegrationInvocationCapacityRepository interface {
	ClaimIntegrationInvocationsWithCapacity(context.Context, value.Principal, string, int32, value.IntegrationInvocationDeliveryCapacity) ([]map[string]any, error)
}
