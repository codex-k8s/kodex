// Package managedworkspace owns bounded local files and explicitly selected commands;
// current execution authority is supplied by the controller, never by model arguments.
// Пакет managedworkspace обслуживает ограниченные локальные файлы и выбранные команды;
// актуальный допуск исполнения выдаёт controller, а не аргументы модели.
package managedworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacefs "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// maximumFileBytes bounds reads and replacements independently of the whole workspace quota.
// maximumFileBytes ограничивает чтение и замену файла независимо от общей квоты workspace.
const maximumFileBytes = 1 << 20

// errFiles refuses unsafe paths, metadata, quotas and compare-and-swap conflicts without paths.
// errFiles закрыто отклоняет пути, metadata, превышение квот и CAS conflict без раскрытия путей.
var errFiles = errors.New("managed workspace file operation denied")

// fileStore retains only immutable input and trusted filesystem identities. It has no secrets,
// command execution or independent authorization; its caller must serialize admitted operations.
// fileStore хранит immutable input и доверенные filesystem identities. Секретов, исполнения команд
// и самостоятельной авторизации нет; caller обязан сериализовать допущенные операции.
type fileStore struct {
	input                       runtimecontract.RunnerInput
	root                        string
	runnerUID, writerUID, group uint32
}

// File describes a regular file or directory in the bounded local view without following links.
// File описывает обычный файл или каталог ограниченного локального вида без следования ссылкам.
type File struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size_bytes"`
}

// FileRead returns a bounded UTF-8 chunk and the digest of the complete admitted file, so edits
// can require the exact previous bytes. An empty last chunk is distinct from missing data.
// FileRead возвращает ограниченный UTF-8 chunk и digest полного допущенного файла для CAS edit.
// Пустой последний chunk отличается от отсутствующих данных.
type FileRead struct {
	Text       string `json:"text"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size_bytes"`
	Offset     int64  `json:"offset_bytes"`
	NextOffset int64  `json:"next_offset_bytes"`
}

// lock opens the own work/outbox base under the shared writer lock. The one bound PVC mount
// exception reuses the existing protected mount proof; all descendants retain NO_XDEV.
// lock открывает свой work/outbox под общей writer-блокировкой. Единственное исключение PVC
// использует существующее protected mount proof; для потомков сохраняется NO_XDEV.
func (store *fileStore) lock(ctx context.Context, selector string) (*os.File, *os.File, string, error) {
	if _, ok := runtimecontract.ManagedWorkspacePath(store.input, selector); !ok {
		return nil, nil, "", errFiles
	}
	lock, err := workspacefs.Lock(ctx, store.root)
	if err != nil {
		return nil, nil, "", errFiles
	}
	base, relative := "work", strings.TrimPrefix(selector, "work")
	if selector == "outbox" || strings.HasPrefix(selector, "outbox/") {
		base = ".kodex/outbox"
		relative = strings.TrimPrefix(selector, "outbox")
	}
	relative = strings.TrimPrefix(relative, "/")
	var fd int
	if base == "work" && store.input.WorkspaceStorage != nil {
		fd, err = workspacefs.OpenBoundWorkDirectory(int(lock.Fd()), store.input)
	} else {
		fd, err = store.openDirectory(int(lock.Fd()), base)
	}
	if err != nil {
		lock.Close()
		return nil, nil, "", errFiles
	}
	return lock, os.NewFile(uintptr(fd), "managed-workspace-base"), relative, nil
}

// openDirectory rejects links and mount changes in the kernel, then checks the trusted shared
// UID/GID boundary. Selectors are validated separately; names are never included in errors.
// openDirectory отклоняет ссылки и mount changes ядром и проверяет доверенные UID/GID.
// Selectors проверяются отдельно; имена никогда не включаются в ошибки.
func (store *fileStore) openDirectory(parent int, relative string) (int, error) {
	fd, err := unix.Openat2(parent, relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return -1, errFiles
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Gid != store.group || (info.Uid != store.runnerUID && info.Uid != store.writerUID) || info.Mode&0o002 != 0 {
		unix.Close(fd)
		return -1, errFiles
	}
	return fd, nil
}

// readAt opens a bounded single-link regular file with descriptor-relative confinement. It
// rejects devices, FIFOs, sockets, foreign owners and changes observed during the exact read.
// readAt открывает ограниченный single-link regular file через confined descriptor. Devices,
// FIFO, sockets, чужие владельцы и замеченные изменения во время чтения отклоняются.
func (store *fileStore) readAt(parent int, name string) ([]byte, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, errFiles
	}
	file := os.NewFile(uintptr(fd), "managed-workspace-file")
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Gid != store.group ||
		(before.Uid != store.runnerUID && before.Uid != store.writerUID) || before.Mode&0o002 != 0 || before.Size < 0 || before.Size > maximumFileBytes {
		return nil, errFiles
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumFileBytes+1))
	if err != nil || int64(len(data)) != before.Size || unix.Fstat(fd, &after) != nil || !sameFileMetadata(before, after) {
		return nil, errFiles
	}
	return data, nil
}

