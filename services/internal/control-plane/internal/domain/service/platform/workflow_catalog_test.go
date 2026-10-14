package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type workflowCatalogRepository struct {
	port.Repository
	calls int
}

func (r *workflowCatalogRepository) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, nil
}
func (r *workflowCatalogRepository) GetExecutionWorkflowCatalog(_ context.Context, _ value.Principal, _ query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	r.calls++
	return entity.ExecutionWorkflowCatalog{}, nil
}
func TestExecutionWorkflowCatalogDomainBoundary(t *testing.T) {
	r := &workflowCatalogRepository{}
	service, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	p := testInstructionPrincipal()
	p.CallerWorkload = "runtime-controller"
	p.Permission = "platform.runtime.execution.workflow.catalog"
	input := query.ExecutionWorkflowCatalog{LeaseRef: "lse_exact001", Fence: "fixture-fence", Generation: 1, Query: strings.Repeat("🙂", 200)}
	if _, err := service.GetExecutionWorkflowCatalog(t.Context(), p, input); err != nil || r.calls != 1 {
		t.Fatalf("valid query: %v", err)
	}
	for _, mutate := range []func(*query.ExecutionWorkflowCatalog){
		func(i *query.ExecutionWorkflowCatalog) { i.Generation = 0 },
		func(i *query.ExecutionWorkflowCatalog) { i.LeaseRef = "" },
		func(i *query.ExecutionWorkflowCatalog) { i.Query = strings.Repeat("🙂", 201) },
		func(i *query.ExecutionWorkflowCatalog) { i.Query = "invalid\x00" },
		func(i *query.ExecutionWorkflowCatalog) { i.Query = string([]byte{0xff}) },
		func(i *query.ExecutionWorkflowCatalog) { i.PageToken = strings.Repeat("x", 513) },
		func(i *query.ExecutionWorkflowCatalog) { i.Publication = &query.ExecutionWorkflowPublicationRead{} },
		func(i *query.ExecutionWorkflowCatalog) { i.ActiveRuns = &query.ExecutionWorkflowActiveRunsRead{} },
	} {
		invalid := input
		mutate(&invalid)
		if _, err := service.GetExecutionWorkflowCatalog(t.Context(), p, invalid); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid query: %v", err)
		}
	}
	p.CallerWorkload = "control-api-gateway"
	if _, err := service.GetExecutionWorkflowCatalog(t.Context(), p, input); !errors.Is(err, errs.ErrForbidden) || r.calls != 1 {
		t.Fatal("foreign workload query reached repository")
	}
}

func TestExecutionWorkflowCatalogTypedModes(t *testing.T) {
	r := &workflowCatalogRepository{}
	service, _ := New(r)
	p := testInstructionPrincipal()
	p.CallerWorkload = "runtime-controller"
	p.Permission = "platform.runtime.execution.workflow.catalog"
	base := query.ExecutionWorkflowCatalog{LeaseRef: "lse_exact001", Fence: "fixture-fence", Generation: 1}
	pins := query.ExecutionWorkflowReadPins{WorkflowRef: "wfl_workflow01", PublishedRef: "wfv_workflow01", SpecDigest: strings.Repeat("a", 64), WorkflowVersion: 7}
	full := base
	full.Publication = &query.ExecutionWorkflowPublicationRead{Pins: pins}
	active := base
	active.ActiveRuns = &query.ExecutionWorkflowActiveRunsRead{Pins: pins}
	active.PageToken = "next"
	for _, input := range []query.ExecutionWorkflowCatalog{full, active} {
		if _, err := service.GetExecutionWorkflowCatalog(t.Context(), p, input); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []query.ExecutionWorkflowCatalog{
		{LeaseRef: base.LeaseRef, Fence: base.Fence, Generation: 1, Publication: full.Publication, ActiveRuns: active.ActiveRuns},
		{LeaseRef: base.LeaseRef, Fence: base.Fence, Generation: 1, Publication: full.Publication, PageToken: "mixed"},
		{LeaseRef: base.LeaseRef, Fence: base.Fence, Generation: 1, ActiveRuns: active.ActiveRuns, Query: "mixed"},
	} {
		if _, err := service.GetExecutionWorkflowCatalog(t.Context(), p, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("mixed mode accepted")
		}
	}
	if r.calls != 2 {
		t.Fatal("invalid mode reached repository")
	}
}
