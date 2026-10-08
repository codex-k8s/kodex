package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRootRunCursorComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, payload any, version *int64) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "root-cursor-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Root cursor", Language: "en"}, nil).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "root-cursor-manager", "Manager")
	manager = *execute(command.ChangeAgentCapability, owner, "delegate-capability", command.AgentBindingInput{AgentRef: manager.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &manager.Version).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "root-cursor-specialist", "Specialist")
	root := execute(command.LaunchRun, owner, "launch", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Delegate two bounded tasks."}, nil).Run
	claimed := execute(command.ClaimExecution, worker, "claim", command.LeaseInput{WorkloadInstance: "root-cursor-worker", Limit: 1}, nil).RuntimeItems
	if len(claimed) != 1 || stringMap(claimed[0], "runRef") != root.Ref {
		t.Fatal("exact root lease missing")
	}
	lease := claimed[0]
	children := make([]entity.Run, 0, 2)
	for _, key := range []string{"first", "second"} {
		delegated := execute(command.DelegateExecution, worker, key, command.DelegateInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), TargetAgentRef: specialist.Ref, Task: "Bounded " + key}, nil)
		if delegated.Run == nil || delegated.Graph == nil {
			t.Fatal("delegation snapshot missing")
		}
		currentRoot, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if delegated.Graph.RunRef != root.Ref || delegated.Graph.Sequence != currentRoot.EventSequence || delegated.Graph.Revision != currentRoot.GraphRevision || delegated.Run.Ref == root.Ref || delegated.Run.RootRunRef != root.Ref || delegated.Run.EventSequence != 0 {
			t.Fatalf("delegation mixed child identity and root cursor: child=%s own=%d graph=%d/%d root=%d/%d", delegated.Run.Ref, delegated.Run.EventSequence, delegated.Graph.Sequence, delegated.Graph.Revision, currentRoot.EventSequence, currentRoot.GraphRevision)
		}
		children = append(children, *delegated.Run)
	}
	assertRead := func(t *testing.T, child entity.Run) int64 {
		t.Helper()
		currentRoot, rootGraph, err := service.GetRunGraph(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal(err)
		}
		read, graph, err := service.GetRunGraph(ctx, owner, child.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if read.Ref != child.Ref || read.SessionRef != child.SessionRef || read.ParentRunRef != child.ParentRunRef || read.Version != child.Version || read.State != child.State || read.EventSequence != child.EventSequence || read.GraphRevision != child.GraphRevision {
			t.Fatal("root cursor changed the child Run projection")
		}
		if graph.RunRef != root.Ref || graph.Sequence != rootGraph.Sequence || graph.Revision != rootGraph.Revision || graph.Sequence != currentRoot.EventSequence || graph.Sequence <= 0 {
			t.Fatal("child graph did not use the exact root snapshot cursor")
		}
		for _, limit := range []int64{500, graph.Sequence, 1} {
			after := int64(0)
			for {
				events, sequence, complete, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: child.Ref, AfterSequence: after, Limit: int32(limit)})
				if err != nil || sequence != graph.Sequence || len(events) == 0 {
					t.Fatalf("root event page cursor: limit=%d after=%d current=%d want=%d err=%v", limit, after, sequence, graph.Sequence, err)
				}
				for _, event := range events {
					if event.RunRef != root.Ref || event.Sequence != after+1 {
						t.Fatal("event page lost canonical root ordering")
					}
					after = event.Sequence
				}
				if complete != (after == graph.Sequence) {
					t.Fatal("event page completeness does not match the root cursor")
				}
				if complete {
					break
				}
			}
		}
		tail, sequence, complete, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: child.Ref, AfterSequence: graph.Sequence, Limit: 1})
		if err != nil || len(tail) != 0 || sequence != graph.Sequence || !complete {
			t.Fatal("empty tail lost the root cursor/completeness")
		}
		return graph.Sequence
	}
	t.Run("child-and-sibling-root-pages", func(t *testing.T) {
		for _, child := range children {
			assertRead(t, child)
		}
		if children[0].SessionRef == children[1].SessionRef || children[0].Ref == children[1].Ref {
			t.Fatal("siblings lost independent execution identity")
		}
	})
	t.Run("foreign-project-denied", func(t *testing.T) {
		foreign := execute(command.CreateProject, owner, "foreign-project", command.ProjectInput{Name: "Foreign root cursor", Language: "en"}, nil).Project
		signed := owner
		signed.ProjectRef = gateTestProjectID(t, ctx, r, owner, foreign.Ref)
		for _, child := range children {
			if _, _, err := service.GetRunGraph(ctx, signed, child.Ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("foreign project read child graph: %v", err)
			}
			if _, _, _, err := service.ListRunEvents(ctx, signed, query.Filter{ResourceRef: child.Ref, Limit: 1}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("foreign project read child events: %v", err)
			}
		}
	})
	t.Run("exact-child-eligibility", func(t *testing.T) {
		reader := contextProjectReader(t, ctx, r, service, owner, project.Ref, "root-cursor-reader")
		actor, err := r.ResolvePrincipal(ctx, reader)
		if err != nil {
			t.Fatal(err)
		}
		readerScope, err := r.resolveScope(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		var membershipPermissions []string
		if err := pool.QueryRow(ctx, queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef,
			readerScope.organizationID, project.Ref, readerScope.actorID).Scan(&membershipPermissions); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("exact-resource fixture unexpectedly has membership presentation: %v", err)
		}
		assertOpenOnly := func(actions []string) {
			t.Helper()
			if len(actions) != 1 || actions[0] != "OPEN" {
				t.Fatalf("read-only projection gained mutation actions: %v", actions)
			}
		}
		assertPublicRead := func(ref string) {
			t.Helper()
			run, graph, err := service.GetRunGraph(ctx, reader, ref)
			if err != nil || run.Ref != ref || graph.RunRef != root.Ref || graph.Sequence <= 0 {
				t.Fatalf("exact-resource public graph denied or mixed identity: %v", err)
			}
			assertOpenOnly(run.NextActions)
			for _, node := range graph.Nodes {
				assertOpenOnly(node.NextActions)
			}
			events, sequence, complete, err := service.ListRunEvents(ctx, reader, query.Filter{ResourceRef: ref, Limit: 500})
			if err != nil || sequence != graph.Sequence || len(events) == 0 || !complete {
				t.Fatalf("exact-resource public events denied or mixed root cursor: %v", err)
			}
			for _, event := range events {
				if event.Delta.Run != nil {
					assertOpenOnly(event.Delta.Run.NextActions)
				}
				if event.Delta.Node != nil {
					assertOpenOnly(event.Delta.Node.NextActions)
				}
				if event.Delta.Gate != nil && len(event.Delta.Gate.NextActions) != 0 {
					t.Fatal("read-only event gained gate actions")
				}
			}
		}
		readEligible := func(ref string) (entity.Run, entity.RunGraph, error) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			run, err := r.readRunWithIncidents(ctx, tx, readerScope, ref)
			if err != nil {
				return entity.Run{}, entity.RunGraph{}, err
			}
			graph, err := readRootRunGraphCursor(ctx, tx, readerScope, run)
			return run, graph, err
		}
		assertDenied := func(ref string) {
			t.Helper()
			if _, _, err := readEligible(ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("exact requested Run eligibility bypassed: %v", err)
			}
			if _, _, err := service.GetRunGraph(ctx, reader, ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("exact run eligibility bypassed for graph: %v", err)
			}
			if _, _, _, err := service.ListRunEvents(ctx, reader, query.Filter{ResourceRef: ref, Limit: 1}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("exact run eligibility bypassed for events: %v", err)
			}
		}
		rootBinding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "root-cursor-exact-root", []string{"run.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "RUN", ResourceRef: root.Ref})
		if _, _, err := readEligible(root.Ref); err != nil {
			t.Fatalf("root-only reader lost its exact eligibility: %v", err)
		}
		assertPublicRead(root.Ref)
		assertDenied(children[0].Ref)
		execute(command.RevokeAccessBinding, owner, "revoke-root-reader", command.AccessBindingInput{BindingRef: rootBinding.Ref}, &rootBinding.Version)
		assertDenied(root.Ref)
		childBinding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "root-cursor-exact-child", []string{"run.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "RUN", ResourceRef: children[0].Ref})
		assertDenied(root.Ref)
		assertDenied(children[1].Ref)
		assertPublicRead(children[0].Ref)
		foreign := reader
		foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000001"
		if _, _, err := service.GetRunGraph(ctx, foreign, children[0].Ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("foreign organization gained graph: %v", err)
		}
		if _, _, _, err := service.ListRunEvents(ctx, foreign, query.Filter{ResourceRef: children[0].Ref, Limit: 1}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("foreign organization gained events: %v", err)
		}
		otherProject := execute(command.CreateProject, owner, "reader-foreign-project", command.ProjectInput{Name: "Reader foreign project", Language: "en"}, nil).Project
		signed := reader
		signed.ProjectRef = gateTestProjectID(t, ctx, r, owner, otherProject.Ref)
		if _, _, err := service.GetRunGraph(ctx, signed, children[0].Ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("foreign signed scope gained graph: %v", err)
		}
		if _, _, _, err := service.ListRunEvents(ctx, signed, query.Filter{ResourceRef: children[0].Ref, Limit: 1}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("foreign signed scope gained events: %v", err)
		}
		child, graph, err := readEligible(children[0].Ref)
		if err != nil || child.Ref != children[0].Ref || graph.RunRef != root.Ref || graph.Sequence <= 0 {
			t.Fatalf("child-only reader gained an extra root eligibility gate: %v", err)
		}
		if len(child.NextActions) != 1 || child.NextActions[0] != "OPEN" {
			t.Fatal("root cursor gave a read-only child extra mutation permissions")
		}
		execute(command.RevokeAccessBinding, owner, "revoke-child-reader", command.AccessBindingInput{BindingRef: childBinding.Ref}, &childBinding.Version)
		assertDenied(children[0].Ref)
	})
	t.Run("trusted-lineage-and-repeatable-snapshot", func(t *testing.T) {
		resolved, err := r.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		current, err := r.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		child, err := r.readRunWithIncidents(ctx, tx, current, children[0].Ref)
		if err != nil {
			t.Fatal(err)
		}
		before, err := readRootRunGraphCursor(ctx, tx, current, child)
		if err != nil {
			t.Fatal(err)
		}
		wrong := child
		wrong.RootRunRef = children[1].Ref
		if _, err := readRootRunGraphCursor(ctx, tx, current, wrong); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("mismatched root lineage accepted")
		}
		foreign := current
		foreign.organizationID = "90000000-0000-4000-8000-000000000001"
		if _, err := readRootRunGraphCursor(ctx, tx, foreign, child); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("foreign organization root accepted")
		}
		// Owner-команда фиксирует событие в другой транзакции после создания
		// snapshot. Тот же RR reader не должен смешивать старые строки и новый cursor.
		toolWorker := worker
		toolWorker.Permission = "platform.runtime.tool-call.record"
		execute(command.RecordRunToolCall, toolWorker, "concurrent-event", command.RunToolCallInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), CallRef: "tcl_root_cursor_snapshot", Tool: "delegate_agent", CapabilityRef: "platform.run.delegate", State: "RUNNING", Revision: 1, SafeParameters: map[string]any{}}, nil)
		after, err := readRootRunGraphCursor(ctx, tx, current, child)
		if err != nil || after.Sequence != before.Sequence || after.Revision != before.Revision {
			t.Fatal("root cursor escaped the RepeatableRead snapshot")
		}
		fresh, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil || fresh.EventSequence <= before.Sequence {
			t.Fatal("concurrent writer did not advance the authoritative root")
		}
	})
	t.Run("terminal-cursor", func(t *testing.T) {
		current, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.CancelRun, owner, "cancel", command.RunCommandInput{RunRef: root.Ref}, &current.Version)
		for _, child := range children {
			terminal, err := service.GetRun(ctx, owner, child.Ref)
			if err != nil || terminal.State != "CANCELLED" {
				t.Fatal("child did not enter authoritative terminal state")
			}
			first := assertRead(t, terminal)
			if second := assertRead(t, terminal); second != first {
				t.Fatal("read changed the terminal root cursor")
			}
		}
	})
}
