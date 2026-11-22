package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// TestMain exposes the real descriptor/receipt runtime only to an explicit private native fixture; ordinary helper tests keep their existing entrypoint and installed-profile checks are not bypassed in production.
// TestMain предоставляет real descriptor/receipt runtime лишь explicit private native fixture; обычные helper tests сохраняют прежний entrypoint, production installed-profile checks не обходятся.
func TestMain(m *testing.M) {
	base := os.Getenv("KODEX_MANAGED_SSH_TEST_ROOT")
	if base == "" {
		os.Exit(m.Run())
	}
	if runSSHTransportFixture(base) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// runSSHTransportFixture confines one real helper request to exact private test roots owned by the current non-root UID; it never accepts an installed path, arbitrary profile selector or persistent host namespace.
// runSSHTransportFixture ограничивает один real helper request exact private test roots текущего non-root UID; installed path, произвольный profile selector либо persistent host namespace не принимаются.
func runSSHTransportFixture(base string) error {
	if os.Getuid() == 0 || os.Getgid() == 0 || !filepath.IsAbs(base) || filepath.Clean(base) != base || !strings.HasPrefix(filepath.Base(base), "kodex-ssh-transport-") {
		return ErrRejected
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrRejected
	}
	var root unix.Stat_t
	if unix.Lstat(base, &root) != nil || root.Uid != uint32(os.Getuid()) || root.Gid != uint32(os.Getgid()) {
		return ErrRejected
	}
	file, err := os.OpenFile(filepath.Join(base, "profile.json"), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return ErrRejected
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0400 || stat.Uid != root.Uid || stat.Gid != root.Gid || stat.Nlink != 1 || stat.Size < 1 || stat.Size > managedssh.MaximumFrame {
		return ErrRejected
	}
	raw := make([]byte, stat.Size)
	defer clear(raw)
	if _, err := file.ReadAt(raw, 0); err != nil {
		return ErrRejected
	}
	profile, err := managedssh.DecodeProfile(raw)
	if err != nil || profile.UID != root.Uid || profile.GID != root.Gid || profile.StateDirectory != filepath.Join(base, "receipts") || len(profile.Roots) != 1 || profile.Roots[0].Alias != "documents" || profile.Roots[0].Path != filepath.Join(base, "documents") || len(profile.Services) != 0 {
		return ErrRejected
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runtime, err := openRuntime(ctx, profile)
	if err != nil {
		return err
	}
	defer runtime.Close()
	return runtime.Serve(ctx, os.Stdin, os.Stdout)
}
