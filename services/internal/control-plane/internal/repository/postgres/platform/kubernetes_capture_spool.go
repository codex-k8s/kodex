package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"golang.org/x/sys/unix"
)

var (
	// errKubernetesCaptureSpool hides private filesystem coordinates and partial captured bytes.
	// errKubernetesCaptureSpool скрывает private filesystem coordinates и partial captured bytes.
	errKubernetesCaptureSpool = errors.New("Kubernetes capture spool is unavailable")
)

// kubernetesCaptureSpoolDirectory owns one configured private directory and its physical-byte
// budget. Anonymous files are transient assembly copies, never durable ACK or adoption evidence.
// kubernetesCaptureSpoolDirectory владеет configured private directory и physical-byte budget.
// Anonymous files — transient assembly copies, не durable ACK либо adoption evidence.
type kubernetesCaptureSpoolDirectory struct {
	mu     sync.Mutex
	fd     int
	bytes  int64
	files  int
	closed bool
}

// kubernetesCaptureSpool keeps one anonymous file within the directory's shared current-byte
// quota. It never accepts a caller path, retains a lifetime counter or authorizes publication.
// kubernetesCaptureSpool хранит anonymous file в общей current-byte quota directory.
// Caller path, lifetime counter либо publication authority не принимает.
type kubernetesCaptureSpool struct {
	directory *kubernetesCaptureSpoolDirectory
	file      *os.File
	size      int64
	closed    bool
	ctx       context.Context
}

// kubernetesCaptureSpoolReader exposes the existing artifact inspection/AV/PUT reader contract
// without transferring Close or Write authority to the SDK's request body wrapper.
// kubernetesCaptureSpoolReader выдаёт existing artifact inspection/AV/PUT reader contract,
// не передавая Close либо Write authority request body wrapper внутри SDK.
type kubernetesCaptureSpoolReader struct{ spool *kubernetesCaptureSpool }

// available checks the still-open owned0700 descriptor without consuming an assembly slot.
// Existing busy files do not turn compatibility readiness into a false failure.
// available проверяет открытый owned0700 descriptor, не занимая assembly slot.
// Existing busy files не превращают compatibility readiness в ложный отказ.
func (directory *kubernetesCaptureSpoolDirectory) available() bool {
	if directory == nil {
		return false
	}
	directory.mu.Lock()
	defer directory.mu.Unlock()
	return !directory.closed && validKubernetesCaptureSpoolDirectory(directory.fd)
}

// validKubernetesCaptureSpoolDirectory verifies the exact held descriptor's owner/type/mode.
// It never repairs permissions or derives domain authority from filesystem ownership.
// validKubernetesCaptureSpoolDirectory проверяет owner/type/mode exact held descriptor.
// Permissions не исправляет; domain authority из filesystem ownership не выводит.
func validKubernetesCaptureSpoolDirectory(fd int) bool {
	var info unix.Stat_t
	return fd >= 0 && unix.Fstat(fd, &info) == nil && info.Mode&unix.S_IFMT == unix.S_IFDIR && info.Mode&0o7777 == 0o700 && info.Uid == uint32(os.Geteuid())
}

// ConfigureKubernetesCaptureSpool opens the deployment-selected mount without symlinks and
// creates only its private owned0700 child. The caller closes the returned directory after all
// request/worker joins; configuring this path alone proves no schema, storage or capture readiness.
// ConfigureKubernetesCaptureSpool открывает выбранный deployment mount без symlinks и создаёт
// лишь private owned0700 child. Caller закрывает directory после request/worker joins;
// одна настройка пути не доказывает schema, storage либо capture readiness.
func (repository *Repository) ConfigureKubernetesCaptureSpool(ctx context.Context, root string) (io.Closer, error) {
	if os.Geteuid() <= 0 || repository == nil || repository.kubernetesCaptureSpool != nil || ctx == nil || ctx.Err() != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errs.ErrInvalid
	}
	parent, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, errKubernetesCaptureSpool
	}
	defer unix.Close(parent)
	owner := securefile.OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	fd, _, err := securefile.OpenOwnedDirectoryAt(ctx, parent, "private", 0o700, owner, true)
	if err != nil {
		return nil, errKubernetesCaptureSpool
	}
	directory := &kubernetesCaptureSpoolDirectory{fd: fd}
	// Probe anonymous-file support before handlers start; this empty file is not artifact evidence.
	// Проверяем anonymous-file support до запуска handlers; empty file не является artifact evidence.
	probe, err := directory.open(ctx, "STDOUT")
	if err != nil {
		_ = directory.Close()
		return nil, errKubernetesCaptureSpool
	}
	if err := probe.Close(); err != nil {
		_ = directory.Close()
		return nil, errKubernetesCaptureSpool
	}
	repository.kubernetesCaptureSpool = directory
	return directory, nil
}

// Close refuses to claim successful teardown while an assembly still owns a live file.
// No path repair, unlink-by-name or removal of unrelated mount contents is performed.
// Close не утверждает successful teardown при live file у assembly.
// Path repair, unlink-by-name либо удаление unrelated mount contents не выполняется.
func (directory *kubernetesCaptureSpoolDirectory) Close() error {
	directory.mu.Lock()
	defer directory.mu.Unlock()
	if directory.closed {
		return nil
	}
	if directory.files != 0 || directory.bytes != 0 {
		return errKubernetesCaptureSpool
	}
	if err := unix.Close(directory.fd); err != nil {
		return errKubernetesCaptureSpool
	}
	directory.closed = true
	return nil
}

