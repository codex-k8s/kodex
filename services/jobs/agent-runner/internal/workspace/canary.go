// Package workspace probes declared writable mounts and coordinates service-owned filesystem operations without replacing runtime policy enforcement.
// Пакет workspace проверяет объявленные writable mounts и согласует служебные файловые операции, не заменяя применение runtime policy.
package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// Denial carries a readiness reason supplied by workspace checks; it deliberately omits filesystem paths and wrapped OS error details.
// Denial хранит причину отказа готовности от проверок workspace; намеренно не включает файловые пути и детали исходной ошибки ОС.
type Denial struct{ Reason string }

const (
	// canaryInitialPayload is the inert first file content used to prove exclusive creation, sync and exact readback in the private nonce directory.
	// canaryInitialPayload — неактивное начальное содержимое для проверки исключительного создания, sync и точного чтения в приватном nonce-каталоге.
	canaryInitialPayload = "kodex-workspace-create\n"
	// canaryReplacementPayload differs from the initial bytes so readback detects a failed atomic replacement rather than accepting the original file.
	// canaryReplacementPayload отличается от исходных байтов, позволяя чтению обнаружить неуспешную атомарную замену вместо принятия прежнего файла.
	canaryReplacementPayload = "kodex-workspace-replace\n"
)

// ResultProvenance records the revision, attempt and execution digest in the workspace-write result; publishing validates its shape, not server-owned authenticity or grant validity.
// ResultProvenance записывает ревизию, попытку и дайджест исполнения в результат записи workspace; публикация проверяет форму, но не серверную подлинность или действительность grant.
type ResultProvenance struct {
	Schema                 string `json:"schema"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	Attempt                int32  `json:"attempt"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
}

// Error formats the stored reason with a fixed workspace-readiness prefix; callers supply canonical reasons rather than raw OS errors or paths.
// Error форматирует сохранённую причину с фиксированным префиксом готовности workspace; вызывающие передают канонические причины вместо сырых ошибок ОС и путей.
func (denial *Denial) Error() string { return "workspace readiness denied: " + denial.Reason }

// DenialReason extracts a constructed workspace Denial through wrapped errors and maps other errors to the generic I/O reason; it does not sanitize arbitrary values of Denial.Reason.
// DenialReason извлекает созданный workspace Denial из обёрнутых ошибок и заменяет прочие общей причиной I/O; произвольное содержимое Denial.Reason не очищает.
func DenialReason(err error) string {
	var denial *Denial
	if errors.As(err, &denial) {
		return denial.Reason
	}
	return runtimecontract.RuntimeWorkspaceIOError
}

// RunCanary validates a clean absolute root and workspace policy, takes the cooperative directory lock, excludes protected read-only areas from writable usage and tests private outbox creation/replacement/deletion within quota. It is a readiness probe, not enforcement of all provider writes.
// RunCanary проверяет чистый абсолютный root и workspace policy, берёт согласующий directory lock, исключает защищённые read-only области из writable usage и проверяет создание/замену/удаление в приватном outbox в пределах квоты. Это проба готовности, не контроль всех записей provider.
func RunCanary(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy) error {
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || policy.Validate() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	lock, err := Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	for _, candidate := range []string{"/workspace/input/readiness", "/workspace/knowledge/readiness", runtimecontract.RuntimeContextRoot + "/readiness"} {
		access, reason := policy.AccessForPath(candidate)
		if reason != "" || access != runtimecontract.RuntimeWorkspaceReadOnly {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
		}
	}
	usage, files, err := writableUsageContext(ctx, root, policy)
	if err != nil {
		return err
	}
	if !withinQuota(usage, files, int64(len(canaryInitialPayload)+len(canaryReplacementPayload)), policy) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	}
	directory, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return classify(err)
	}
	defer unix.Close(directory)
	return runDirectoryCanary(ctx, directory)
}