// read selects a UTF-8 chunk without splitting codepoints and commits to the entire file's hash.
// read выбирает UTF-8 chunk без разрыва codepoints и связывает результат хешем полного файла.
func (store *fileStore) read(ctx context.Context, selector string, offset, maximum int64) (FileRead, error) {
	if offset < 0 || maximum < 4 || maximum > 32768 {
		return FileRead{}, errFiles
	}
	lock, base, relative, err := store.lock(ctx, selector)
	if err != nil {
		return FileRead{}, err
	}
	defer lock.Close()
	defer base.Close()
	if relative == "" {
		return FileRead{}, errFiles
	}
	data, err := store.readAt(int(base.Fd()), relative)
	if err != nil || !utf8.Valid(data) || offset > int64(len(data)) || (offset < int64(len(data)) && !utf8.RuneStart(data[offset])) {
		return FileRead{}, errFiles
	}
	end := min(offset+maximum, int64(len(data)))
	for end < int64(len(data)) && end > offset && !utf8.RuneStart(data[end]) {
		end--
	}
	digest := sha256.Sum256(data)
	return FileRead{Text: string(data[offset:end]), SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data)), Offset: offset, NextOffset: end}, nil
}

// list enumerates only one confined directory, validates every entry and returns a stable page
// ordered by name. Pagination cursors carry no authority and never choose a filesystem root.
// list перечисляет один confined каталог, проверяет каждую запись и возвращает стабильную страницу
// по имени. Cursor пагинации не даёт полномочий и не выбирает filesystem root.
func (store *fileStore) list(ctx context.Context, selector, after string) ([]File, bool, error) {
	lock, base, relative, err := store.lock(ctx, selector)
	if err != nil {
		return nil, false, err
	}
	defer lock.Close()
	defer base.Close()
	directory := base
	if relative != "" {
		fd, e := store.openDirectory(int(base.Fd()), relative)
		if e != nil {
			return nil, false, e
		}
		directory = os.NewFile(uintptr(fd), "managed-workspace-list")
		defer directory.Close()
	}
	entries, err := directory.ReadDir(int(store.input.WorkspacePolicy.MaximumFileCount) + 1)
	if (err != nil && !errors.Is(err, io.EOF)) || int64(len(entries)) > store.input.WorkspacePolicy.MaximumFileCount {
		return nil, false, errFiles
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	result := make([]File, 0, min(len(entries), 200))
	more := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, false, errFiles
		}
		var info unix.Stat_t
		if unix.Fstatat(int(directory.Fd()), entry.Name(), &info, unix.AT_SYMLINK_NOFOLLOW) != nil || info.Gid != store.group || (info.Uid != store.runnerUID && info.Uid != store.writerUID) || info.Mode&0o002 != 0 {
			return nil, false, errFiles
		}
		kind := "FILE"
		switch info.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			kind = "DIRECTORY"
		case unix.S_IFREG:
			if info.Nlink != 1 {
				return nil, false, errFiles
			}
		default:
			return nil, false, errFiles
		}
		if !utf8.ValidString(entry.Name()) || strings.ContainsAny(entry.Name(), "\n\r\\") {
			return nil, false, errFiles
		}
		if entry.Name() <= after {
			continue
		}
		if len(result) == 200 {
			more = true
			break
		}
		result = append(result, File{Path: selector + "/" + entry.Name(), Kind: kind, Size: info.Size})
	}
	return result, more, nil
}

