package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	// socketRoot pins the directory prepared for the local authority sockets and readiness executable; callers cannot choose another root through arguments or environment.
	// socketRoot закрепляет каталог подготовки локальных сокетов authority и бинаря готовности; другой корень нельзя выбрать аргументами или окружением.
	socketRoot = "/run/kodex/internal-rpc-authority"
	// socketUID is the exact required real process and prepared-directory owner UID for socket initialization; it is not the issuer or verifier server UID.
	// socketUID — точный требуемый реальный UID процесса и владельца подготовленного каталога при инициализации сокетов; это не UID сервера issuer или verifier.
	socketUID = 29000
	// socketGID is the exact required real process and prepared-directory group for the shared local socket root.
	// socketGID — точная требуемая реальная группа процесса и подготовленного общего каталога локальных сокетов.
	socketGID = 29000
	// readinessSource pins the installed readiness binary copied into the socket directory; installation validates its initial regular-file type and size, not a signed digest.
	// readinessSource закрепляет установленный бинарь готовности для копирования в каталог сокетов; при установке проверяются исходные тип обычного файла и размер, а не подписанный дайджест.
	readinessSource = "/usr/local/bin/internal-rpc-authority-local-readiness"
	// readinessTarget pins the published local readiness executable path under socketRoot, with a separate exclusive .starting file used while copying.
	// readinessTarget закрепляет путь опубликованного локального бинаря готовности внутри socketRoot; при копировании используется отдельный эксклюзивный файл .starting.
	readinessTarget = "/run/kodex/internal-rpc-authority/local-readiness"
)

// main performs the socket-root preparation once, prints an English failure diagnostic and exits nonzero on error. It does not start issuer/verifier servers or repeatedly remove their sockets.
// main однократно выполняет подготовку каталога сокетов, выводит английскую диагностику и завершает процесс ненулевым кодом при ошибке. Серверы issuer и verifier не запускаются, их сокеты циклически не удаляются.
func main() {
	if err := prepareSocketRoot(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "internal-rpc-authority socket init failed: %v\n", err)
		os.Exit(1)
	}
}

// prepareSocketRoot requires the fixed real UID/GID, rejects a symlink or nondirectory parent/root, creates or chmods the root to sticky 0770 and validates ownership. It then removes the two existing socket paths and installs the readiness executable; it does not prove an existing socket is inactive or undo earlier filesystem changes after a later failure.
// prepareSocketRoot требует фиксированные реальные UID и GID, отклоняет ссылку или не-каталог для родителя и корня, создаёт либо chmod-ит корень до sticky 0770 и проверяет владельца. Затем удаляются два существующих пути сокетов и устанавливается бинарь готовности; не доказывается неактивность существующего сокета, ранние файловые изменения не откатываются при поздней ошибке.
func prepareSocketRoot() error {
	if os.Getuid() != socketUID || os.Getgid() != socketGID {
		return errors.New("socket init process uid/gid mismatch")
	}
	parent := filepath.Dir(socketRoot)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect socket parent: %w", err)
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("socket parent is not a real directory")
	}
	info, err := os.Lstat(socketRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(socketRoot, 0o770|os.ModeSticky); err != nil {
			return fmt.Errorf("create socket root: %w", err)
		}
	case err != nil:
		return fmt.Errorf("inspect socket root: %w", err)
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return errors.New("socket root is not a real directory")
	}
	if err := os.Chmod(socketRoot, 0o770|os.ModeSticky); err != nil {
		return fmt.Errorf("set socket root mode: %w", err)
	}
	if err := validateRoot(); err != nil {
		return err
	}
	for _, name := range []string{"issuer.sock", "verifier.sock"} {
		if err := removeStale(filepath.Join(socketRoot, name)); err != nil {
			return err
		}
	}
	return installReadinessProbe()
}

// validateRoot checks the prepared root's Lstat owner/group, exact 0770 permission bits and sticky bit against the fixed profile. It does not create, chown or recursively validate directory contents; parent/root type checks belong to preparation.
// validateRoot проверяет по Lstat владельца и группу подготовленного корня, точные права 0770 и sticky-бит по фиксированному профилю. Каталог не создаётся, chown и рекурсивная проверка содержимого не выполняются; тип родителя и корня проверяется подготовкой.
func validateRoot() error {
	info, err := os.Lstat(socketRoot)
	if err != nil {
		return fmt.Errorf("inspect prepared socket root: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		stat.Uid != socketUID ||
		stat.Gid != socketGID ||
		info.Mode().Perm() != 0o770 ||
		info.Mode()&os.ModeSticky == 0 {
		return errors.New("prepared socket root ownership or mode mismatch")
	}
	return nil
}

// removeStale treats an absent path as success, rejects a symlink or any nonsocket entry and removes an existing socket path. It does not connect to the socket, determine liveness or recursively delete directories.
// removeStale считает отсутствие пути успехом, отклоняет ссылку или любой объект не типа socket и удаляет существующий путь сокета. Подключение к сокету, определение активности и рекурсивное удаление каталогов не выполняются.
func removeStale(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect stale socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("socket path contains a symlink or non-socket")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

// installReadinessProbe checks the source's initial regular nonsymlink type and nonzero size up to sixty-four MiB, rejects unsafe existing targets, and removes an old regular target. It copies to an exclusive 0550 .starting file, syncs, closes and renames it, cleaning that temporary file on copy/sync/close/rename failure. It does not rehash, bound a growing source during ReadFrom, restore an old target or sync the parent directory.
// installReadinessProbe проверяет исходные тип обычного файла без ссылки и ненулевой размер до шестидесяти четырёх МиБ, отклоняет небезопасные существующие цели и удаляет старый обычный целевой файл. Копия пишется в эксклюзивный .starting с режимом 0550, синхронизируется, закрывается и переименовывается; временный файл удаляется при ошибках копирования, sync, close или rename. Пересчёт хеша, ограничение растущего источника во время ReadFrom, восстановление старой цели и sync родительского каталога не выполняются.
func installReadinessProbe() error {
	sourceInfo, err := os.Lstat(readinessSource)
	if err != nil ||
		!sourceInfo.Mode().IsRegular() ||
		sourceInfo.Mode()&os.ModeSymlink != 0 ||
		sourceInfo.Size() <= 0 ||
		sourceInfo.Size() > 64<<20 {
		return errors.New("local readiness source binary is unsafe")
	}
	if targetInfo, targetErr := os.Lstat(readinessTarget); targetErr == nil {
		if !targetInfo.Mode().IsRegular() ||
			targetInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("local readiness target is unsafe")
		}
		if err := os.Remove(readinessTarget); err != nil {
			return fmt.Errorf("remove stale local readiness binary: %w", err)
		}
	} else if !errors.Is(targetErr, os.ErrNotExist) {
		return fmt.Errorf("inspect local readiness target: %w", targetErr)
	}
	source, err := os.Open(readinessSource)
	if err != nil {
		return fmt.Errorf("open local readiness source: %w", err)
	}
	defer source.Close()
	temporary := readinessTarget + ".starting"
	target, err := os.OpenFile(
		temporary,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o550,
	)
	if err != nil {
		return fmt.Errorf("create local readiness target: %w", err)
	}
	if _, err := target.ReadFrom(source); err != nil {
		_ = target.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("copy local readiness binary: %w", err)
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("sync local readiness binary: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close local readiness binary: %w", err)
	}
	if err := os.Rename(temporary, readinessTarget); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish local readiness binary: %w", err)
	}
	return nil
}
