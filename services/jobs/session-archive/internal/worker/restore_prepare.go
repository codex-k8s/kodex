package worker

import (
	"context"
	"errors"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// restoreTaskFile is the fixed controller-mounted immutable task used by the non-root restore preparer.
// RU: restoreTaskFile задаёт фиксированное неизменяемое задание от контроллера для non-root подготовки восстановления.
const restoreTaskFile = "/var/run/config/kodex/session-archive/task.json"

// errRestorePreparation hides which identity, path or directory-ownership invariant rejected preparation.
// RU: errRestorePreparation скрывает конкретную причину отказа идентичности, пути либо владельца каталога при подготовке.
var errRestorePreparation = errors.New("session restore directory preparation is invalid")

// PrepareRestore uses fixed paths and the trusted init UID to create and verify only the shared Codex home directory.
// RU: PrepareRestore использует фиксированные пути и доверенный UID init для создания и проверки лишь общего каталога Codex home.
func PrepareRestore(ctx context.Context) error {
	if os.Geteuid() != 10001 || ctx.Err() != nil {
		return errRestorePreparation
	}
	task, err := model.DecodeFile(restoreTaskFile)
	if err != nil || validateRestorePreparationTask(task) != nil {
		return errRestorePreparation
	}
	root, err := unix.Open("/workspace", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errRestorePreparation
	}
	defer func() { _ = unix.Close(root) }()
	for _, component := range []string{".kodex", "state"} {
		next, openErr := unix.Openat(root, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return errRestorePreparation
		}
		unix.Close(root)
		root = next
	}
	var stat unix.Stat_t
	if unix.Fstat(root, &stat) != nil || stat.Uid != 0 || stat.Gid != 29000 ||
		stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o070 != 0o070 || ctx.Err() != nil {
		return errRestorePreparation
	}
	if err := unix.Mkdirat(root, "codex-home", 0o2770); err != nil && !errors.Is(err, unix.EEXIST) {
		return errRestorePreparation
	}
	home, err := unix.Openat(root, "codex-home", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errRestorePreparation
	}
	defer unix.Close(home)
	if unix.Fstat(home, &stat) != nil || stat.Uid != 10001 || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errRestorePreparation
	}
	if unix.Fchown(home, -1, 29000) != nil || unix.Fchmod(home, 0o2770) != nil || unix.Fsync(home) != nil ||
		unix.Fsync(root) != nil || unix.Fstat(home, &stat) != nil || stat.Uid != 10001 ||
		stat.Gid != 29000 || stat.Mode&0o7777 != 0o2770 || ctx.Err() != nil {
		return errRestorePreparation
	}
	return nil
}

// validateRestorePreparationTask accepts only a valid RESTORE task with identical task and archive source metadata.
// RU: validateRestorePreparationTask принимает лишь корректное RESTORE-задание с одинаковыми метаданными источника в задании и архиве.
func validateRestorePreparationTask(task model.Task) error {
	if task.Validate() != nil || task.Kind != "RESTORE" || task.Archive == nil ||
		task.Archive.SourceRelativePath != task.SourceRelativePath ||
		task.Archive.SourceSHA256 != task.SourceSHA256 || task.Archive.SourceSizeBytes != task.SourceSizeBytes {
		return errRestorePreparation
	}
	return nil
}
