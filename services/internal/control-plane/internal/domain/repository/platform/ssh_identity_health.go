package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHIdentityHealthRepository consumes one existing health lease and retains immutable completion without using generic TOKEN verification.
// SSHIdentityHealthRepository расходует existing health lease и сохраняет immutable completion без generic TOKEN verification.
type SSHIdentityHealthRepository interface {
	// BeginSSHIdentityHealthProbe atomically checks current owner source before releasing private material pins once.
	// BeginSSHIdentityHealthProbe атомарно проверяет current owner source до однократной выдачи private material pins.
	BeginSSHIdentityHealthProbe(context.Context, value.Principal, managedssh.Binding) (entity.SSHIdentityHealthAdmission, error)
	// CompleteSSHIdentityHealthProbe records an exact joined read or discards stale authority in the same owner transaction.
	// CompleteSSHIdentityHealthProbe сохраняет exact joined read либо отбрасывает stale authority в той же owner transaction.
	CompleteSSHIdentityHealthProbe(context.Context, value.Principal, command.SSHIdentityHealthComplete) (entity.SSHIdentityHealthReceipt, error)
}
