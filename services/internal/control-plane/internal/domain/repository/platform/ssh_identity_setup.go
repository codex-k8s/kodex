package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHIdentitySetupRepository owns the existing platform's purpose-specific setup ledger; it never receives private key bytes or selects generic TOKEN credentials.
// SSHIdentitySetupRepository владеет purpose-specific setup ledger существующей платформы; private key bytes и выбор generic TOKEN credentials сюда не передаются.
type SSHIdentitySetupRepository interface {
	// BeginSSHIdentitySetup resolves actual self/shared ownership before OCC/idempotency and grants the first one-shot upload capability only once.
	// BeginSSHIdentitySetup разрешает actual self/shared ownership до OCC/idempotency и выдаёт first one-shot upload capability лишь один раз.
	BeginSSHIdentitySetup(context.Context, value.Principal, value.Mutation, command.SSHIdentitySetupBegin) (entity.SSHIdentitySetupAdmission, error)
	// GetSSHIdentitySetup returns current safe metadata under the fresh owner boundary, never raw permits or physical material pins.
	// GetSSHIdentitySetup возвращает current safe metadata под fresh owner boundary без raw permits либо physical material pins.
	GetSSHIdentitySetup(context.Context, value.Principal, command.SSHIdentitySetupRead) (entity.SSHIdentitySetupView, error)
	// CancelSSHIdentitySetup closes all setup grants atomically while retaining immutable late/orphan evidence without claiming remote absence.
	// CancelSSHIdentitySetup атомарно закрывает все setup grants, сохраняя immutable late/orphan evidence без утверждения remote absence.
	CancelSSHIdentitySetup(context.Context, value.Principal, value.Mutation, command.SSHIdentitySetupCancel) (entity.SSHIdentitySetupView, error)
	// ResolveSSHIdentityUpload reads current private owner coordinates without spending external write credit or reissuing a raw permit.
	// ResolveSSHIdentityUpload читает current private owner coordinates без external write credit либо повторной выдачи raw permit.
	ResolveSSHIdentityUpload(context.Context, value.Principal, command.SSHIdentityUploadResolve) (entity.SSHIdentityUploadSnapshot, error)
	// ClaimSSHIdentityUpload durably admits one sole WRITE or a receipt-backed observation; expired unreported and rejected attempts never gain observation or recreate credit.
	// ClaimSSHIdentityUpload устойчиво допускает sole WRITE либо receipt-backed observation; expired unreported и rejected attempts не получают observation либо recreate credit.
	ClaimSSHIdentityUpload(context.Context, value.Principal, command.SSHIdentityUploadClaim) (entity.SSHIdentityUploadAdmission, error)
	// CompleteSSHIdentityUpload retains exact material evidence; attachment requires current owner/version/deadline in the same transaction, and contradictory late ACK evidence quarantines only the original revision.
	// CompleteSSHIdentityUpload сохраняет exact material evidence; attachment требует current owner/version/deadline в той же transaction, а contradictory late ACK evidence quarantines только original revision.
	CompleteSSHIdentityUpload(context.Context, value.Principal, command.SSHIdentityUploadComplete) (entity.SSHIdentitySetupView, error)
}
