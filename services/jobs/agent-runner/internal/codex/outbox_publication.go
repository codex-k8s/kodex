package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

const maximumPublishedArtifactBytes = 1 << 20

var errProviderOutboxPublication = errors.New("provider outbox publication is invalid")

// Публикация выполняется самим writer UID после join provider-процессов.
// Runner не получает CAP_CHOWN или доступ к приватному provider home.
func publishProviderOutbox(ctx context.Context, input model.Input) error {
	if ctx.Err() != nil || os.Geteuid() != 10002 || input.WorkspacePolicy.Validate() != nil {
		return errProviderOutboxPublication
	}
	lock, err := workspacepolicy.Lock(ctx, input.WorkspaceRoot)
	if err != nil {
		return errProviderOutboxPublication
	}
	defer lock.Close()
	directory, err := workspacepolicy.OpenOutbox(input.WorkspaceRoot)
	if err != nil {
		return errProviderOutboxPublication
	}
	defer directory.Close()
	return publishOutboxDirectory(ctx, directory, input.WorkspacePolicy.MaximumFileCount, 10001, 10002, 29000)
}

// UID/GID здесь задаёт только закрытый production callsite, не runtime input.
// Отдельная fd-bound операция позволяет проверять отказ без root fixtures.
func publishOutboxDirectory(ctx context.Context, directory *os.File, maximumFiles int64, runnerUID, providerUID, group uint32) error {
	if ctx.Err() != nil || maximumFiles < 1 || maximumFiles > runtimecontract.RuntimeWorkspaceMaximumFiles {
		return errProviderOutboxPublication
	}
	var stat unix.Stat_t
	if unix.Fstat(int(directory.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Uid != runnerUID || stat.Gid != group || stat.Mode&0o7777 != 0o2770 {
		return errProviderOutboxPublication
	}
	entries, err := directory.ReadDir(int(maximumFiles) + 1)
	if err != nil && !errors.Is(err, io.EOF) || int64(len(entries)) > maximumFiles {
		return errProviderOutboxPublication
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return errProviderOutboxPublication
		}
		name := entry.Name()
		if entry.IsDir() || name == "result.md" {
			continue
		}
		if name == "workspace-write-result.json" || name == "." || name == ".." || name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
			return errProviderOutboxPublication
		}
		if publishOutboxFile(int(directory.Fd()), name, providerUID, group) != nil {
			return errProviderOutboxPublication
		}
	}
	return nil
}

func publishOutboxFile(directory int, name string, providerUID, group uint32) error {
	descriptor, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errProviderOutboxPublication
	}
	defer unix.Close(descriptor)
	var before unix.Stat_t
	if unix.Fstat(descriptor, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG ||
		before.Uid != providerUID || before.Nlink != 1 || before.Size < 1 || before.Size > maximumPublishedArtifactBytes {
		return errProviderOutboxPublication
	}
	// Только открытый provider-owned inode получает shared read. Путь никогда
	// не переоткрывается для chmod/chown, в том числе после atomic replace.
	if before.Gid != group && unix.Fchown(descriptor, -1, int(group)) != nil || unix.Fchmod(descriptor, 0o640) != nil {
		return errProviderOutboxPublication
	}
	var after, current unix.Stat_t
	if unix.Fstat(descriptor, &after) != nil || unix.Fstatat(directory, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		after.Dev != before.Dev || after.Ino != before.Ino || after.Uid != providerUID || after.Gid != group ||
		after.Nlink != 1 || after.Mode&unix.S_IFMT != unix.S_IFREG || after.Mode&0o7777 != 0o640 ||
		after.Size != before.Size || after.Mtim != before.Mtim || current.Dev != after.Dev || current.Ino != after.Ino ||
		current.Nlink != 1 || current.Mode != after.Mode || current.Uid != after.Uid || current.Gid != after.Gid ||
		current.Size != after.Size || current.Mtim != after.Mtim {
		return errProviderOutboxPublication
	}
	return nil
}
