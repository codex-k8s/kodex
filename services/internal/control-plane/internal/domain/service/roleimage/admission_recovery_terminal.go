package roleimage

import (
	"context"
	"regexp"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// admissionRecoveryRunPattern accepts only a version timestamp followed by the forty-lowercase-hex
// revision used by canonical admission recovery runs.
// admissionRecoveryRunPattern принимает только version timestamp и ревизию из сорока строчных
// hex-символов, используемые каноническими admission recovery runs.
var admissionRecoveryRunPattern = regexp.MustCompile(`^v[0-9]{14}-[a-f0-9]{40}$`)

// GetAdmissionRecoveryTerminal requires resolved controller recovery authority and a canonical run
// ID, reconstructing the original bridge claim UUID before the authoritative terminal-proof read.
// GetAdmissionRecoveryTerminal требует разрешённые recovery полномочия controller и канонический
// run ID, восстанавливая исходный claim UUID bridge до авторитетного чтения terminal proof.
func (service *Service) GetAdmissionRecoveryTerminal(ctx context.Context, principal value.Principal, runID string) (repository.AdmissionTerminalProof, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return repository.AdmissionTerminalProof{}, err
	}
	if err := authorize(principal, "platform.role-images.admission.recovery-terminal.get", "image-admission-controller"); err != nil {
		return repository.AdmissionTerminalProof{}, err
	}
	if !admissionRecoveryRunPattern.MatchString(runID) {
		return repository.AdmissionTerminalProof{}, errs.ErrInvalid
	}
	// Тот же immutable ключ, который назначает bridge при исходном claim.
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("image-admission-bridge\x00claim\x00"+runID)).String()
	return service.repository.GetAdmissionRecoveryTerminal(ctx, principal, key)
}
