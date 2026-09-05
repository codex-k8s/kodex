package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testTemplateVariableContext(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, agentRef, runtimeRef string) {
	t.Helper()
	read := func(filter query.Filter) map[string]entity.TemplateVariable {
		t.Helper()
		items, _, _, err := service.ListTemplateVariables(ctx, owner, filter)
		if err != nil {
			t.Fatal(err)
		}
		result := make(map[string]entity.TemplateVariable, len(items))
		for _, item := range items {
			result[item.Name] = item
		}
		return result
	}
	base := query.Filter{Page: query.Page{Size: 100}}
	global := read(base)
	if !global["user.ref"].Available || global["project.ref"].Available || global["project.ref"].Reason != variableProjectRequired || global["agent.ref"].Reason != variableAgentRequired {
		t.Fatal("global variable eligibility mismatch")
	}
	base.ProjectRef = projectRef
	base.TemplateContext = &query.TemplateVariableContext{AgentRef: agentRef}
	agent := read(base)
	if !agent["agent.ref"].Available || !agent["project.ref"].Available || agent["run.ref"].Available || agent["run.ref"].Reason != variableRuntimeRequired {
		t.Fatal("agent variable eligibility mismatch")
	}
	base.TemplateContext = &query.TemplateVariableContext{AgentRef: agentRef, RuntimeRevisionRef: runtimeRef}
	runtime := read(base)
	if !runtime["run.ref"].Available || !runtime["session.ref"].Available || !runtime["agent.ref"].Available || runtime["workflow.ref"].Available || runtime["workflow.ref"].Reason != variableNotMaterialized {
		t.Fatal("sealed variable eligibility mismatch")
	}
	base.Page.Size = 1
	items, total, next, err := service.ListTemplateVariables(ctx, owner, base)
	if err != nil || len(items) != 1 || total != int64(len(templateVariableCatalog())) || next == "" {
		t.Fatalf("variable page: %v", err)
	}
	base.Page.Token = next
	if _, _, _, err := service.ListTemplateVariables(ctx, owner, base); err != nil {
		t.Fatal(err)
	}
	base.TemplateContext = &query.TemplateVariableContext{AgentRef: agentRef}
	if _, _, _, err := service.ListTemplateVariables(ctx, owner, base); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("context cursor reuse: %v", err)
	}
	base.Page.Token = ""
	base.TemplateContext = &query.TemplateVariableContext{AgentRef: "agt_wrongcontext", RuntimeRevisionRef: runtimeRef}
	if _, _, _, err := service.ListTemplateVariables(ctx, owner, base); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("wrong runtime agent: %v", err)
	}
	reader := contextProjectReader(t, ctx, repository, service, owner, projectRef, "VARIABLES")
	base.TemplateContext = &query.TemplateVariableContext{RuntimeRevisionRef: runtimeRef}
	if _, _, _, err := service.ListTemplateVariables(ctx, reader, base); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("runtime without run.view: %v", err)
	}
}
