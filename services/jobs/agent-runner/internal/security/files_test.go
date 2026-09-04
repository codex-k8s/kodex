package security

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkspaceVolumeRootsAcceptKubeletPermissionsOnlyAtExactMounts(t *testing.T) {
	for _, mode := range []uint32{0o2770, 0o2775, 0o2777} {
		stat := unix.Stat_t{Uid: 0, Gid: 29000, Mode: unix.S_IFDIR | mode}
		for _, path := range []string{"input", "knowledge", ".kodex/state"} {
			if !isWorkspaceVolumeRoot(path, stat) {
				t.Fatalf("kubelet mount %s mode %o rejected", path, mode)
			}
		}
		for _, path := range []string{".kodex", ".kodex/outbox", "input/foreign", "../input"} {
			if isWorkspaceVolumeRoot(path, stat) {
				t.Fatalf("non-mount %s accepted", path)
			}
		}
	}
	for _, stat := range []unix.Stat_t{
		{Uid: 1, Gid: 29000, Mode: unix.S_IFDIR | 0o2777},
		{Uid: 0, Gid: 1, Mode: unix.S_IFDIR | 0o2777},
		{Uid: 0, Gid: 29000, Mode: unix.S_IFDIR | 0o2750},
		{Uid: 0, Gid: 29000, Mode: unix.S_IFLNK | 0o2777},
	} {
		if isWorkspaceVolumeRoot("input", stat) {
			t.Fatal("invalid mount ownership or type accepted")
		}
	}
}
