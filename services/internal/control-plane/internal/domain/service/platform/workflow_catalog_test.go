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

// workflowCatalogRepository counts successful domain workflow-catalog reads while supplying an
// inert principal resolver for local boundary tests.
// workflowCatalogRepository подсчитывает успешные чтения доменного каталога workflow с неактивным
// разрешением principal для локальных тестов границы.
type workflowCatalogRepository struct {
	port.Repository
	calls int
}

// ResolvePrincipal returns the supplied synthetic workflow-catalog principal unchanged to isolate
// workload and typed input validation.
// ResolvePrincipal возвращает переданный синтетический principal каталога workflow без изменений
// для отдельной проверки workload и типизированного ввода.
func (r *workflowCatalogRepository) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, nil
}

// GetExecutionWorkflowCatalog counts the owner query and returns an empty synthetic workflow
// catalog without reading execution state.
// GetExecutionWorkflowCatalog подсчитывает query владельца и возвращает пустой синтетический
// каталог workflow без чтения состояния выполнения.
func (r *workflowCatalogRepository) GetExecutionWorkflowCatalog(_ context.Context, _ value.Principal, _ query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	r.calls++
	return entity.ExecutionWorkflowCatalog{}, nil
}

// TestExecutionWorkflowCatalogDomainBoundary checks a 200-rune query succeeds while malformed
// generation, lease, encoding, cursor or typed mode fails before owner reads and foreign workloads
// are denied.
// TestExecutionWorkflowCatalogDomainBoundary проверяет успех запроса из 200 Unicode-символов и
// отказ до чтения владельца неверным поколению, lease, кодировке, курсору либо типизированному
// режиму с запретом чужого workload.
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

// TestExecutionWorkflowCatalogTypedModes checks immutable publication and active-run modes with
// exact pins, rejecting mixed modes, publication pagination and active-run text search before
// owner reads.
// TestExecutionWorkflowCatalogTypedModes проверяет immutable-режимы publication и active runs с
// точными pins и отказ до чтений владельца смешанным режимам, пагинации publication и текстовому
// поиску active runs.
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
