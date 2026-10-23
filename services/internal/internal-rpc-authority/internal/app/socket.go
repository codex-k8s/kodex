package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ownedUnixListener keeps the bound Unix listener and the published pathname that its explicit Close will remove. The pathname is tracked separately because publication renames the initially bound temporary socket and automatic unlink is disabled.
// ownedUnixListener хранит привязанный Unix-listener и опубликованный путь, удаляемый явным Close. Путь учитывается отдельно, потому что публикация переименовывает первоначальный временный сокет, а автоматический unlink отключён.
type ownedUnixListener struct {
	*net.UnixListener
	path string
}

// listenUnix validates the fixed-profile parent, rejects/removes an existing socket according to the stale-path policy, binds a PID-specific temporary Unix path and disables automatic unlink. It chmods and verifies the temporary socket's exact owner/group/mode before rename publication, cleaning temporary state on failures after bind. This constructs a real listener; it does not start gRPC or perform an application authorization handshake.
// listenUnix проверяет родительский каталог фиксированного профиля, отклоняет либо удаляет существующий сокет по политике старого пути, привязывает временный Unix-путь с PID и отключает автоматический unlink. До публикации rename задаёт режим и проверяет точные владельца, группу и права временного сокета, очищая временное состояние при ошибках после bind. Создаётся настоящий listener, но gRPC не запускается и прикладная авторизация не проверяется.
func listenUnix(config Config) (*ownedUnixListener, error) {
	if err := validateSocketRoot(filepath.Dir(config.SocketPath)); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(config.SocketPath); err != nil {
		return nil, err
	}
	temporaryPath := fmt.Sprintf("%s.starting-%d", config.SocketPath, os.Getpid())
	if err := removeKnownTemporarySocket(temporaryPath); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: temporaryPath,
		Net:  "unix",
	})
	if err != nil {
		return nil, fmt.Errorf("listen on temporary authority UDS: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	cleanup := func() {
		_ = listener.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := os.Chmod(temporaryPath, config.SocketMode); err != nil {
		cleanup()
		return nil, fmt.Errorf("set authority UDS mode: %w", err)
	}
	if err := validateSocketIdentity(
		temporaryPath,
		config.ExpectedProcessUID,
		config.ExpectedProcessGID,
		config.SocketMode,
	); err != nil {
		cleanup()
		return nil, err
	}
	if err := os.Rename(temporaryPath, config.SocketPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("publish authority UDS atomically: %w", err)
	}
	return &ownedUnixListener{UnixListener: listener, path: config.SocketPath}, nil
}

// Close closes the Unix listener and attempts to remove its saved published pathname even if closing failed, ignoring an already absent path and joining other removal errors. It does not recheck pathname inode ownership before unlink or provide a separate idempotent-close guarantee.
// Close закрывает Unix-listener и пытается удалить сохранённый опубликованный путь даже при ошибке закрытия, игнорируя уже отсутствующий путь и объединяя остальные ошибки удаления. Перед unlink идентичность inode по пути повторно не проверяется, отдельная гарантия идемпотентного Close не предоставляется.
func (listener *ownedUnixListener) Close() error {
	closeErr := listener.UnixListener.Close()
	removeErr := os.Remove(listener.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return errors.Join(closeErr, fmt.Errorf("remove authority UDS: %w", removeErr))
	}
	return closeErr
}

// validateSocketRoot requires a real nonsymlink directory whose Lstat UID/GID are exactly 29000, permission bits exactly 0770 and sticky bit set. It validates existing metadata only, without creating/chmodding/chowning the path or checking its contents recursively.
// validateSocketRoot требует настоящий каталог без ссылки, точные UID и GID 29000 по Lstat, права ровно 0770 и sticky-бит. Проверяются только существующие метаданные без создания, chmod, chown или рекурсивной проверки содержимого.
func validateSocketRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect authority UDS root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("authority UDS root is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		stat.Uid != 29000 ||
		stat.Gid != 29000 ||
		info.Mode().Perm() != 0o770 ||
		info.Mode()&os.ModeSticky == 0 {
		return errors.New("authority UDS root ownership or mode mismatch")
	}
	return nil
}

// removeStaleSocket treats absence as success, rejects symlink/nonsocket entries and refuses removal when a two-hundred-millisecond Unix dial succeeds. Any dial failure leads to a removal attempt; that branch is not proof that a listener is inactive, and this helper does not recursively remove directories.
// removeStaleSocket считает отсутствие успехом, отклоняет ссылку или объект не типа socket и запрещает удаление при успешном Unix dial со сроком двести миллисекунд. Любая ошибка dial ведёт к попытке удаления; эта ветвь не доказывает неактивность listener, рекурсивного удаления каталогов нет.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing authority UDS: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("existing authority UDS path is a symlink or non-socket")
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("authority UDS is already active")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale authority UDS: %w", err)
	}
	return nil
}

// removeKnownTemporarySocket removes only an existing nonsymlink socket at the caller-selected known temporary pathname, accepts absence and rejects any other entry type. It performs no liveness dial; the caller must supply its own PID-specific temporary path.
// removeKnownTemporarySocket удаляет только существующий сокет без ссылки по выбранному вызывающим известному временному пути, принимает отсутствие и отклоняет другие типы. Dial активности не выполняется; вызывающий должен передать собственный временный путь с PID.
func removeKnownTemporarySocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect temporary authority UDS: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("temporary authority UDS path is unsafe")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove temporary authority UDS: %w", err)
	}
	return nil
}

// validateSocketIdentity compares Lstat socket type, symlink absence, UID/GID and permission bits to the exact supplied expected profile. It does not mutate the file, authenticate a connected peer or prove that the socket's server is ready.
// validateSocketIdentity сравнивает тип socket по Lstat, отсутствие ссылки, UID, GID и права с точным переданным ожидаемым профилем. Файл не меняется, подключённый партнёр не аутентифицируется, готовность сервера сокета не доказывается.
func validateSocketIdentity(
	path string,
	expectedUID uint32,
	expectedGID uint32,
	expectedMode os.FileMode,
) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect created authority UDS: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 ||
		stat.Uid != expectedUID ||
		stat.Gid != expectedGID ||
		info.Mode().Perm() != expectedMode.Perm() {
		return errors.New("created authority UDS ownership, type or mode mismatch")
	}
	return nil
}