// parent creates missing directories only under the already confined base. Each open rechecks
// metadata and each creation is durable; earlier empty directories may remain after later failure.
// parent создаёт отсутствующие каталоги только под confined base. Каждый open проверяет metadata,
// каждое создание durable; при позднем отказе ранее созданные пустые каталоги могут сохраниться.
func (store *fileStore) parent(base int, relative string, remaining int64) (*os.File, string, error) {
	parts := strings.Split(relative, "/")
	fd, err := unix.Dup(base)
	if err != nil {
		return nil, "", errFiles
	}
	for _, name := range parts[:len(parts)-1] {
		next, e := store.openDirectory(fd, name)
		if e != nil {
			if remaining < 1 {
				unix.Close(fd)
				return nil, "", errFiles
			}
			remaining--
			if unix.Mkdirat(fd, name, 0o2770) != nil {
				unix.Close(fd)
				return nil, "", errFiles
			}
			if unix.Fsync(fd) != nil {
				unix.Close(fd)
				return nil, "", errFiles
			}
			next, e = store.openDirectory(fd, name)
			if e == nil && unix.Fchmod(next, 0o2770) != nil {
				unix.Close(next)
				unix.Close(fd)
				return nil, "", errFiles
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, "", errFiles
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), "managed-workspace-parent"), parts[len(parts)-1], nil
}

// usage accounts for every regular file and directory recursively, refusing unsafe entries and
// bounding enumeration with the remaining quota. It reads metadata only and never follows links.
// usage учитывает каждый regular file и каталог рекурсивно, отклоняет небезопасные записи и
// ограничивает перечисление оставшейся квотой. Читает лишь metadata без следования ссылкам.
func (store *fileStore) usage(ctx context.Context, fd int, files, bytes *int64) error {
	if ctx.Err() != nil || *files >= store.input.WorkspacePolicy.MaximumFileCount {
		return errFiles
	}
	*files += 1
	duplicate, err := store.openDirectory(fd, ".")
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(duplicate), "managed-workspace-usage")
	defer directory.Close()
	entries, err := directory.ReadDir(int(store.input.WorkspacePolicy.MaximumFileCount-*files) + 1)
	if (err != nil && !errors.Is(err, io.EOF)) || int64(len(entries)) > store.input.WorkspacePolicy.MaximumFileCount-*files {
		return errFiles
	}
	for _, entry := range entries {
		var info unix.Stat_t
		if ctx.Err() != nil || unix.Fstatat(fd, entry.Name(), &info, unix.AT_SYMLINK_NOFOLLOW) != nil || info.Gid != store.group || (info.Uid != store.runnerUID && info.Uid != store.writerUID) || info.Mode&0o002 != 0 {
			return errFiles
		}
		switch info.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			child, e := store.openDirectory(fd, entry.Name())
			if e != nil {
				return e
			}
			e = store.usage(ctx, child, files, bytes)
			unix.Close(child)
			if e != nil {
				return e
			}
		case unix.S_IFREG:
			if info.Nlink != 1 || info.Size < 0 {
				return errFiles
			}
			*files += 1
			if info.Size > store.input.WorkspacePolicy.MaximumWritableBytes-*bytes {
				return errFiles
			}
			*bytes += info.Size
		default:
			return errFiles
		}
		if *files > store.input.WorkspacePolicy.MaximumFileCount || *bytes > store.input.WorkspacePolicy.MaximumWritableBytes {
			return errFiles
		}
	}
	return nil
}

// write atomically replaces an exact previous hash, or creates only an absent path. It checks
// whole-workspace quotas before publication and fsyncs bytes and parent; no write is auto-retried.
// write атомарно заменяет точный предыдущий hash либо создаёт лишь отсутствующий путь. Проверяет
// общие квоты до публикации, fsync bytes и parent; автоматического повторения записи нет.
func (store *fileStore) write(ctx context.Context, selector, text, expected string) (string, error) {
	if !utf8.ValidString(text) {
		return "", errFiles
	}
	return store.writeBytes(ctx, selector, []byte(text), expected)
}

