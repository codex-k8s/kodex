package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestGraphRetrySnapshotExactLineage accepts only the exact canonical retry predecessor/edge graph
// and rejects missing, foreign, duplicate, mismatched or cyclic lineage.
// TestGraphRetrySnapshotExactLineage принимает только точный канонический граф предшественника и
// ребра повтора и отклоняет отсутствующие, чужие, дублированные, несовпадающие и циклические связи
// происхождения.
func TestGraphRetrySnapshotExactLineage(t *testing.T) {
	fixture := func() (*entity.RunGraph, map[string]entity.Run) {
		return &entity.RunGraph{Nodes: []entity.RunNode{
			{Ref: "nod_previous", RunRef: "run_previous", Type: "ROOT_PROCESS"},
			{Ref: "nod_current1", RunRef: "run_current1", Type: "ROOT_PROCESS"},
		}, Edges: []entity.RunEdge{{RunRef: "run_current1", SourceNodeRef: "nod_previous", TargetNodeRef: "nod_current1", Type: "RETRY_OF"}}}, map[string]entity.Run{
			"run_previous": {Ref: "run_previous", RootRunRef: "run_previous", State: "CANCELLED"},
			"run_current1": {Ref: "run_current1", RootRunRef: "run_current1", RetryOfRunRef: "run_previous"},
		}
	}
	graph, runs := fixture()
	if old, ok := graphRetryPredecessors(graph, runs); !ok || !old["run_previous"] {
		t.Fatal("exact cancelled predecessor rejected")
	}
	for name, mutate := range map[string]func(*entity.RunGraph, map[string]entity.Run){
		"missing edge":       func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges = nil },
		"foreign node":       func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].SourceNodeRef = "nod_foreign1" },
		"foreign owner edge": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges[0].RunRef = "run_foreign1" },
		"missing owner run":  func(_ *entity.RunGraph, r map[string]entity.Run) { delete(r, "run_previous") },
		"pin mismatch": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_current1"]
			v.RetryOfRunRef = "run_foreign1"
			r[v.Ref] = v
		},
		"noncanonical": func(_ *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_previous"]
			v.RootRunRef = "run_foreign1"
			r[v.Ref] = v
		},
		"duplicate edge": func(g *entity.RunGraph, _ map[string]entity.Run) { g.Edges = append(g.Edges, g.Edges[0]) },
		"cycle": func(g *entity.RunGraph, r map[string]entity.Run) {
			v := r["run_previous"]
			v.RetryOfRunRef = "run_current1"
			r[v.Ref] = v
			g.Edges = append(g.Edges, entity.RunEdge{RunRef: "run_previous", SourceNodeRef: "nod_current1", TargetNodeRef: "nod_previous", Type: "RETRY_OF"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			g, r := fixture()
			mutate(g, r)
			if _, ok := graphRetryPredecessors(g, r); ok {
				t.Fatal("unproven retry lineage accepted")
			}
		})
	}
}

// TestGraphContinuationSnapshotExactLineage requires a succeeded predecessor in the same nonempty
// Session with exact root nodes and one owner-bound CONTINUES edge, rejecting retry or foreign
// lineage substitutions.
// TestGraphContinuationSnapshotExactLineage требует успешного предшественника той же непустой
// Session с точными корневыми узлами и одним принадлежащим владельцу ребром CONTINUES, отклоняя
// подмену повтором либо чужим происхождением.
func TestGraphContinuationSnapshotExactLineage(t *testing.T) {
	fixture := func() (*entity.RunGraph, map[string]entity.Run) {
		return &entity.RunGraph{Nodes: []entity.RunNode{
			{Ref: "nod_previous", RunRef: "run_previous", Type: "ROOT_PROCESS"},
			{Ref: "nod_current1", RunRef: "run_current1", Type: "ROOT_PROCESS"},
		}, Edges: []entity.RunEdge{{RunRef: "run_current1", SourceNodeRef: "nod_previous", TargetNodeRef: "nod_current1", Type: "CONTINUES"}}}, map[string]entity.Run{
			"run_previous": {Ref: "run_previous", RootRunRef: "run_previous", SessionRef: "ses_exact001", State: "SUCCEEDED"},
			"run_current1": {Ref: "run_current1", RootRunRef: "run_current1", SessionRef: "ses_exact001"},
		}
	}
	g, r := fixture()
	if old, ok := graphRetryPredecessors(g, r); !ok || !old["run_previous"] {
		t.Fatal("exact continuation predecessor rejected")
	}
	for _, name := range []string{"session", "empty session", "state", "root", "retry", "node", "edge", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			g, r := fixture()
			old, fresh := r["run_previous"], r["run_current1"]
			switch name {
			case "session":
				fresh.SessionRef = "ses_foreign1"
			case "empty session":
				old.SessionRef, fresh.SessionRef = "", ""
			case "state":
				old.State = "RUNNING"
			case "root":
				old.RootRunRef = "run_foreign1"
			case "retry":
				fresh.RetryOfRunRef = old.Ref
			case "node":
				g.Nodes[0].Type = "AGENT_EXECUTION"
			case "edge":
				g.Edges[0].RunRef = "run_foreign1"
			case "duplicate":
				g.Edges = append(g.Edges, g.Edges[0])
			}
			r[old.Ref], r[fresh.Ref] = old, fresh
			if _, ok := graphRetryPredecessors(g, r); ok {
				t.Fatal("unproven continuation accepted")
			}
		})
	}
}