// open creates one unnamed owned0600 regular file for a known output channel. Five channels
// and the existing original stream-byte ceiling bound simultaneous storage, not session history.
// open создаёт unnamed owned0600 regular file для known output channel. Пять channels и existing
// original stream-byte ceiling ограничивают simultaneous storage, не session history.
func (directory *kubernetesCaptureSpoolDirectory) open(ctx context.Context, channel string) (*kubernetesCaptureSpool, error) {
	if directory == nil || ctx == nil || ctx.Err() != nil || !captureOutputChannel(channel) {
		return nil, errKubernetesCaptureSpool
	}
	directory.mu.Lock()
	defer directory.mu.Unlock()
	if directory.closed || directory.files >= 5 || !validKubernetesCaptureSpoolDirectory(directory.fd) {
		return nil, errKubernetesCaptureSpool
	}
	fd, err := unix.Openat(directory.fd, ".", unix.O_RDWR|unix.O_TMPFILE|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, errKubernetesCaptureSpool
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o7777 != 0o600 || info.Uid != uint32(os.Geteuid()) || info.Nlink != 0 || info.Size != 0 {
		_ = unix.Close(fd)
		return nil, errKubernetesCaptureSpool
	}
	directory.files++
	return &kubernetesCaptureSpool{directory: directory, file: os.NewFile(uintptr(fd), "private-kubernetes-capture"), ctx: ctx}, nil
}

// Write appends one bounded piece without exceeding the original global stream-byte ceiling.
// Read/seek consumers cannot use this method to overwrite a scanned prefix or create sparse holes.
// Write добавляет bounded piece в пределах existing global stream-byte ceiling.
// Read/seek consumers не могут перезаписать scanned prefix либо создать sparse holes этим методом.
func (spool *kubernetesCaptureSpool) Write(body []byte) (int, error) {
	directory := spool.directory
	directory.mu.Lock()
	defer directory.mu.Unlock()
	if directory.closed || spool.closed || spool.ctx.Err() != nil || int64(len(body)) > int64(managedkubernetes.MaximumStreamBytes)-directory.bytes {
		return 0, errKubernetesCaptureSpool
	}
	position, err := spool.file.Seek(0, io.SeekCurrent)
	if err != nil || position != spool.size {
		return 0, errKubernetesCaptureSpool
	}
	count, err := spool.file.Write(body)
	spool.size += int64(count)
	directory.bytes += int64(count)
	if err != nil {
		return count, errKubernetesCaptureSpool
	}
	return count, nil
}

// reset discards only this owned transient copy before replacing it with validated copy output.
// Durable private stages and canonical revisions remain unchanged and recoverable on failure.
// reset удаляет лишь owned transient copy до замены проверенным copy output.
// Durable private stages и canonical revisions неизменны и доступны recovery при отказе.
func (spool *kubernetesCaptureSpool) reset() error {
	directory := spool.directory
	directory.mu.Lock()
	defer directory.mu.Unlock()
	if directory.closed || spool.closed || spool.ctx.Err() != nil || spool.file.Truncate(0) != nil {
		return errKubernetesCaptureSpool
	}
	directory.bytes -= spool.size
	spool.size = 0
	if _, err := spool.file.Seek(0, io.SeekStart); err != nil {
		return errKubernetesCaptureSpool
	}
	return nil
}

// Close closes the exact anonymous descriptor and releases current physical storage accounting.
// It does not claim durable deletion of stage or artifact objects and is safe to call twice.
// Close закрывает exact anonymous descriptor и освобождает current physical storage accounting.
// Durable deletion stage/artifact objects не утверждает; повторный вызов безопасен.
func (spool *kubernetesCaptureSpool) Close() error {
	directory := spool.directory
	directory.mu.Lock()
	defer directory.mu.Unlock()
	if spool.closed {
		return nil
	}
	if err := spool.file.Close(); err != nil {
		return errKubernetesCaptureSpool
	}
	directory.bytes -= spool.size
	directory.files--
	spool.closed = true
	return nil
}

// Read keeps each synchronous file read inside the owner's cancellation and descriptor lifetime.
// Read удерживает synchronous file read в пределах owner cancellation и descriptor lifetime.
func (reader *kubernetesCaptureSpoolReader) Read(body []byte) (int, error) {
	spool := reader.spool
	spool.directory.mu.Lock()
	defer spool.directory.mu.Unlock()
	if spool.closed || spool.ctx.Err() != nil {
		return 0, errKubernetesCaptureSpool
	}
	return spool.file.Read(body)
}

// ReadAt permits bounded artifact header inspection without accepting an external file path.
// ReadAt допускает bounded artifact header inspection без external file path.
func (reader *kubernetesCaptureSpoolReader) ReadAt(body []byte, offset int64) (int, error) {
	spool := reader.spool
	spool.directory.mu.Lock()
	defer spool.directory.mu.Unlock()
	if spool.closed || spool.ctx.Err() != nil {
		return 0, errKubernetesCaptureSpool
	}
	return spool.file.ReadAt(body, offset)
}

// Seek rewinds only the already owned spool for inspection, scanning and the one immutable PUT.
// Seek перемещает позицию только owned spool для inspection, scan и одного immutable PUT.
func (reader *kubernetesCaptureSpoolReader) Seek(offset int64, whence int) (int64, error) {
	spool := reader.spool
	spool.directory.mu.Lock()
	defer spool.directory.mu.Unlock()
	if spool.closed || spool.ctx.Err() != nil {
		return 0, errKubernetesCaptureSpool
	}
	return spool.file.Seek(offset, whence)
}

// Size returns the completed local byte count; it conveys no provider durability or owner grant.
// Size возвращает completed local byte count, не provider durability либо owner grant.
func (reader *kubernetesCaptureSpoolReader) Size() int64 {
	reader.spool.directory.mu.Lock()
	defer reader.spool.directory.mu.Unlock()
	return reader.spool.size
}
