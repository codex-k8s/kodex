package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) ReadAssistantTaskSession(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, input query.AssistantTaskSessionRead) (query.AssistantTaskSessionPage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return query.AssistantTaskSessionPage{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.assistant.resources.search" || strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 {
		return query.AssistantTaskSessionPage{}, errs.ErrForbidden
	}
	if len(input.RunRef) < 8 || len(input.RunRef) > 96 || len(input.Cursor) > 1024 {
		return query.AssistantTaskSessionPage{}, errs.ErrInvalid
	}
	for _, c := range input.RunRef {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return query.AssistantTaskSessionPage{}, errs.ErrInvalid
		}
	}
	return service.repository.ReadAssistantTaskSession(ctx, p, leaseRef, fence, generation, input)
}
