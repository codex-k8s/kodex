package model

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestFromProtoAcceptsCanonicalNativeArchive accepts the native export path for its own session.
// RU: TestFromProtoAcceptsCanonicalNativeArchive принимает путь native export для его собственной сессии.
func TestFromProtoAcceptsCanonicalNativeArchive(t *testing.T) {
	t.Parallel()

	input := validSnapshotProto()
	if _, err := FromProto(input); err != nil {
		t.Fatalf("decode canonical timestamped archive task: %v", err)
	}
}

// TestFromProtoRejectsArchiveFromAnotherNativeSession rejects a path whose embedded session differs from the task.
// RU: TestFromProtoRejectsArchiveFromAnotherNativeSession отклоняет путь с идентичностью сессии, отличной от задания.
func TestFromProtoRejectsArchiveFromAnotherNativeSession(t *testing.T) {
	t.Parallel()

	input := validSnapshotProto()
	input.NativeSessionId = "00000000-0000-4000-8000-000000000004"
	if _, err := FromProto(input); err == nil {
		t.Fatal("task accepted an archive path from another native session")
	}
}

// TestFromProtoRejectsPVCFromAnotherSession requires the server-derived volume name for the exact session.
// RU: TestFromProtoRejectsPVCFromAnotherSession требует производное от сервера имя тома точной сессии.
func TestFromProtoRejectsPVCFromAnotherSession(t *testing.T) {
	t.Parallel()

	input := validSnapshotProto()
	input.PvcName = "runtime-session-0123456789abcdef"
	if _, err := FromProto(input); err == nil {
		t.Fatal("task accepted a PVC from another Session")
	}
}

// validSnapshotProto constructs a synthetic snapshot with consistent session path and derived PVC identity.
// RU: validSnapshotProto создаёт синтетический снимок с согласованным путём сессии и производной идентичностью PVC.
func validSnapshotProto() *controlplanev1.SessionArchiveTask {
	const nativeSessionID = "ses_NativeFixture3"
	input := &controlplanev1.SessionArchiveTask{
		TaskRef:                "sat_00000000-0000-4000-8000-000000000001",
		Kind:                   controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT,
		OrganizationRef:        "org_00000000-0000-4000-8000-000000000001",
		ProjectRef:             "prj_00000000-0000-4000-8000-000000000001",
		SessionRef:             "ses_00000000-0000-4000-8000-000000000001",
		ProviderAccountRef:     "pva_00000000-0000-4000-8000-000000000001",
		RuntimeRevisionRef:     "rrv_00000000-0000-4000-8000-000000000001",
		RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest:  strings.Repeat("a", 64),
		NativeSessionId:        nativeSessionID,
		ContentGeneration:      1,
		InputDigest:            strings.Repeat("b", 64),
		SourceRelativePath:     ".kodex/state/opencode/checkpoints/g1/" + nativeSessionID + ".json",
		SourceSha256:           strings.Repeat("c", 64),
		SourceSizeBytes:        1024,
		TargetObjectKey:        "session-archive/v3/org/prj/session/g1/task-a1.tar",
		Attempt:                1,
	}
	input.PvcName, _ = runtimecontract.SessionPVCName(input.SessionRef)
	return input
}
