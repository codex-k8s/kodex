package model

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestFromProtoAcceptsCanonicalTimestampedCodexArchive(t *testing.T) {
	t.Parallel()

	input := validSnapshotProto()
	if _, err := FromProto(input); err != nil {
		t.Fatalf("decode canonical timestamped archive task: %v", err)
	}
}

func TestFromProtoRejectsArchiveFromAnotherCodexSession(t *testing.T) {
	t.Parallel()

	input := validSnapshotProto()
	input.CodexSessionId = "00000000-0000-4000-8000-000000000004"
	if _, err := FromProto(input); err == nil {
		t.Fatal("task accepted an archive path from another Codex session")
	}
}

func validSnapshotProto() *controlplanev1.SessionArchiveTask {
	const codexSessionID = "00000000-0000-4000-8000-000000000003"
	return &controlplanev1.SessionArchiveTask{
		TaskRef:                "sat_00000000-0000-4000-8000-000000000001",
		Kind:                   controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT,
		OrganizationRef:        "org_00000000-0000-4000-8000-000000000001",
		ProjectRef:             "prj_00000000-0000-4000-8000-000000000001",
		SessionRef:             "ses_00000000-0000-4000-8000-000000000001",
		ProviderAccountRef:     "pva_00000000-0000-4000-8000-000000000001",
		RuntimeRevisionRef:     "rrv_00000000-0000-4000-8000-000000000001",
		RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest:  strings.Repeat("a", 64),
		CodexSessionId:         codexSessionID,
		ContentGeneration:      1,
		PvcName:                "runtime-session-0123456789abcdef",
		InputDigest:            strings.Repeat("b", 64),
		SourceRelativePath:     ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-" + codexSessionID + ".jsonl",
		SourceSha256:           strings.Repeat("c", 64),
		SourceSizeBytes:        1024,
		TargetObjectKey:        "session-archive/v1/org/prj/session/g1/task-a1.tar",
		Attempt:                1,
	}
}
