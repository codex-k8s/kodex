package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

type runGraphCursorFixture struct {
	t                  *testing.T
	ref                string
	revision, sequence int64
	failure            error
	called             bool
}

func (fixture *runGraphCursorFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected graph cursor query")
}

func (fixture *runGraphCursorFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	fixture.t.Helper()
	fixture.called = true
	if sql != queryRunGraphCursor || len(args) != 1 {
		fixture.t.Fatal("root cursor did not use the narrow owner query")
	}
	pins, ok := args[0].(pgx.StrictNamedArgs)
	if !ok || len(pins) != 3 || pins["organization_id"] != "organization-fixture" || pins["run_ref"] != "run_child" || pins["root_run_ref"] != "run_root" {
		fixture.t.Fatal("root cursor lost exact organization/child/root lineage pins")
	}
	return fixture
}

func (fixture *runGraphCursorFixture) Scan(dest ...any) error {
	if fixture.failure != nil {
		return fixture.failure
	}
	*dest[0].(*string) = fixture.ref
	*dest[1].(*int64) = fixture.revision
	*dest[2].(*int64) = fixture.sequence
	return nil
}

func TestRootRunGraphCursorClosed(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		ref                string
		revision, sequence int64
		failure            error
		valid              bool
	}{
		"confirmed-root":    {ref: "run_root", revision: 67, sequence: 66, valid: true},
		"initial-root":      {ref: "run_root", revision: 1, sequence: 0, valid: true},
		"missing-lineage":   {failure: pgx.ErrNoRows},
		"read-failure":      {failure: errors.New("synthetic read failure")},
		"different-root":    {ref: "run_other", revision: 67, sequence: 66},
		"invalid-revision":  {ref: "run_root", revision: 0, sequence: 66},
		"negative-sequence": {ref: "run_root", revision: 67, sequence: -1},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := &runGraphCursorFixture{t: t, ref: test.ref, revision: test.revision, sequence: test.sequence, failure: test.failure}
			child := entity.Run{Ref: "run_child", RootRunRef: "run_root", Version: 8, SessionRef: "ses_child", ParentRunRef: "run_parent", State: "RUNNING", Attempt: 2, GraphRevision: 1, NextActions: []string{"CANCEL"}}
			graph, err := readRootRunGraphCursor(t.Context(), fixture, scope{organizationID: "organization-fixture"}, child)
			if !fixture.called {
				t.Fatal("child cursor did not resolve authoritative root")
			}
			if !test.valid {
				if !errors.Is(err, errs.ErrUnavailable) || graph.RunRef != "" {
					t.Fatal("invalid lineage/cursor returned a graph")
				}
				return
			}
			if err != nil || graph.RunRef != "run_root" || graph.Revision != test.revision || graph.Sequence != test.sequence || len(graph.Nodes) != 0 || len(graph.Edges) != 0 {
				t.Fatal("root cursor projection is inconsistent")
			}
			if child.Ref != "run_child" || child.Version != 8 || child.SessionRef != "ses_child" || child.ParentRunRef != "run_parent" || child.State != "RUNNING" || child.Attempt != 2 || child.GraphRevision != 1 || child.EventSequence != 0 || len(child.NextActions) != 1 || child.NextActions[0] != "CANCEL" {
				t.Fatal("cursor resolution changed the child Run")
			}
		})
	}
	for name, run := range map[string]entity.Run{
		"root":          {Ref: "run_root", RootRunRef: "run_root", GraphRevision: 67, EventSequence: 66},
		"missing-root":  {Ref: "run_child", GraphRevision: 1},
		"missing-child": {RootRunRef: "run_root", GraphRevision: 1},
		"invalid-root":  {Ref: "run_root", RootRunRef: "run_root", GraphRevision: 0},
		"negative-root": {Ref: "run_root", RootRunRef: "run_root", GraphRevision: 1, EventSequence: -1},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := &runGraphCursorFixture{t: t}
			graph, err := readRootRunGraphCursor(t.Context(), fixture, scope{}, run)
			if fixture.called {
				t.Fatal("root fast path or invalid identity queried another owner")
			}
			if name == "root" {
				if err != nil || graph.RunRef != run.Ref || graph.Revision != run.GraphRevision || graph.Sequence != run.EventSequence {
					t.Fatal("root snapshot changed")
				}
			} else if !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("invalid cursor accepted")
			}
		})
	}
}
