package platform

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

var workflowCatalogRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func (service *Service) GetExecutionWorkflowCatalog(ctx context.Context, p value.Principal, input query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ExecutionWorkflowCatalog{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.execution.workflow.catalog" {
		return entity.ExecutionWorkflowCatalog{}, errs.ErrForbidden
	}
	if input.Generation < 1 || len(input.Fence) < 8 || len(input.Fence) > 256 ||
		!workflowCatalogRefPattern.MatchString(input.LeaseRef) || !utf8.ValidString(input.Query) ||
		utf8.RuneCountInString(input.Query) > 200 || strings.ContainsRune(input.Query, '\x00') || len(input.PageToken) > 512 {
		return entity.ExecutionWorkflowCatalog{}, errs.ErrInvalid
	}
	return service.repository.GetExecutionWorkflowCatalog(ctx, p, input)
}
