package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ReadKubernetesStreamOutput admits only the public owner route and validates the bounded result without changing execution state.
// ReadKubernetesStreamOutput допускает только public owner route и проверяет bounded result без изменения execution state.
func (s *Service) ReadKubernetesStreamOutput(ctx context.Context, p value.Principal, q query.KubernetesStreamOutputRead) (entity.KubernetesStreamOutput, error) {
	var zero entity.KubernetesStreamOutput
	p, err := s.principal(ctx, p)
	if err != nil {
		return zero, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.integrations.kubernetes.output.read" {
		return zero, errs.ErrForbidden
	}
	if !validKubernetesSetupRef(q.InvocationRef, "inv_") || !validKubernetesSetupRef(q.StreamRef, "kst_") || q.Generation < 1 || q.AfterSequence > managedkubernetes.MaximumFrameSequence {
		return zero, errs.ErrInvalid
	}
	reader, ok := s.repository.(port.KubernetesStreamOutputReader)
	if !ok {
		return zero, errs.ErrUnavailable
	}
	v, err := reader.ReadKubernetesStreamOutput(ctx, p, q)
	if err != nil {
		if v.Frame != nil {
			clear(v.Frame.Payload)
		}
		return zero, err
	}
	if v.InvocationRef != q.InvocationRef || v.StreamRef != q.StreamRef || v.Generation != q.Generation || v.AfterSequence != q.AfterSequence || !v.Valid() || !v.AuthorizedUntil.After(time.Now()) {
		if v.Frame != nil {
			clear(v.Frame.Payload)
		}
		return zero, errs.ErrUnavailable
	}
	return v, nil
}
