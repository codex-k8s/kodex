package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testContextBinding(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, resourceRef, revisionRef, key string, memory bool) {
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
		return bindings
	}
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
}