// runDirectoryCanary creates a random private child directory, proves exclusive write/fsync/readback, atomic rename and deletion, and makes best-effort cleanup on every exit. It accesses only its nonce-owned files, never pre-existing user files.
// runDirectoryCanary создаёт случайный приватный подкаталог, проверяет исключительную запись/fsync/чтение, атомарный rename и удаление, выполняя best-effort очистку при любом выходе. Обращается лишь к своим nonce-файлам, не к существующим файлам пользователя.
func runDirectoryCanary(ctx context.Context, directory int) error {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	nested := ".readiness-" + stringHex(nonce)
	if err := unix.Mkdirat(directory, nested, 0o700); err != nil {
		return classify(err)
	}
	defer unix.Unlinkat(directory, nested, unix.AT_REMOVEDIR)
	nestedDirectory, err := unix.Openat(directory, nested, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return classify(err)
	}
	defer unix.Close(nestedDirectory)
	// current is the nonce-local file used for exact pre/post-replacement readback.
	// current — файл внутри nonce-каталога для точного чтения до и после замены.
	const current = "current.txt"
	// temporary is the exclusive nonce-local source for the atomic replacement.
	// temporary — исключительный источник атомарной замены внутри nonce-каталога.
	const temporary = "current.txt.next"
	defer unix.Unlinkat(nestedDirectory, temporary, 0)
	defer unix.Unlinkat(nestedDirectory, current, 0)
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	file, err := unix.Openat(nestedDirectory, current, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, []byte(canaryInitialPayload)); err == nil {
		err = unix.Fsync(file)
	}
	closeErr := unix.Close(file)
	if err != nil {
		return classify(err)
	}
	if closeErr != nil {
		return classify(closeErr)
	}
	if err := verifyFile(nestedDirectory, current, canaryInitialPayload); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	file, err = unix.Openat(nestedDirectory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, []byte(canaryReplacementPayload)); err == nil {
		err = unix.Fsync(file)
	}
	closeErr = unix.Close(file)
	if err != nil || closeErr != nil {
		return classify(errors.Join(err, closeErr))
	}
	if err = unix.Renameat(nestedDirectory, temporary, nestedDirectory, current); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(nestedDirectory); err != nil {
		return classify(err)
	}
	if err := verifyFile(nestedDirectory, current, canaryReplacementPayload); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if err = unix.Unlinkat(nestedDirectory, current, 0); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(nestedDirectory); err != nil {
		return classify(err)
	}
	if err = unix.Unlinkat(directory, nested, unix.AT_REMOVEDIR); err != nil {
		return classify(err)
	}
	return nil
}

