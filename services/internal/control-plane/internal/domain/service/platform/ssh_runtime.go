package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ReadSSHRuntimeInvocation admits only the registered runtime reader and a complete execution proof.
// ReadSSHRuntimeInvocation допускает лишь зарегистрированный runtime reader и полное доказательство выполнения.
func (s *Service) ReadSSHRuntimeInvocation(ctx context.Context, p value.Principal, q query.SSHRuntimeRead) (entity.SSHRuntimeResult, error) {
	var zero entity.SSHRuntimeResult
	p, err := s.principal(ctx, p)
	if err != nil {
		return zero, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.ssh.invocation.read" {
		return zero, errs.ErrForbidden
	}
	if !validSSHSetupRef(q.InvocationRef, "inv_") || !validSSHSetupRef(q.LeaseRef, "lea_") || len(q.Fence) < 1 || len(q.Fence) > 128 || q.Generation < 1 || !managedssh.Digest(q.RuntimeInputDigest) {
		return zero, errs.ErrInvalid
	}
	reader, ok := s.repository.(port.SSHRuntimeReader)
	if !ok {
		return zero, errs.ErrUnavailable
	}
	result, err := reader.ReadSSHRuntimeInvocation(ctx, p, q)
	if err != nil {
		return zero, err
	}
	if result.InvocationRef != q.InvocationRef || !result.Valid() {
		return zero, errs.ErrUnavailable
	}
	return result, nil
}
