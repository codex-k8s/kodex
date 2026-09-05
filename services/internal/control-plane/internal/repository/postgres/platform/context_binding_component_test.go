package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testContextBinding(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, resourceRef, revisionRef, key string, memory bool) {
	t.Helper()
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, key+"-agent", "Context consumer")
	bindKind, unbindKind := command.BindAgentSkillBundle, command.UnbindAgentSkillBundle
	if memory {
		bindKind, unbindKind = command.BindAgentMemoryRecord, command.UnbindAgentMemoryRecord
	}
	invoke := func(kind command.Kind, suffix string, agentVersion, bindingVersion int64) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + suffix, ExpectedVersion: &agentVersion}, Payload: command.AgentContextBindingInput{AgentRef: agent.Ref, ResourceRef: resourceRef, RevisionRef: revisionRef, ExpectedBindingVersion: bindingVersion}})
	}
	read := func(want int, version int64) []entity.AgentContextBinding {
		view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
		if err != nil {
			t.Fatalf("binding readback: %v", err)
		}
		bindings := view.SkillBindings
		if memory {
			bindings = view.MemoryBindings
		}
		if len(bindings) != want || view.AgentVersion != version {
			t.Fatalf("binding readback count/version: %d/%d expected %d/%d", len(bindings), view.AgentVersion, want, version)
		}
		folder := "skills"
		if memory {
			folder = "memories"
		}
		parent := "/projects/" + projectRef + "/entities/agents/" + agent.Ref
		directories, total, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ResourceRef: parent, Page: query.Page{Size: 1}})
		if err != nil || total != int64(want) || len(directories) != want {
			t.Fatalf("context VFS applicable directories: count=%d total=%d err=%v", len(directories), total, err)
		}
		nodes, total, _, err := service.ListVFSNodes(ctx, owner, query.Filter{ResourceRef: parent + "/" + folder, Page: query.Page{Size: 1}})
		if err != nil || total != int64(want) || len(nodes) != want {
			t.Fatalf("context VFS bindings: count=%d total=%d err=%v", len(nodes), total, err)
		}
		if want == 1 && (nodes[0].Ref != "context-binding:"+bindings[0].Ref || nodes[0].EntityRef != resourceRef || nodes[0].Digest != bindings[0].Digest) {
			t.Fatal("VFS binding did not preserve exact revision digest")
		}
		return bindings
	}
	read(0, agent.Version)
	bound, err := invoke(bindKind, "-bind", agent.Version, 0)
	if err != nil || bound.ContextBinding == nil {
		t.Fatalf("bind: %v", err)
	}
	bindings := read(1, agent.Version+1)
	if bindings[0].Ref != bound.ContextBinding.Ref || bindings[0].RevisionRef != revisionRef || bindings[0].Digest == "" {
		t.Fatal("binding projection mismatch")
	}
	if _, err := invoke(unbindKind, "-stale-agent", agent.Version, bound.ContextBinding.Version); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale agent binding: %v", err)
	}
	if _, err := invoke(unbindKind, "-stale-binding", agent.Version+1, 0); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale binding: %v", err)
	}
	unbound, err := invoke(unbindKind, "-unbind", agent.Version+1, bound.ContextBinding.Version)
	if err != nil || unbound.ContextBinding == nil {
		t.Fatalf("unbind: %v", err)
	}
	read(0, agent.Version+2)
	rebound, err := invoke(bindKind, "-rebind", agent.Version+2, 0)
	if err != nil || rebound.ContextBinding == nil || rebound.ContextBinding.Version <= unbound.ContextBinding.Version {
		t.Fatalf("rebind disabled: %v", err)
	}
	read(1, agent.Version+3)
	if memory {
		reader := contextProjectReader(t, ctx, repository, service, owner, projectRef, "MEMORY")
		items, total, _, err := service.ListMemoryRecords(ctx, reader, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 1}})
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("project reader memory catalog: count=%d total=%d err=%v", len(items), total, err)
		}
		items, total, _, err = service.ListMemoryRecords(ctx, reader, query.Filter{ProjectRef: projectRef, ResourceRef: agent.Ref, Page: query.Page{Size: 1}})
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("hidden agent binding disclosed: count=%d total=%d err=%v", len(items), total, err)
		}
		nodes, total, _, err := service.SearchVFS(ctx, reader, query.Filter{Query: agent.Ref, Page: query.Page{Size: 1}})
		if err != nil || total != 0 || len(nodes) != 0 {
			t.Fatalf("hidden agent VFS subtree disclosed: count=%d total=%d err=%v", len(nodes), total, err)
		}
	}
}