// writeBytes is the internal binary-safe snapshot writer under the same quotas and CAS rules.
// Callers supply verified source bytes; the public edit tool accepts UTF-8 only.
// writeBytes — внутренний binary-safe snapshot writer с теми же квотами и CAS. Callers передают
// проверенные source bytes; публичный edit tool допускает только UTF-8.
func (store *fileStore) writeBytes(ctx context.Context, selector string, data []byte, expected string) (string, error) {
	text := string(data)
	if len(text) > maximumFileBytes || (expected != "ABSENT" && len(expected) != 64) {
		return "", errFiles
	}
	lock, base, relative, err := store.lock(ctx, selector)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	defer base.Close()
	if relative == "" {
		return "", errFiles
	}
	var files, bytes int64
	files, bytes, err = store.totalUsage(ctx, int(lock.Fd()))
	if err != nil {
		return "", err
	}
	remaining := int64(0)
	if expected == "ABSENT" {
		remaining = store.input.WorkspacePolicy.MaximumFileCount - files - 1
	}
	parent, name, err := store.parent(int(base.Fd()), relative, remaining)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	var before unix.Stat_t
	statErr := unix.Fstatat(int(parent.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW)
	oldBytes := int64(0)
	additional := int64(1)
	if expected == "ABSENT" {
		if !errors.Is(statErr, unix.ENOENT) {
			return "", errFiles
		}
	} else {
		if statErr != nil {
			return "", errFiles
		}
		old, e := store.readAt(int(parent.Fd()), name)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(old)
		if hex.EncodeToString(sum[:]) != expected {
			return "", errFiles
		}
		oldBytes = int64(len(old))
		additional = 0
	}
	files, bytes = 0, 0
	files, bytes, err = store.totalUsage(ctx, int(lock.Fd()))
	if err != nil {
		return "", err
	}
	if files+additional > store.input.WorkspacePolicy.MaximumFileCount || bytes-oldBytes+int64(len(text)) > store.input.WorkspacePolicy.MaximumWritableBytes {
		return "", errFiles
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", errFiles
	}
	temporary := ".kodex-write-" + hex.EncodeToString(random)
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o660)
	if err != nil {
		return "", errFiles
	}
	file := os.NewFile(uintptr(fd), "managed-workspace-write")
	defer file.Close()
	defer unix.Unlinkat(int(parent.Fd()), temporary, 0)
	mode := os.FileMode(0o660)
	if strings.HasPrefix(selector, "outbox/") {
		mode = 0o640
	}
	if err := file.Chmod(mode); err != nil {
		return "", errFiles
	}
	if _, err := io.WriteString(file, text); err != nil || file.Sync() != nil {
		return "", errFiles
	}
	flags := uint(0)
	if expected == "ABSENT" {
		flags = unix.RENAME_NOREPLACE
	}
	if ctx.Err() != nil || unix.Renameat2(int(parent.Fd()), temporary, int(parent.Fd()), name, flags) != nil || unix.Fsync(int(parent.Fd())) != nil {
		return "", errFiles
	}
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:]), nil
}

// openBaseUnderLock opens only the two fixed quota roots while the caller already owns the lock.
// openBaseUnderLock открывает лишь два фиксированных корня квот под уже удерживаемой блокировкой.
func (store *fileStore) openBaseUnderLock(root int, name string) (*os.File, error) {
	relative := name
	if name == "outbox" {
		relative = path.Join(".kodex", name)
	}
	var fd int
	var err error
	if name == "work" && store.input.WorkspaceStorage != nil {
		fd, err = workspacefs.OpenBoundWorkDirectory(root, store.input)
	} else {
		fd, err = store.openDirectory(root, relative)
	}
	if err != nil {
		return nil, errFiles
	}
	return os.NewFile(uintptr(fd), "managed-workspace-quota"), nil
}

// sameFileMetadata ignores read access time but binds identity, type, ownership, size and both
// change timestamps. It detects observed races without claiming a snapshot against rogue writers.
// sameFileMetadata игнорирует atime чтения, связывая identity, type, owner, размер и timestamps
// изменения. Обнаруживает замеченные races, но не обещает snapshot против произвольного writer.
func sameFileMetadata(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Nlink == b.Nlink && a.Uid == b.Uid && a.Gid == b.Gid && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// totalUsage counts both fixed mutable roots with one shared file/byte budget under the existing
// writer lock. It never enumerates private state, input catalogs or knowledge volumes.
// totalUsage считает оба fixed mutable roots с общим file/byte budget под существующим writer
// lock. Private state, input catalogs и knowledge volumes не перечисляются.
func (store *fileStore) totalUsage(ctx context.Context, root int) (files, bytes int64, resultErr error) {
	for _, name := range []string{"work", "outbox"} {
		directory, err := store.openBaseUnderLock(root, name)
		if err != nil {
			return 0, 0, err
		}
		err = store.usage(ctx, int(directory.Fd()), &files, &bytes)
		directory.Close()
		if err != nil {
			return 0, 0, err
		}
	}
	return files, bytes, nil
}
