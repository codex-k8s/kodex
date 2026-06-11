package service

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func TestPrepareBuildContextRecordsSafePendingBlocker(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	input := testPrepareBuildContextInput()
	input.Meta = commandMeta(mustUUID("00000000-0000-0000-0000-000000001101"), 0)

	buildContext, err := svc.PrepareBuildContext(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareBuildContext(): %v", err)
	}
	if buildContext.Status != enum.BuildContextStatusPending ||
		buildContext.LastErrorCode != buildContextSourceSnapshotUnavailableCode ||
		buildContext.NextAction != buildContextSourceSnapshotUnavailableAction {
		t.Fatalf("build context = %#v, want pending source snapshot blocker", buildContext)
	}
	if buildContext.BuildContextRef != "" || buildContext.BuildContextDigest != "" {
		t.Fatalf("build context artifact = %q/%q, want empty before materialization", buildContext.BuildContextRef, buildContext.BuildContextDigest)
	}
	if len(repo.buildContexts) != 1 {
		t.Fatalf("stored build contexts = %d, want 1", len(repo.buildContexts))
	}

	replay, err := svc.PrepareBuildContext(context.Background(), input)
	if err != nil {
		t.Fatalf("replay PrepareBuildContext(): %v", err)
	}
	if replay.ID != buildContext.ID || len(repo.buildContexts) != 1 {
		t.Fatalf("replay = %s, stored = %d; want same aggregate without duplicate", replay.ID, len(repo.buildContexts))
	}
}

func TestPrepareBuildContextReusesFingerprintAcrossCommands(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	firstInput := testPrepareBuildContextInput()
	firstInput.AffectedServiceKeys = []string{"runtime-manager", "access-manager"}
	firstInput.Meta = commandMeta(mustUUID("00000000-0000-0000-0000-000000001102"), 0)
	first, err := svc.PrepareBuildContext(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("first PrepareBuildContext(): %v", err)
	}

	secondInput := testPrepareBuildContextInput()
	secondInput.AffectedServiceKeys = []string{"access-manager", "runtime-manager"}
	secondInput.Meta = commandMeta(mustUUID("00000000-0000-0000-0000-000000001103"), 0)
	second, err := svc.PrepareBuildContext(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("second PrepareBuildContext(): %v", err)
	}
	if second.ID != first.ID || len(repo.buildContexts) != 1 {
		t.Fatalf("second = %s stored = %d, want reused fingerprint %s", second.ID, len(repo.buildContexts), first.ID)
	}
}

func TestBuildContextProgressRequiresCheckedSnapshotBeforeReady(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	input := testPrepareBuildContextInput()
	input.Meta = commandMeta(mustUUID("00000000-0000-0000-0000-000000001104"), 0)
	buildContext, err := svc.PrepareBuildContext(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareBuildContext(): %v", err)
	}

	_, err = svc.ReportBuildContextProgress(context.Background(), ReportBuildContextProgressInput{
		BuildContextID:     buildContext.ID,
		Status:             enum.BuildContextStatusReady,
		BuildContextRef:    "pvc://runtime/build-context-runtime-manager",
		BuildContextDigest: testDigest("b"),
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000001105"), buildContext.Version),
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ReportBuildContextProgress(ready without snapshot) err = %v, want precondition failed", err)
	}

	running, err := svc.ReportBuildContextProgress(context.Background(), ReportBuildContextProgressInput{
		BuildContextID:       buildContext.ID,
		Status:               enum.BuildContextStatusRunning,
		SourceSnapshotRef:    "artifact://provider/source-snapshots/runtime-manager",
		SourceSnapshotDigest: testDigest("c"),
		Meta:                 commandMeta(mustUUID("00000000-0000-0000-0000-000000001106"), buildContext.Version),
	})
	if err != nil {
		t.Fatalf("ReportBuildContextProgress(running): %v", err)
	}
	if running.Status != enum.BuildContextStatusRunning || running.SourceSnapshotRef == "" || running.StartedAt == nil {
		t.Fatalf("running build context = %#v, want running with source snapshot", running)
	}

	ready, err := svc.ReportBuildContextProgress(context.Background(), ReportBuildContextProgressInput{
		BuildContextID:     buildContext.ID,
		Status:             enum.BuildContextStatusReady,
		BuildContextRef:    "pvc://runtime/build-context-runtime-manager",
		BuildContextDigest: testDigest("d"),
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000001107"), running.Version),
	})
	if err != nil {
		t.Fatalf("ReportBuildContextProgress(ready): %v", err)
	}
	if ready.Status != enum.BuildContextStatusReady ||
		ready.BuildContextRef == "" ||
		ready.BuildContextDigest == "" ||
		ready.LastErrorCode != "" ||
		ready.FinishedAt == nil {
		t.Fatalf("ready build context = %#v, want ready safe refs without errors", ready)
	}
}

func testPrepareBuildContextInput() PrepareBuildContextInput {
	return PrepareBuildContextInput{
		ProjectID:            mustUUID("00000000-0000-0000-0000-000000001001"),
		RepositoryID:         mustUUID("00000000-0000-0000-0000-000000001002"),
		Provider:             "github",
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex",
		SourceRef:            "refs/heads/main",
		SourceCommitSHA:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AffectedServiceKeys:  []string{"runtime-manager"},
		BuildPlanFingerprint: testDigest("a"),
	}
}

func testDigest(hexChar string) string {
	return "sha256:" + repeatString(hexChar, 64)
}

func repeatString(value string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += value
	}
	return result
}
