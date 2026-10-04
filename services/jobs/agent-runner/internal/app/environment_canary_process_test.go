package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

func TestEnvironmentCanaryProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_ENVIRONMENT_CANARY_TEST")
	if mode == "" {
		return
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	policy.Volumes = []runtimecontract.RuntimeVolume{{Name: "scratch", Kind: runtimecontract.RuntimeVolumeEphemeralDisk, SizeMiB: 16}, {Name: "cache", Kind: runtimecontract.RuntimeVolumeEphemeralMemory, SizeMiB: 16}}
	if mode == "empty" {
		policy.Volumes = nil
	}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if mode == "cancelled" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	input := model.Input{WorkspaceRoot: "/workspace", WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(), EnvironmentPolicy: policy}
	err = runWorkspaceCanary(ctx, input)
	result := "OK"
	if err != nil {
		result = workspacepolicy.DenialReason(err)
	}
	_, _ = io.WriteString(os.Stdout, result)
	os.Exit(0)
}

func TestEnvironmentCanaryNativeMountProcess(t *testing.T) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("bwrap is required for environment volume canary tests")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"exact", "empty", "diskReadonly", "memoryOversize", "wrongMemoryType", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := workspaceProcessFixture(t)
			// /tmp на стенде может быть tmpfs. Disk fixture находится на FS
			// рабочего дерева, чтобы не выдавать memory bind за disk emptyDir.
			disk, err := os.MkdirTemp(".", ".kodex-volume-canary-")
			if err != nil {
				t.Fatal(err)
			}
			disk, err = filepath.Abs(disk)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(disk); err != nil {
					t.Error(err)
				}
			})
			if err := os.MkdirAll(filepath.Join(root, ".kodex/volumes/cache"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(disk, "preserve"), []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--unshare-user", "--unshare-net", "--uid", strconv.Itoa(os.Geteuid()), "--gid", strconv.Itoa(os.Getegid()), "--tmpfs", "/", "--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64", "--ro-bind", executable, "/agent-test", "--bind", root, "/workspace"}
			diskFlag := "--bind"
			if mode == "diskReadonly" {
				diskFlag = "--ro-bind"
			}
			args = append(args, diskFlag, disk, "/workspace/.kodex/volumes/scratch")
			if mode == "wrongMemoryType" {
				args = append(args, "--bind", disk, "/workspace/.kodex/volumes/cache")
			} else {
				size := "16777216"
				if mode == "memoryOversize" {
					size = "33554432"
				}
				args = append(args, "--size", size, "--tmpfs", "/workspace/.kodex/volumes/cache")
			}
			args = append(args, "--chdir", "/workspace", "--remount-ro", "/", "/agent-test", "-test.run=^TestEnvironmentCanaryProcessFixture$")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, bwrap, args...)
			command.Env = []string{"PATH=/usr/bin:/bin", "KODEX_ENVIRONMENT_CANARY_TEST=" + mode}
			var diagnostics bytes.Buffer
			command.Stderr = &diagnostics
			err = runCanaryCommand(ctx, command)
			if (mode == "exact" || mode == "empty") != (err == nil) {
				t.Fatalf("native configured volume outcome: %v diagnostics=%s", err, diagnostics.String())
			}
			if ctx.Err() != nil || command.ProcessState == nil {
				t.Fatal("native volume canary was not bounded and reaped")
			}
			if raw, err := os.ReadFile(filepath.Join(disk, "preserve")); err != nil || string(raw) != "synthetic" {
				t.Fatal("canary changed existing disk file")
			}
			entries, err := os.ReadDir(disk)
			if err != nil || len(entries) != 1 {
				t.Fatal("canary did not clean only its nonce files")
			}
		})
	}
}
