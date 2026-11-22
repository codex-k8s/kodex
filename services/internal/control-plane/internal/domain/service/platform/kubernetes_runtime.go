package platform

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ReadKubernetesRuntimeInvocation admits only the registered runtime reader and a complete execution proof.
// ReadKubernetesRuntimeInvocation допускает лишь зарегистрированный runtime reader и полное доказательство выполнения.
func (s *Service) ReadKubernetesRuntimeInvocation(ctx context.Context, p value.Principal, q query.KubernetesRuntimeRead) (entity.KubernetesRuntimeResult, error) {
	var zero entity.KubernetesRuntimeResult
	p, err := s.principal(ctx, p)
	if err != nil {
		return zero, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.kubernetes.invocation.read" {
		return zero, errs.ErrForbidden
	}
	if !validKubernetesSetupRef(q.InvocationRef, "inv_") || !validKubernetesSetupRef(q.LeaseRef, "lea_") || len(q.Fence) < 1 || len(q.Fence) > 128 || q.Generation < 1 || !managedkubernetes.ValidDigest(q.RuntimeInputDigest) {
		return zero, errs.ErrInvalid
	}
	reader, ok := s.repository.(port.KubernetesRuntimeReader)
	if !ok {
		return zero, errs.ErrUnavailable
	}
	result, err := reader.ReadKubernetesRuntimeInvocation(ctx, p, q)
	if err != nil {
		return zero, err
	}
	if result.InvocationRef != q.InvocationRef || !result.Valid() {
		return zero, errs.ErrUnavailable
	}
	return result, nil
}
