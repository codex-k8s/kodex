package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

func environmentCanaryPolicy(t *testing.T, volumes []runtimecontract.RuntimeVolume) runtimecontract.RuntimeEnvironmentPolicy {
	t.Helper()
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	policy.Volumes = volumes
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestEnvironmentCanaryRejectsMissingMountsAndSymlinks(t *testing.T) {
	for _, mode := range []string{"absent", "directory", "symlink", "digest", "path", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root, foreign := t.TempDir(), t.TempDir()
			parent := filepath.Join(root, ".kodex/volumes")
			if err := os.MkdirAll(parent, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(foreign, "preserve"), []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			policy := environmentCanaryPolicy(t, []runtimecontract.RuntimeVolume{{Name: "scratch", Kind: runtimecontract.RuntimeVolumeEphemeralDisk, SizeMiB: 16}})
			ctx := t.Context()
			switch mode {
			case "directory":
				if err := os.Mkdir(filepath.Join(parent, "scratch"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(foreign, filepath.Join(parent, "scratch")); err != nil {
					t.Fatal(err)
				}
			case "digest":
				policy.VolumesDigest = "foreign"
			case "path":
				policy.Volumes[0].MountPath = "/tmp/foreign"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := RunEnvironmentCanary(ctx, root, policy); err == nil {
				t.Fatal("invalid volume boundary accepted")
			}
			if raw, err := os.ReadFile(filepath.Join(foreign, "preserve")); err != nil || string(raw) != "synthetic" {
				t.Fatal("volume canary touched a foreign file")
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "scratch" {
					t.Fatal("failed canary created unexpected files")
				}
			}
		})
	}
	if err := RunEnvironmentCanary(t.Context(), t.TempDir(), environmentCanaryPolicy(t, nil)); err != nil {
		t.Fatal("absence of configured volumes was rejected")
	}
}

func TestEnvironmentFilesystemChecksTypeReadOnlyAndMemoryLimit(t *testing.T) {
	volume := runtimecontract.RuntimeVolume{Kind: runtimecontract.RuntimeVolumeEphemeralMemory, SizeMiB: 16}
	stat := unix.Statfs_t{Type: unix.TMPFS_MAGIC, Bsize: 4096, Blocks: 4096}
	if !validEnvironmentFilesystem(stat, volume) {
		t.Fatal("exact bounded tmpfs rejected")
	}
	stat.Blocks = 2048
	if !validEnvironmentFilesystem(stat, volume) {
		t.Fatal("smaller node-bounded tmpfs rejected")
	}
	for _, mode := range []string{"oversize", "readonly", "wrongtype", "zero", "overflow"} {
		candidate := stat
		switch mode {
		case "oversize":
			candidate.Blocks = 4097
		case "readonly":
			candidate.Flags = unix.ST_RDONLY
		case "wrongtype":
			candidate.Type = unix.EXT4_SUPER_MAGIC
		case "zero":
			candidate.Bsize = 0
		case "overflow":
			candidate.Blocks = ^uint64(0)
		}
		if validEnvironmentFilesystem(candidate, volume) {
			t.Fatalf("invalid filesystem accepted: %s", mode)
		}
	}
	volume.Kind = runtimecontract.RuntimeVolumeEphemeralDisk
	if validEnvironmentFilesystem(stat, volume) {
		t.Fatal("tmpfs accepted as disk")
	}
	stat.Type = unix.EXT4_SUPER_MAGIC
	if !validEnvironmentFilesystem(stat, volume) {
		t.Fatal("disk filesystem rejected")
	}
}
