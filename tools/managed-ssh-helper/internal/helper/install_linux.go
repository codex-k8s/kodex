package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// ErrRejected is the only public helper setup diagnostic; it never contains a local path, file content or syscall detail.
// ErrRejected является единственной публичной setup diagnostic helper; она не содержит local path, file content либо syscall detail.
var ErrRejected = errors.New("managed SSH helper rejected")

// configurationPath is selected by the operator installation, never SSH_ORIGINAL_COMMAND, environment or a request field.
// configurationPath выбирается operator installation, а не SSH_ORIGINAL_COMMAND, environment либо request field.
const configurationPath = "/etc/kodex/ssh-target.json"

// OpenInstalled validates the root-owned installed configuration and actual unprivileged executable before opening selected managed namespaces.
// OpenInstalled до открытия выбранных managed namespaces проверяет root-owned installed configuration и фактический unprivileged executable.
func OpenInstalled(ctx context.Context) (*Runtime, error) {
	profile, err := installedProfile(ctx, false)
	if err != nil {
		return nil, err
	}
	return openRuntime(ctx, profile)
}

// installedProfile validates the shared root-owned deployment and actual identity for either executable; switching the executable never skips the UID, capability, expiry or namespace checks.
// installedProfile проверяет общий root-owned deployment и actual identity обоих executable; смена executable не пропускает UID, capability, expiry либо namespace checks.
func installedProfile(ctx context.Context, accountShell bool) (managedssh.Profile, error) {
	if ctx == nil || ctx.Err() != nil || os.Getuid() == 0 || os.Getuid() != os.Geteuid() || os.Getgid() == 0 || os.Getgid() != os.Getegid() {
		return managedssh.Profile{}, ErrRejected
	}
	parent, err := operatorDirectory(path.Dir(configurationPath))
	if err != nil {
		return managedssh.Profile{}, ErrRejected
	}
	defer parent.Close()
	configuration, err := unix.Openat(int(parent.Fd()), path.Base(configurationPath), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return managedssh.Profile{}, ErrRejected
	}
	file := os.NewFile(uintptr(configuration), "configuration")
	defer file.Close()
	if !operatorFile(file, false) {
		return managedssh.Profile{}, ErrRejected
	}
	raw, err := io.ReadAll(io.LimitReader(file, managedssh.MaximumFrame+1))
	if err != nil {
		return managedssh.Profile{}, ErrRejected
	}
	profile, err := managedssh.DecodeProfile(raw)
	if err != nil || profile.ValidUntil <= time.Now().Unix() || int(profile.UID) != os.Getuid() || int(profile.GID) != os.Getgid() {
		return managedssh.Profile{}, ErrRejected
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) > 32 {
		return managedssh.Profile{}, ErrRejected
	}
	observed := make([]uint32, len(groups))
	for index, group := range groups {
		if group <= 0 {
			return managedssh.Profile{}, ErrRejected
		}
		observed[index] = uint32(group)
	}
	slices.Sort(observed)
	observed = slices.Compact(observed)
	if !slices.Equal(observed, profile.Groups) {
		return managedssh.Profile{}, ErrRejected
	}
	executable, err := os.Open("/proc/self/exe")
	if err != nil {
		return managedssh.Profile{}, ErrRejected
	}
	defer executable.Close()
	expectedExecutable := profile.ExecutableDigest
	if accountShell {
		expectedExecutable = profile.ShellDigest
	}
	if !operatorFile(executable, true) || fileDigest(executable, 64<<20) != expectedExecutable {
		return managedssh.Profile{}, ErrRejected
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil || len(status) > 32768 {
		return managedssh.Profile{}, ErrRejected
	}
	for _, name := range []string{"CapInh:", "CapPrm:", "CapEff:", "CapAmb:"} {
		matched := false
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, name) {
				matched = len(strings.Fields(line)) == 2 && strings.TrimSpace(strings.TrimPrefix(line, name)) == "0000000000000000"
			}
		}
		if !matched {
			return managedssh.Profile{}, ErrRejected
		}
	}
	if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return managedssh.Profile{}, ErrRejected
	}
	for _, directory := range append([]managedssh.RootScope{{Path: profile.StateDirectory}}, profile.Roots...) {
		index := strings.LastIndexByte(directory.Path, '/')
		parentName := directory.Path[:index]
		if parentName == "" {
			parentName = "/"
		}
		parent, err := operatorDirectory(parentName)
		if err != nil {
			return managedssh.Profile{}, ErrRejected
		}
		parent.Close()
	}
	return profile, nil
}

// operatorDirectory walks each absolute component without symlinks and requires root-owned non-writable parents; the selected namespace itself is checked separately.
// operatorDirectory обходит каждый absolute component без symlinks и требует root-owned non-writable parents; выбранный namespace проверяется отдельно.
func operatorDirectory(name string) (*os.File, error) {
	if !strings.HasPrefix(name, "/") || name != "/" && !managedssh.RelativePath(strings.TrimPrefix(name, "/")) {
		return nil, ErrRejected
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRejected
	}
	file := os.NewFile(uintptr(fd), "operator-directory")
	for _, part := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(int(file.Fd()), part, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		file.Close()
		if err != nil {
			return nil, ErrRejected
		}
		file = os.NewFile(uintptr(next), "operator-directory")
		var stat unix.Stat_t
		if unix.Fstat(next, &stat) != nil || stat.Uid != 0 || stat.Mode&0022 != 0 || stat.Mode&07000 != 0 {
			file.Close()
			return nil, ErrRejected
		}
	}
	return file, nil
}

// operatorFile admits one immutable root-owned regular inode without hard links or privilege bits; files used for execution must be executable and carry no Linux file capabilities.
// operatorFile допускает один immutable root-owned regular inode без hard links либо privilege bits; executable files обязаны быть исполняемыми и не иметь Linux file capabilities.
func operatorFile(file *os.File, executable bool) bool {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Nlink != 1 || stat.Mode&0022 != 0 || stat.Mode&07000 != 0 || executable && stat.Mode&0111 == 0 {
		return false
	}
	if executable {
		if _, err := unix.Fgetxattr(int(file.Fd()), "security.capability", nil); err != unix.ENODATA && err != unix.ENOTSUP {
			return false
		}
	}
	return true
}

// fileDigest hashes a bounded opened inode and returns no digest after overflow, read failure or concurrent metadata change.
// fileDigest хеширует bounded opened inode и не возвращает digest после overflow, read failure либо concurrent metadata change.
func fileDigest(file *os.File, maximum int64) string {
	before, err := file.Stat()
	if err != nil || before.Size() < 0 || before.Size() > maximum {
		return ""
	}
	hash := sha256.New()
	length, err := io.Copy(hash, io.LimitReader(file, maximum+1))
	after, final := file.Stat()
	if err != nil || final != nil || length != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
