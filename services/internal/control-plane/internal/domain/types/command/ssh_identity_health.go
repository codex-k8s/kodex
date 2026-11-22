package command

import (
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"time"
)

// SSHIdentityHealthComplete reports a joined bounded read only; it cannot claim permission for any resource effect.
// SSHIdentityHealthComplete сообщает лишь joined bounded read и не подтверждает permission для resource effect.
type SSHIdentityHealthComplete struct {
	Binding                                           managedssh.Binding
	AttemptRef, SnapshotDigest, ObservedSubjectDigest string
	Outcome                                           entity.PersonalIntegrationCredentialTestOutcome
	QualifiedUntil                                    time.Time
}
