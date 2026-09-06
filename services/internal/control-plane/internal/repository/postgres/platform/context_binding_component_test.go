package platform

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testContextBinding(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, resourceRef, revisionRef, key string, memory bool) {
	t.Helper()
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, key+"-agent", "Context consumer")
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key + "-projection-run"}, Payload: command.LaunchRunInput{
			ProjectRef: projectRef, Title: "Typed context projection", Task: "Resolve immutable context", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}})
	if err != nil || run.Run == nil {
		t.Fatalf("context projection run: %v", err)
	}
	defer func() {
		currentRun, err := service.GetRun(ctx, owner, run.Run.Ref)
		if err != nil {
			t.Errorf("read context fixture for cleanup: %v", err)
			return
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + "-projection-cancel", ExpectedVersion: &currentRun.Version}, Payload: command.RunCommandInput{RunRef: run.Run.Ref, Reason: "Context fixture cleanup"}})
		if err != nil {
			t.Errorf("cancel context projection run: %v", err)
		}
	}()
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
		parent := "/projects/" + projectRef + "/agents/" + agent.Ref
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
		resolved, err := repository.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		current, err := repository.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := repository.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		snapshot, err := repository.runtimeContextSnapshot(ctx, tx, current, run.Run.Ref, projectRef, agent.Ref)
		if err != nil {
			t.Fatalf("runtime context projection: %v", err)
		}
		count := len(snapshot.Skills)
		if memory {
			count = len(snapshot.Memories)
		}
		if count != want || snapshot.ValidateFor(runtimecontract.RunnerInput{OrganizationRef: current.organizationRef, ProjectRef: projectRef, AgentRef: agent.Ref}, time.Now()) != nil {
			t.Fatalf("runtime context count or lineage: %d expected %d", count, want)
		}
		if want == 1 && memory && (snapshot.Memories[0].RevisionRef != revisionRef || snapshot.Memories[0].BindingRef != bindings[0].Ref || snapshot.Memories[0].Summary == "") {
			t.Fatal("runtime memory lost exact pin or summary")
		}
		if want == 1 && !memory && (snapshot.Skills[0].RevisionRef != revisionRef || snapshot.Skills[0].BindingRef != bindings[0].Ref || len(snapshot.Skills[0].Files) == 0) {
			t.Fatal("runtime skill lost exact pin or files")
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
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	reader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read",
	}, "runtime-controller")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-runtime-claim"}, Payload: command.LeaseInput{WorkloadInstance: key + "-worker", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != run.Run.Ref {
		t.Fatalf("claim typed context: count=%d err=%v", len(claimed.RuntimeItems), err)
	}
	claim := claimed.RuntimeItems[0]
	snapshot, ok := claim["contextSnapshot"].(runtimecontract.RuntimeContextSnapshot)
	if !ok || memory && len(snapshot.Memories) != 1 || !memory && len(snapshot.Skills) != 1 {
		t.Fatal("runtime claim omitted typed context snapshot")
	}
	readSkill := func(want bool) {
		t.Helper()
		if memory {
			return
		}
		testRuntimeSkillFileManifest(t, ctx, repository, service, claim, want)
		file := snapshot.Skills[0].Files[0]
		download, err := service.ReadExecutionArtifact(ctx, reader, stringMap(claim, "leaseRef"), stringMap(claim, "fence"), runtimeRevisionMapInt64(claim, "generation"), file.ArtifactRef)
		if !want {
			if err == nil {
				_ = download.Reader.Close()
				t.Fatal("revoked context pin remained readable")
			}
			return
		}
		if err != nil {
			t.Fatalf("fenced Skill file download: %v", err)
		}
		body, readErr := io.ReadAll(download.Reader)
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil || int64(len(body)) != file.SizeBytes || download.Artifact.Digest != file.Digest {
			t.Fatal("fenced Skill file body/pin mismatch")
		}
	}
	readSkill(true)
	if !memory {
		func() {
			tx, err := repository.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			for _, state := range []string{"ARCHIVED", "PURGED"} {
				if _, err := tx.Exec(ctx, `UPDATE control_plane.skill_bundles SET state=$2,version=version+1 WHERE ref=$1`, resourceRef, state); err != nil {
					t.Fatalf("simulate removed Skill catalog in rollback fixture: %v", err)
				}
			}
			file := snapshot.Skills[0].Files[0]
			var references int64
			if err := tx.QueryRow(ctx, `SELECT control_plane.skill_artifact_reference_count($1::uuid,$2,$3,$4)`,
				owner.AuthorityTenant, file.ArtifactRef, file.ArtifactRevision, file.Digest).Scan(&references); err != nil || references != 1 {
				t.Fatalf("active runtime pin did not retain removed Skill file: references=%d err=%v", references, err)
			}
		}()
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
	readSkill(false)
	rebound, err := invoke(bindKind, "-rebind", agent.Version+2, 0)
	if err != nil || rebound.ContextBinding == nil || rebound.ContextBinding.Version <= unbound.ContextBinding.Version {
		t.Fatalf("rebind disabled: %v", err)
	}
	read(1, agent.Version+3)
	readSkill(false)
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
