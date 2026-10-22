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

// maximumPublishedArtifactBytes caps each nonempty published regular file at one MiB.
// maximumPublishedArtifactBytes ограничивает каждый непустой публикуемый regular file одним МиБ.
const maximumPublishedArtifactBytes = 1 << 20

// errProviderOutboxPublication is the fixed sentinel wrapped with a closed filesystem reason;
// it does not contain the artifact path or underlying operating-system diagnostic.
// errProviderOutboxPublication — фиксированный sentinel с закрытой filesystem reason;
// путь артефакта и исходная диагностика ОС в него не входят.
var errProviderOutboxPublication = errors.New("provider outbox publication is invalid")

// publishProviderOutbox requires writer UID 10002, validates policy and takes the workspace lock
// before granting shared read on eligible outbox files. The caller must already have joined provider
// processes; this function neither stops them nor uploads artifacts or grants access to private home.
// publishProviderOutbox требует writer UID 10002, проверяет policy и берёт workspace lock
// перед выдачей shared read допустимым файлам outbox. Caller обязан заранее выполнить join provider
// processes; функция их не останавливает, не загружает артефакты и не открывает приватный home.
func publishProviderOutbox(ctx context.Context, input model.Input) error {
	if ctx.Err() != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemContext)
	}
	if os.Geteuid() != 10002 {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemOwner)
	}
	if input.WorkspacePolicy.Validate() != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemPolicy)
	}
	lock, err := workspacepolicy.Lock(ctx, input.WorkspaceRoot)
	if err != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemLock)
	}
	defer lock.Close()
	directory, err := workspacepolicy.OpenOutbox(input.WorkspaceRoot)
	if err != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemDirectory)
	}
	defer directory.Close()
	return publishOutboxDirectory(ctx, directory, input.WorkspacePolicy.MaximumFileCount, 10001, 10002, 29000)
}

// publishOutboxDirectory consumes an open directory at its current read offset, checks its exact
// owner/group/setgid mode and entry budget, skips directories/result.md and publishes remaining files.
// Trusted production code supplies identities and a fresh descriptor. Failure can follow earlier
// chmod/chown effects: this flat permission publication is neither transactional nor an artifact upload.
// publishOutboxDirectory читает открытый каталог с текущего смещения, проверяет точные
// owner/group/setgid mode и бюджет записей, пропускает каталоги/result.md, публикуя остальные файлы.
// Identities и свежий descriptor задаёт доверенный production code. До ошибки уже могли пройти
// chmod/chown: эта плоская публикация прав не транзакционна и не является загрузкой артефакта.
func publishOutboxDirectory(ctx context.Context, directory *os.File, maximumFiles int64, runnerUID, providerUID, group uint32) error {
	if ctx.Err() != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemContext)
	}
	if maximumFiles < 1 || maximumFiles > runtimecontract.RuntimeWorkspaceMaximumFiles {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemPolicy)
	}
	var stat unix.Stat_t
	if unix.Fstat(int(directory.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Uid != runnerUID || stat.Gid != group || stat.Mode&0o7777 != 0o2770 {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemDirectoryIdentity)
	}
	entries, err := directory.ReadDir(int(maximumFiles) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemIO)
	}
	if int64(len(entries)) > maximumFiles {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemEntryCount)
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return providerFilesystemDenied(errProviderOutboxPublication, filesystemContext)
		}
		name := entry.Name()
		if entry.IsDir() || name == "result.md" {
			continue
		}
		if name == "workspace-write-result.json" || name == "." || name == ".." || name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
			return providerFilesystemDenied(errProviderOutboxPublication, filesystemEntryName)
		}
		if err := publishOutboxFile(int(directory.Fd()), name, providerUID, group); err != nil {
			return err
		}
	}
	return nil
}

// publishOutboxFile expects a validated single-component name beneath the supplied directory fd.
// It opens without following a leaf symlink, requires a provider-owned single-link bounded regular
// file, adjusts that inode's group/mode, then compares fd/path metadata. It does not parse contents,
// freeze later writes or roll back permission changes if a subsequent check fails.
// publishOutboxFile требует уже проверенное однокомпонентное имя под переданным directory fd.
// Она открывает leaf без следования symlink, требует provider-owned single-link bounded regular
// file, меняет group/mode этого inode и сравнивает fd/path metadata. Содержимое не разбирается,
// последующие записи не замораживаются, изменения прав при поздней ошибке не откатываются.
func publishOutboxFile(directory int, name string, providerUID, group uint32) error {
	descriptor, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemEntryOpen)
	}
	defer unix.Close(descriptor)
	var before unix.Stat_t
	if unix.Fstat(descriptor, &before) != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemIO)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemFileType)
	}
	if before.Uid != providerUID {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemOwner)
	}
	if before.Nlink != 1 {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemLinkCount)
	}
	if before.Size < 1 || before.Size > maximumPublishedArtifactBytes {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemFileSize)
	}
	// Только открытый provider-owned inode получает shared read. Путь никогда
	// не переоткрывается для chmod/chown, в том числе после atomic replace.
	if before.Gid != group && unix.Fchown(descriptor, -1, int(group)) != nil || unix.Fchmod(descriptor, 0o640) != nil {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemPermissions)
	}
	var after, current unix.Stat_t
	if unix.Fstat(descriptor, &after) != nil || unix.Fstatat(directory, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		after.Dev != before.Dev || after.Ino != before.Ino || after.Uid != providerUID || after.Gid != group ||
		after.Nlink != 1 || after.Mode&unix.S_IFMT != unix.S_IFREG || after.Mode&0o7777 != 0o640 ||
		after.Size != before.Size || after.Mtim != before.Mtim || current.Dev != after.Dev || current.Ino != after.Ino ||
		current.Nlink != 1 || current.Mode != after.Mode || current.Uid != after.Uid || current.Gid != after.Gid ||
		current.Size != after.Size || current.Mtim != after.Mtim {
		return providerFilesystemDenied(errProviderOutboxPublication, filesystemMetadataChanged)
	}
	return nil
}
