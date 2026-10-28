package platform

import (
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// serverRetrySnapshotFixture separates a logical anchor, technical final and fresh business anchor;
// historical model success is intentionally independent of the public predicate-accepted flag.
// serverRetrySnapshotFixture разделяет logical anchor, technical final и новую business anchor;
// исторический успех модели намеренно не зависит от публичного predicate-accepted.
func serverRetrySnapshotFixture(previous int32, accepted bool) (*entity.RunGraph, map[string]entity.Run) {
	origin := entity.RunNode{Ref: "nod_origin01", ParentNodeRef: "nod_root0001", Type: "AGENT_EXECUTION", AgentRef: "agt_fixture1", Attempt: 1, State: "SUCCEEDED"}
	anchors := []entity.RunNode{}
	runs := map[string]entity.Run{"run_root0001": {Ref: "run_root0001", RootRunRef: "run_root0001", ProjectRef: "prj_fixture1", Target: entity.RunTarget{Type: "WORKFLOW", Ref: "wfl_fixture1"}, State: "CANCELLED"}}
	for attempt := int32(1); attempt <= previous; attempt++ {
		ref := "run_history" + strconv.FormatInt(int64(attempt), 10)
		if attempt == previous {
			ref = "run_previous"
		}
		origin.WorkflowResults = append(origin.WorkflowResults, entity.WorkflowAttemptResult{Attempt: attempt, RunRef: ref, Accepted: accepted})
		anchor := entity.RunNode{Ref: "nod_history" + strconv.FormatInt(int64(attempt), 10), RunRef: ref, ParentNodeRef: "nod_root0001", Type: "AGENT_EXECUTION", AgentRef: "agt_fixture1", Attempt: 1, State: "SUCCEEDED"}
		if attempt == 1 {
			anchor.Ref = origin.Ref
		}
		anchors = append(anchors, anchor)
		runs[ref] = entity.Run{Ref: ref, RootRunRef: "run_root0001", ParentRunRef: "run_root0001", ProjectRef: "prj_fixture1", Target: entity.RunTarget{Type: "AGENT", Ref: "agt_fixture1"}, Source: "AGENT_DELEGATION", Attempt: 1, State: "SUCCEEDED"}
	}
	origin.RunRef = origin.WorkflowResults[0].RunRef
	middle := entity.RunNode{Ref: "nod_middle01", RunRef: "run_previous", ParentNodeRef: anchors[len(anchors)-1].Ref, Type: "AGENT_EXECUTION", AgentRef: "agt_fixture1", Attempt: 2, State: "SUCCEEDED"}
	old := entity.RunNode{Ref: "nod_previous", RunRef: "run_previous", ParentNodeRef: middle.Ref, Type: "AGENT_EXECUTION", AgentRef: "agt_fixture1", Attempt: 3, State: "SUCCEEDED"}
	fresh := entity.RunNode{Ref: "nod_current1", RunRef: "run_current1", ParentNodeRef: "nod_root0001", Type: "AGENT_EXECUTION", AgentRef: "agt_fixture1", Attempt: 1, State: "CANCELLED"}
	proof := func(attempt int32) *entity.WorkflowRetryReadProof {
		return &entity.WorkflowRetryReadProof{WorkflowVersionRef: "wfv_fixture1", WorkflowVersionDigest: strings.Repeat("a", 64), OriginNodeRef: origin.Ref, StepKey: "work", PreviousBusinessAttempt: attempt - 1, BusinessAttempt: attempt}
	}
	graph := &entity.RunGraph{RunRef: "run_root0001", Nodes: []entity.RunNode{{Ref: "nod_root0001", RunRef: "run_root0001", Type: "ROOT_PROCESS", State: "CANCELLED"}, origin, old, fresh, middle}, Edges: []entity.RunEdge{
		{Ref: "edg_retry001", RunRef: "run_root0001", SourceNodeRef: old.Ref, TargetNodeRef: fresh.Ref, Type: "RETRY_OF", ServerWorkflowRetry: proof(previous + 1)},
		{Ref: "edg_cont0001", RunRef: "run_root0001", SourceNodeRef: middle.ParentNodeRef, TargetNodeRef: middle.Ref, Type: "CONTINUES"},
		{Ref: "edg_cont0002", RunRef: "run_root0001", SourceNodeRef: middle.Ref, TargetNodeRef: old.Ref, Type: "CONTINUES"},
	}}
	for index := 1; index < len(anchors); index++ {
		graph.Nodes = append(graph.Nodes, anchors[index])
		graph.Edges = append(graph.Edges, entity.RunEdge{Ref: "edg_prior00" + strconv.Itoa(index), RunRef: graph.RunRef, SourceNodeRef: anchors[index-1].Ref, TargetNodeRef: anchors[index].Ref, Type: "RETRY_OF", ServerWorkflowRetry: proof(int32(index + 1))})
	}
	runs[fresh.RunRef] = entity.Run{Ref: fresh.RunRef, RootRunRef: graph.RunRef, ParentRunRef: graph.RunRef, ProjectRef: "prj_fixture1", Target: entity.RunTarget{Type: "AGENT", Ref: old.AgentRef}, Source: "AGENT_DELEGATION", Attempt: 1, State: "CANCELLED"}
	return graph, runs
}

// TestServerWorkflowRetrySnapshotProof checks business attempts two through four, accepted/rejected
// history and retained terminal runs, then rejects unrelated graph facts or absent/ambiguous proofs.
// TestServerWorkflowRetrySnapshotProof проверяет business attempts2..4, accepted/rejected history
// и terminal runs, затем отклоняет чужие graph facts и отсутствующие/неоднозначные proofs.
func TestServerWorkflowRetrySnapshotProof(t *testing.T) {
	for previous := int32(1); previous <= 3; previous++ {
		for _, accepted := range []bool{false, true} {
			graph, runs := serverRetrySnapshotFixture(previous, accepted)
			if _, valid := graphRetryPredecessors(graph, runs); !valid {
				t.Fatal("owner-proved business retry with technical final was rejected")
			}
		}
	}
	for name, mutate := range map[string]func(*entity.RunGraph, map[string]entity.Run){
		"missing technical edge": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges = g.Edges[:2] },
		"duplicate technical edge": func(g *entity.RunGraph, _ map[string]entity.Run) {
			edge := g.Edges[2]
			edge.Ref = "edg_duplicate1"
			g.Edges = append(g.Edges, edge)
		},
		"skipped technical attempt": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[2].Attempt = 5 },
		"technical parent is root":  func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[2].ParentNodeRef = "nod_root0001" },
		"foreign technical run":     func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[4].RunRef = "run_current1" },
		"foreign technical agent":   func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[4].AgentRef = "agt_foreign1" },
		"missing proof":             func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].ServerWorkflowRetry = nil },
		"wrong origin": func(g *entity.RunGraph, _ map[string]entity.Run) {
			g.Edges[0].ServerWorkflowRetry.OriginNodeRef = "nod_foreign1"
		},
		"wrong root": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].RunRef = "run_foreign1" },
		"wrong step syntax": func(g *entity.RunGraph, _ map[string]entity.Run) {
			g.Edges[0].ServerWorkflowRetry.StepKey = "other.step"
		},
		"wrong attempt":     func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].ServerWorkflowRetry.BusinessAttempt = 4 },
		"oversized attempt": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].ServerWorkflowRetry.BusinessAttempt = 5 },
		"wrong digest": func(g *entity.RunGraph, _ map[string]entity.Run) {
			g.Edges[0].ServerWorkflowRetry.WorkflowVersionDigest = strings.Repeat("G", 64)
		},
		"wrong type":            func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].Type = "CONTINUES" },
		"wrong agent":           func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[3].AgentRef = "agt_foreign1" },
		"wrong parent":          func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[3].ParentNodeRef = "nod_foreign1" },
		"wrong runtime attempt": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[3].Attempt = 2 },
		"missing journal":       func(g *entity.RunGraph, _ map[string]entity.Run) { g.Nodes[1].WorkflowResults = nil },
		"journal run": func(g *entity.RunGraph, _ map[string]entity.Run) {
			g.Nodes[1].WorkflowResults[0].RunRef = "run_foreign1"
		},
		"duplicate journal": func(g *entity.RunGraph, _ map[string]entity.Run) {
			g.Nodes[1].WorkflowResults = append(g.Nodes[1].WorkflowResults, g.Nodes[1].WorkflowResults[0])
		},
		"duplicate edge": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges = append(g.Edges, g.Edges[0]) },
		"foreign project": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_current1"]
			v.ProjectRef = "prj_foreign1"
			r[v.Ref] = v
		},
		"foreign lineage": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_current1"]
			v.RootRunRef = "run_foreign1"
			r[v.Ref] = v
		},
		"fake ordinary retry": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_current1"]
			v.RetryOfRunRef = "run_previous"
			r[v.Ref] = v
		},
		"wrong target": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_current1"]
			v.Target.Ref = "agt_foreign1"
			r[v.Ref] = v
		},
	} {
		t.Run(name, func(t *testing.T) {
			graph, runs := serverRetrySnapshotFixture(1, false)
			mutate(graph, runs)
			if _, valid := graphRetryPredecessors(graph, runs); valid {
				t.Fatal("unproved server business retry was accepted")
			}
		})
	}
}
