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

// workflowCatalogRefPattern accepts the bounded ASCII reference shape for workflow catalog leases
// and immutable workflow/publication pins, independently of authority.
// workflowCatalogRefPattern принимает ограниченную ASCII-форму ссылок leases каталога workflow и
// immutable pins workflow и публикации независимо от полномочий.
var workflowCatalogRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// workflowCatalogDigestPattern accepts exactly sixty-four lowercase hex characters for the pinned
// workflow specification digest.
// workflowCatalogDigestPattern принимает ровно шестьдесят четыре lowercase hex-символа
// закреплённого дайджеста спецификации workflow.
var workflowCatalogDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// GetExecutionWorkflowCatalog requires resolved runtime-controller catalog authority and bounded
// lease/fence/query inputs, enforcing exclusive publication or active-run modes and exact
// immutable workflow pins before owner access.
// GetExecutionWorkflowCatalog требует разрешённые полномочия каталога runtime-controller и
// ограниченный ввод lease, fence и поиска, проверяет исключающие режимы publication либо active
// runs и точные immutable pins workflow до владельца.
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
	if input.Publication != nil && input.ActiveRuns != nil {
		return entity.ExecutionWorkflowCatalog{}, errs.ErrInvalid
	}
	var pins *query.ExecutionWorkflowReadPins
	if input.Publication != nil {
		pins = &input.Publication.Pins
		if input.Query != "" || input.PageToken != "" {
			return entity.ExecutionWorkflowCatalog{}, errs.ErrInvalid
		}
	}
	if input.ActiveRuns != nil {
		pins = &input.ActiveRuns.Pins
		if input.Query != "" {
			return entity.ExecutionWorkflowCatalog{}, errs.ErrInvalid
		}
	}
	if pins != nil && (!workflowCatalogRefPattern.MatchString(pins.WorkflowRef) || !workflowCatalogRefPattern.MatchString(pins.PublishedRef) || !workflowCatalogDigestPattern.MatchString(pins.SpecDigest) || pins.WorkflowVersion < 1) {
		return entity.ExecutionWorkflowCatalog{}, errs.ErrInvalid
	}
	return service.repository.GetExecutionWorkflowCatalog(ctx, p, input)
}