// verifyFile opens a relative name without following its final symlink, requires a regular single-link inode and compares exact bytes using a one-extra-byte buffer, closing the descriptor on all paths.
// verifyFile открывает относительное имя без перехода по конечной символьной ссылке, требует обычный inode с одной ссылкой и сравнивает точные байты буфером с одним запасным байтом, закрывая дескриптор на всех путях.
func verifyFile(directory int, name, expected string) error {
	file, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return classify(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(file, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		_ = unix.Close(file)
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	buffer := make([]byte, len(expected)+1)
	read, readErr := readBounded(file, buffer)
	closeErr := unix.Close(file)
	if readErr != nil || closeErr != nil || read != len(expected) || string(buffer[:read]) != expected {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return nil
}

// PublishResult validates provenance shape and writable policy, locks the workspace, checks usage and atomically replaces the result via a synced exclusive temporary file. It syncs the directory and cleans failed temporary writes; it neither signs provenance nor grants execution authority.
// PublishResult проверяет форму provenance и право записи policy, блокирует workspace, сверяет usage и атомарно заменяет результат через синхронизированный исключительный временный файл. Синхронизирует каталог и очищает неудачные временные записи; не подписывает provenance и не даёт полномочий исполнения.
func PublishResult(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy, provenance ResultProvenance) error {
	if provenance.Schema != "kodex.workspace-write-result.v1" || provenance.RuntimeRevisionRef == "" ||
		provenance.RuntimeRevisionVersion < 1 || !validSHA256(provenance.RuntimeRevisionDigest) ||
		provenance.Attempt < 1 || !validSHA256(provenance.ExecutionBindingDigest) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	raw, err := json.Marshal(provenance)
	if err != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	raw = append(raw, '\n')
	if access, reason := policy.AccessForPath(".kodex/outbox/workspace-write-result.json"); reason != "" || access != runtimecontract.RuntimeWorkspaceWritable {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
	}
	lock, err := Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	directory, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return classify(err)
	}
	defer unix.Close(directory)
	usage, files, err := writableUsage(root, policy)
	if err != nil {
		return err
	}
	if !withinQuota(usage, files, int64(len(raw)), policy) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	}
	// temporary names the lock-protected outbox staging file, never a caller path.
	// temporary задаёт защищённый lock промежуточный файл outbox, а не путь клиента.
	const temporary = ".workspace-write-result.next"
	// committed is the fixed observable result replaced only after a successful sync.
	// committed — фиксированный наблюдаемый результат, заменяемый лишь после успешного sync.
	const committed = "workspace-write-result.json"
	_ = unix.Unlinkat(directory, temporary, 0)
	file, err := unix.Openat(directory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, raw); err == nil {
		err = unix.Fsync(file)
	}
	closeErr := unix.Close(file)
	if err != nil || closeErr != nil {
		_ = unix.Unlinkat(directory, temporary, 0)
		return classify(errors.Join(err, closeErr))
	}
	if err = unix.Renameat(directory, temporary, directory, committed); err != nil {
		_ = unix.Unlinkat(directory, temporary, 0)
		return classify(err)
	}
	if err = unix.Fsync(directory); err != nil {
		return classify(err)
	}
	return nil
}

// validSHA256 accepts exactly thirty-two hex-decoded bytes, including either letter case, without checking the digest against file content.
// validSHA256 принимает ровно тридцать два декодированных hex-байта с любым регистром букв без сверки дайджеста с содержимым.
func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// writeFull writes all bytes to a Unix descriptor, retries EINTR and returns a fixed error on failed or non-progressing writes; it does not own or close the descriptor.
// writeFull записывает все байты в Unix-дескриптор, повторяет EINTR и возвращает фиксированную ошибку при отказе или отсутствии прогресса; дескриптором не владеет и не закрывает.
func writeFull(file int, payload []byte) error {
	for len(payload) != 0 {
		written, err := unix.Write(file, payload)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || written <= 0 {
			return errors.New("workspace write is incomplete")
		}
		payload = payload[written:]
	}
	return nil
}

// readBounded fills at most the supplied buffer, retries EINTR and returns bytes read on EOF or error without allocating beyond the caller's bound.
// readBounded заполняет не более переданного буфера, повторяет EINTR и возвращает прочитанное при EOF или ошибке без выделения сверх предела вызывающего.
func readBounded(file int, buffer []byte) (int, error) {
	total := 0
	for total < len(buffer) {
		read, err := unix.Read(file, buffer[total:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return total, err
		}
		if read == 0 {
			return total, nil
		}
		total += read
	}
	return total, nil
}

// writableUsage performs the same bounded policy-aware walk without cancellation checks; it is used after the publisher has acquired the workspace lock.
// writableUsage выполняет тот же ограниченный обход с policy без проверок отмены; используется после получения publisher блокировки workspace.
func writableUsage(root string, policy runtimecontract.RuntimeWorkspacePolicy) (int64, int64, error) {
	return writableUsageContext(nil, root, policy)
}

// writableUsageContext counts regular-file bytes and files while bounding all visited writable entries, checking cancellation and rejecting encountered symlinks or policy escapes. It skips read-only directories and the separate session-state subtree, so it is not a whole-volume quota or race-proof filesystem snapshot.
// writableUsageContext считает байты и обычные файлы, ограничивая все посещённые writable entries, проверяя отмену и отклоняя встреченные symlinks и выход из policy. Пропускает read-only каталоги и отдельное дерево session state; не является квотой всего тома или защищённым от гонок снимком ФС.
func writableUsageContext(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy) (int64, int64, error) {
	var bytes, files, entries int64
	err := filepath.WalkDir(root, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if ctx != nil && ctx.Err() != nil {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
		}
		if walkErr != nil {
			return classify(walkErr)
		}
		relative, err := filepath.Rel(root, localPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if !strings.HasPrefix(filepath.ToSlash(relative), "work/") {
				return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
			}
			directory, openErr := openDirectory(root, "work")
			if openErr != nil {
				return classify(openErr)
			}
			linkErr := CheckWorkLink(directory, strings.TrimPrefix(filepath.ToSlash(relative), "work/"))
			unix.Close(directory)
			if linkErr != nil {
				return linkErr
			}
		}
		// Session state находится на отдельном PVC с собственной ёмкостью и
		// содержит provider-owned каталоги. Они намеренно могут быть недоступны
		// UID основного runner и не относятся к квоте ephemeral workspace.
		if relative == ".kodex/state" && entry.IsDir() {
			return filepath.SkipDir
		}
		canonical := policy.Root
		if relative != "." {
			canonical += "/" + filepath.ToSlash(relative)
		}
		access, reason := policy.AccessForPath(canonical)
		if reason != "" {
			return &Denial{Reason: reason}
		}
		if access == runtimecontract.RuntimeWorkspaceReadOnly && entry.IsDir() {
			return filepath.SkipDir
		}
		entries++
		if entries > policy.MaximumFileCount {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
		}
		if entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			info, err := entry.Info()
			if err != nil {
				return classify(err)
			}
			if info.Size() < 0 || info.Size() > policy.MaximumWritableBytes-bytes {
				return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
			}
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files, err
}

// withinQuota requires nonnegative usage/reservation and enough byte space plus one free file slot; callers separately bound directory-walk entries and proposed payload sizes.
// withinQuota требует неотрицательные usage/reservation, достаточное место в байтах и один свободный файловый слот; вызывающие отдельно ограничивают entries обхода и размер предполагаемых данных.
func withinQuota(currentBytes, currentFiles, additionalBytes int64, policy runtimecontract.RuntimeWorkspacePolicy) bool {
	return currentBytes >= 0 && currentFiles >= 0 && additionalBytes >= 0 &&
		currentBytes <= policy.MaximumWritableBytes-additionalBytes && currentFiles < policy.MaximumFileCount
}

// openDirectory opens the root's final component and each relative directory using O_NOFOLLOW/O_DIRECTORY, closes intermediate descriptors and rejects empty or dot components. It does not inspect ancestors preceding the supplied root; callers own that mount boundary.
// openDirectory открывает конечный компонент root и каждый относительный каталог с O_NOFOLLOW/O_DIRECTORY, закрывает промежуточные дескрипторы и отклоняет пустые и точечные компоненты. Не исследует предков перед заданным root; эта mount boundary принадлежит вызывающему.
func openDirectory(root, relative string) (int, error) {
	current, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			unix.Close(current)
			return -1, syscall.EINVAL
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

// OpenOutbox returns an owned directory handle for the no-follow outbox traversal and maps failures to workspace reasons; the caller must close the returned file.
// OpenOutbox возвращает принадлежащий вызывающему directory handle outbox после no-follow обхода и переводит ошибки в причины workspace; вызывающий должен закрыть файл.
func OpenOutbox(root string) (*os.File, error) {
	descriptor, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return nil, classify(err)
	}
	file := os.NewFile(uintptr(descriptor), "runtime-outbox")
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return file, nil
}

// classify maps permission/read-only, quota and traversal-related errno families to fixed workspace denial reasons and collapses all remaining errors to generic I/O without retaining paths.
// classify переводит семейства errno доступа/read-only, квоты и обхода в фиксированные причины workspace, заменяя остальные ошибки общей I/O без сохранения путей.
func classify(err error) error {
	switch {
	case errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EINVAL):
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	default:
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
}

// stringHex encodes nonce bytes as two lowercase hexadecimal characters each using a fixed alphabet, without exposing or interpreting filesystem paths.
// stringHex кодирует каждый байт nonce двумя строчными hex-символами фиксированного алфавита без раскрытия или интерпретации файловых путей.
func stringHex(value []byte) string {
	// digits fixes the lowercase alphabet used for nonce directory names.
	// digits закрепляет строчный алфавит для имён nonce-каталогов.
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2], result[index*2+1] = digits[item>>4], digits[item&15]
	}
	return string(result)
}
