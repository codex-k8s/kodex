package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// nativeUIDFixture creates only inert bindings; it is not a runtime admission or an engine transcript test.
// nativeUIDFixture создаёт лишь inert bindings; это не runtime admission или engine transcript test.
func nativeUIDFixture(root string) (model.Input, Result, *nativeCaptureProof) {
	input := model.Input{Provider: "opencode", Mode: runtimecontract.RunnerModeTurn, WorkspaceRoot: root,
		OrganizationRef: "org_synthetic", ProjectRef: "prj_synthetic", AgentRef: "agt_synthetic", SessionRef: "ses_synthetic", TurnRef: "trn_synthetic", LeaseRef: "lea_synthetic", LeaseGeneration: 1, Attempt: 1,
		ExecutionBindingDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64)}
	path, _ := runtimecontract.NativeSessionExportPath("ses_native", 1)
	digest := sha256.Sum256([]byte("synthetic opaque checkpoint"))
	checkpoint := runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: path, SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len("synthetic opaque checkpoint"))}
	proof := &nativeCaptureProof{Schema: nativeCaptureSchema, ExecutionBindingDigest: input.ExecutionBindingDigest, RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef, SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, Checkpoint: checkpoint}
	return input, Result{NativeSession: &checkpoint}, proof
}

// TestNativeCaptureFixedUID qualifies real filesystem access for provider, runner and role identities.
// It runs only in an explicitly selected disposable root test process and never touches a cluster.
// TestNativeCaptureFixedUID проверяет actual filesystem access provider, runner и role identities.
// Запускается лишь в явно выбранном disposable root test process без обращения к кластеру.
func TestNativeCaptureFixedUID(t *testing.T) {
	mode := os.Getenv("KODEX_TEST_NATIVE_UID_MODE")
	if mode == "" {
		t.Skip("requires explicit disposable fixed-UID qualification")
	}
	if mode == "coordinator" {
		if os.Geteuid() != 0 {
			t.Fatal("fixed-UID coordinator requires isolated root execution")
		}
		root, err := os.MkdirTemp("/tmp", "kodex-native-uid-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(root)
		if os.Chmod(root, 0o755) != nil {
			t.Fatal("prepare disposable root")
		}
		for _, relative := range []string{".kodex", ".kodex/state", ".kodex/state/opencode"} {
			path := filepath.Join(root, relative)
			if os.Mkdir(path, 0o750) != nil || os.Chown(path, 10001, 29000) != nil {
				t.Fatal("prepare trusted state root")
			}
		}
		if os.Chmod(filepath.Join(root, ".kodex/state/opencode"), os.ModeSetgid|0o770) != nil {
			t.Fatal("prepare shared native root")
		}
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		run := func(mode string, uid, gid uint32) {
			t.Helper()
			command := exec.Command(binary, "-test.run=^TestNativeCaptureFixedUID$", "-test.timeout=15s")
			command.Env = []string{"KODEX_TEST_NATIVE_UID_MODE=" + mode, "KODEX_TEST_NATIVE_UID_ROOT=" + root}
			command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}}}
			if raw, err := command.CombinedOutput(); err != nil {
				t.Fatalf("fixed-UID child %s failed: %v\n%s", mode, err, raw)
			}
		}
		run("writer", 10002, 29000)
		run("retry", 10002, 29000)
		run("witness", 10001, 29000)
		run("role", 10003, 10003)
		input, result, _ := nativeUIDFixture(root)
		path := filepath.Join(input.WorkspaceRoot, result.NativeSession.RelativePath)
		if os.Chmod(path, 0o644) != nil {
			t.Fatal("prepare unsafe publication mode")
		}
		run("reject", 10001, 29000)
		if os.Chmod(path, 0o640) != nil || os.Link(path, path+".link") != nil {
			t.Fatal("prepare unsafe linked publication")
		}
		run("reject", 10001, 29000)
		return
	}
	root := os.Getenv("KODEX_TEST_NATIVE_UID_ROOT")
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Base(root), "kodex-native-uid-") {
		t.Fatal("invalid disposable fixture root")
	}
	input, result, proof := nativeUIDFixture(root)
	path := filepath.Join(root, result.NativeSession.RelativePath)
	switch mode {
	case "writer":
		syscall.Umask(0o077)
		if os.Geteuid() != 10002 {
			t.Fatal("wrong writer identity")
		}
		if generation, err := prepareNativeGeneration(input); err != nil || generation != 1 {
			for _, relative := range []string{".kodex/state/opencode", ".kodex/state/opencode/checkpoints", ".kodex/state/opencode/checkpoints/g1", ".kodex/state/opencode/checkpoints/g1/intent.json"} {
				info, statErr := os.Lstat(filepath.Join(root, relative))
				if statErr != nil {
					t.Log("fixture stage absent", relative)
					continue
				}
				stat, _ := info.Sys().(*syscall.Stat_t)
				t.Logf("fixture stage %s mode=%v uid=%d gid=%d", relative, info.Mode(), stat.Uid, stat.Gid)
			}
			t.Fatal("prepare native writer intent", err)
		}
		if os.WriteFile(path, []byte("synthetic opaque checkpoint"), 0o640) != nil || os.Chmod(path, 0o640) != nil {
			t.Fatal("publish private fixture export")
		}
	case "retry":
		if _, err := prepareNativeGeneration(input); err == nil {
			t.Fatal("uncertain generation was overwritten")
		}
	case "witness":
		if os.Geteuid() != 10001 || verifyNativeCapture(input, result, proof, 10002) != nil {
			t.Fatal("runner could not verify actual provider-owned bytes")
		}
		foreign := input
		foreign.LeaseGeneration++
		if verifyNativeCapture(foreign, result, proof, 10002) == nil {
			t.Fatal("old export acquired newer writer authority")
		}
	case "role":
		if _, err := os.ReadFile(path); !os.IsPermission(err) {
			t.Fatal("role identity could read private native history")
		}
	case "reject":
		if verifyNativeCapture(input, result, proof, 10002) == nil {
			t.Fatal("unsafe export publication accepted")
		}
	default:
		t.Fatal("unknown native fixture mode")
	}
}
