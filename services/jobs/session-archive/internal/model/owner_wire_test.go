package model

import (
	"encoding/json"
	"os"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestWorkspaceArchiveOwnerWireConsumer consumes exact bytes exported by the real owner caster in a separate module.
// It proves typed wire delivery and fail-closed worker decoding, not stored authority or filesystem restoration.
// TestWorkspaceArchiveOwnerWireConsumer читает exact bytes настоящего owner caster из отдельного модуля.
// Проверяется typed wire delivery и закрытый worker decode, не stored authority либо filesystem restore.
func TestWorkspaceArchiveOwnerWireConsumer(t *testing.T) {
	path := os.Getenv("KODEX_WORKSPACE_OWNER_PROTO_FIXTURE")
	if path == "" {
		t.Skip("explicit owner-to-worker wire harness is required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > MaximumTaskBytes {
		t.Fatal("owner wire fixture is not a bounded private file")
	}
	raw, err := os.ReadFile(path)
	var fixtures map[string][]byte
	if err != nil || json.Unmarshal(raw, &fixtures) != nil || len(fixtures) != 6 {
		t.Fatal("read owner wire fixture")
	}
	for _, key := range []string{"SNAPSHOT", "RESTORE", "DELETE_PVC", "DELETE_OBJECT", "SNAPSHOT_RECONCILE", "RESTORE_RECONCILE"} {
		var input cp.SessionArchiveTask
		if len(fixtures[key]) == 0 || proto.Unmarshal(fixtures[key], &input) != nil {
			t.Fatal("decode actual owner protobuf", key)
		}
		task, err := FromProto(&input)
		if err != nil || task.Workspace == nil || task.Validate() != nil || task.CodexSessionID != "" || task.Archive != nil {
			t.Fatal("actual owner output rejected or downgraded", key, err)
		}
		raw, err := json.Marshal(task)
		if err != nil {
			t.Fatal("encode actual worker task", key, err)
		}
		decoded, err := DecodeTask(raw)
		if err != nil || decoded.Workspace == nil || decoded.Workspace.Target.Scope != task.Workspace.Target.Scope ||
			decoded.Workspace.Policy.Digest != task.Workspace.Policy.Digest || decoded.ContentGeneration != task.ContentGeneration {
			t.Fatal("worker task codec lost owner commitments", key, err)
		}
		changed := proto.Clone(&input).(*cp.SessionArchiveTask)
		changed.SessionRef = "ses_foreign000"
		if _, err := FromProto(changed); err == nil {
			t.Fatal("worker accepted foreign owner scope", key)
		}
		changed = proto.Clone(&input).(*cp.SessionArchiveTask)
		changed.Workspace.Policy.Rules[0].Access = cp.RuntimeWorkspaceAccess(999)
		if _, err := FromProto(changed); err == nil {
			t.Fatal("worker accepted unknown policy enum", key)
		}
		changed = proto.Clone(&input).(*cp.SessionArchiveTask)
		changed.Workspace = nil
		if _, err := FromProto(changed); err == nil {
			t.Fatal("work-only task silently downgraded to legacy", key)
		}
	}
}
