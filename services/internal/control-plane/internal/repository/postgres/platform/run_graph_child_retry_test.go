package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestChildRetryGraphProjection covers the failing child-to-root retry and rejects unrelated edges.
// TestChildRetryGraphProjection проверяет повтор дочернего запуска и отказ для чужих связей.
func TestChildRetryGraphProjection(t *testing.T) {
	fixture := func() (entity.RunGraph, entity.Run, entity.Run) {
		return entity.RunGraph{RunRef: "run_retry001", Nodes: []entity.RunNode{
				{Ref: "nod_previous", RunRef: "run_previous", Type: "ROOT_PROCESS"},
				{Ref: "nod_retry001", RunRef: "run_retry001", Type: "ROOT_PROCESS"},
				{Ref: "nod_agent001", RunRef: "run_retry001", Type: "AGENT_EXECUTION"},
			}, Edges: []entity.RunEdge{
				{Ref: "edg_retry001", RunRef: "run_retry001", SourceNodeRef: "nod_previous", TargetNodeRef: "nod_retry001", Type: "RETRY_OF"},
				{Ref: "edg_agent001", RunRef: "run_retry001", SourceNodeRef: "nod_retry001", TargetNodeRef: "nod_agent001", Type: "DELEGATED_TO"},
			}}, entity.Run{Ref: "run_retry001", RootRunRef: "run_retry001", RetryOfRunRef: "run_child001", ProjectRef: "prj_exact001"},
			entity.Run{Ref: "run_child001", RootRunRef: "run_previous", ProjectRef: "prj_exact001"}
	}
	g, current, previous := fixture()
	if err := projectChildRetryGraph(&g, current, previous); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[0].Type != "DELEGATED_TO" {
		t.Fatal("active graph changed")
	}
	if _, ok := graphRetryPredecessors(&g, map[string]entity.Run{current.Ref: current}); !ok {
		t.Fatal("retry snapshot rejected")
	}
	if err := projectChildRetryGraph(&g, current, previous); err != nil {
		t.Fatal("projection is not idempotent", err)
	}
	for _, name := range []string{"project", "pin", "source", "target", "other edge"} {
		t.Run(name, func(t *testing.T) {
			g, current, previous := fixture()
			switch name {
			case "project":
				previous.ProjectRef = "prj_foreign1"
			case "pin":
				previous.Ref = "run_foreign1"
			case "source":
				g.Nodes[0].RunRef = "run_foreign1"
			case "target":
				g.Nodes[1].RunRef = "run_foreign1"
			case "other edge":
				g.Edges[1].SourceNodeRef = "nod_previous"
			}
			if projectChildRetryGraph(&g, current, previous) == nil {
				t.Fatal("unrelated lineage accepted")
			}
		})
	}
	g, current, previous = fixture()
	previous.RootRunRef = previous.Ref
	if err := projectChildRetryGraph(&g, current, previous); err != nil || len(g.Edges) != 2 {
		t.Fatal("root retry history changed", err)
	}
}
