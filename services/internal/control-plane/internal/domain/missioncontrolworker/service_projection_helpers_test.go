package missioncontrolworker

import (
	"testing"
	"time"

	agentrunrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/agentrun"
	enumtypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/enum"
)

func TestTrackRunLineageLinksOlderRunToLatestContour(t *testing.T) {
	t.Parallel()

	lineageState := make(map[string]string)
	latest := agentrunrepo.RunLookupItem{RunID: "run-2", IssueNumber: 542}
	if got := trackRunLineage(lineageState, "codex-k8s/codex-k8s", latest, runProjectionKey(latest.RunID)); got != "" {
		t.Fatalf("trackRunLineage() for latest run = %q, want empty successor", got)
	}

	older := agentrunrepo.RunLookupItem{RunID: "run-1", IssueNumber: 542}
	if got, want := trackRunLineage(lineageState, "codex-k8s/codex-k8s", older, runProjectionKey(older.RunID)), "run-2"; got != want {
		t.Fatalf("trackRunLineage() successor = %q, want %q", got, want)
	}
}

func TestRunContinuityStatusUsesLatestSucceededRunState(t *testing.T) {
	t.Parallel()

	if got, want := runContinuityStatus(
		"succeeded",
		enumtypes.MissionControlCoverageClassOpenPrimary,
		"codex-k8s/codex-k8s/pull/542",
		"",
	), enumtypes.MissionControlContinuityStatusMissingFollowUpIssue; got != want {
		t.Fatalf("runContinuityStatus() = %s, want %s", got, want)
	}

	if got, want := runContinuityStatus(
		"succeeded",
		enumtypes.MissionControlCoverageClassOpenPrimary,
		"",
		"",
	), enumtypes.MissionControlContinuityStatusMissingPullRequest; got != want {
		t.Fatalf("runContinuityStatus() missing PR = %s, want %s", got, want)
	}

	if got, want := runContinuityStatus(
		"succeeded",
		enumtypes.MissionControlCoverageClassOpenPrimary,
		"",
		"run-2",
	), enumtypes.MissionControlContinuityStatusComplete; got != want {
		t.Fatalf("runContinuityStatus() for superseded run = %s, want %s", got, want)
	}
}

func TestBuildWorkspaceWatermarksReflectsShadowCoverageAndOpenGaps(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC)
	staleAfter := observedAt.Add(2 * time.Hour)
	recentClosedAt := observedAt.Add(-3 * time.Hour)
	entitySeeds := map[string]projectionSeed{
		"issue-1": {
			entityKind:    enumtypes.MissionControlEntityKindWorkItem,
			providerKind:  enumtypes.MissionControlProviderKindGitHub,
			coverageClass: enumtypes.MissionControlCoverageClassOpenPrimary,
			projectedAt:   observedAt.Add(-time.Hour),
			staleAfter:    &staleAfter,
		},
		"run-1": {
			entityKind:    enumtypes.MissionControlEntityKindRun,
			providerKind:  enumtypes.MissionControlProviderKindPlatform,
			coverageClass: enumtypes.MissionControlCoverageClassOpenPrimary,
			projectedAt:   observedAt.Add(-time.Hour),
		},
		"pr-1": {
			entityKind:    enumtypes.MissionControlEntityKindPullRequest,
			providerKind:  enumtypes.MissionControlProviderKindGitHub,
			coverageClass: enumtypes.MissionControlCoverageClassRecentClosedContext,
			projectedAt:   recentClosedAt,
			staleAfter:    &staleAfter,
		},
	}
	gapSeeds := map[string]continuityGapSeed{
		"run-1::missing_pull_request": {
			subjectEntityKey: "run-1",
			gapKind:          enumtypes.MissionControlGapKindMissingPullRequest,
			severity:         enumtypes.MissionControlGapSeverityBlocking,
			detectedAt:       observedAt.Add(-30 * time.Minute),
		},
	}

	watermarks := buildWorkspaceWatermarks("proj-1", entitySeeds, gapSeeds, observedAt)
	if got, want := len(watermarks), 4; got != want {
		t.Fatalf("watermark len = %d, want %d", got, want)
	}

	if got, want := watermarks[0].status, enumtypes.MissionControlWorkspaceWatermarkStatusFresh; got != want {
		t.Fatalf("provider freshness status = %s, want %s", got, want)
	}
	if got, want := watermarks[1].status, enumtypes.MissionControlWorkspaceWatermarkStatusOutOfScope; got != want {
		t.Fatalf("provider coverage status = %s, want %s", got, want)
	}
	if watermarks[1].windowStartedAt == nil || watermarks[1].windowEndedAt == nil {
		t.Fatal("provider coverage watermark must expose bounded recent-closed window when shadow context exists")
	}
	if got, want := watermarks[2].status, enumtypes.MissionControlWorkspaceWatermarkStatusDegraded; got != want {
		t.Fatalf("graph projection status = %s, want %s", got, want)
	}
	if got, want := watermarks[3].status, enumtypes.MissionControlWorkspaceWatermarkStatusDegraded; got != want {
		t.Fatalf("launch policy status = %s, want %s", got, want)
	}
}
