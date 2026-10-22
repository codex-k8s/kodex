package callback

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// maximumConcurrentArtifactTransfers caps simultaneously acquired transfer files
// at two per spool; the readiness canary deliberately does not consume these slots.
// maximumConcurrentArtifactTransfers ограничивает одновременно acquired transfer files
// двумя на spool; readiness canary намеренно не занимает эти slots.
const maximumConcurrentArtifactTransfers = 2

var (
	// errArtifactSpool reports fixed safe spool setup/readiness/I/O failures without
	// revealing local paths, file names or partial artifact content.
	// errArtifactSpool сообщает fixed safe spool setup/readiness/I/O failures без local
	// paths,file names и partial artifact content.
	errArtifactSpool = errors.New("runtime artifact spool is unavailable")
	// errArtifactCapacity distinguishes immediate transfer-slot exhaustion internally;
	// the HTTP transfer boundary normalizes acquisition failures to spool-unavailable.
	// errArtifactCapacity выделяет immediate transfer-slot exhaustion внутри пакета;
	// HTTP transfer boundary нормализует acquisition failures в spool-unavailable.
	errArtifactCapacity = errors.New("runtime artifact transfer capacity exhausted")
)

// PrepareArtifactSpool prepares a controller child under an absolute clean volume
// directory using rooted handles and inode checks. The child must already be safe
// or be created as current-UID 0700; unsafe existing content is neither repaired nor
// deleted. Non-root init and isolated subPath mounting are deployment obligations.
// PrepareArtifactSpool подготавливает controller child в absolute clean volume directory
// через rooted handles/inode checks. Child должен быть безопасным либо создаваться
// current-UID 0700; unsafe existing content не исправляется и не удаляется.
// Non-root init и isolated subPath mount обеспечиваются deployment.
func PrepareArtifactSpool(directory string) error {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errArtifactSpool
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errArtifactSpool
	}
	base, err := os.OpenRoot(directory)
	if err != nil {
		return errArtifactSpool
	}
	defer base.Close()
	actual, err := base.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return errArtifactSpool
	}
	if err := base.Mkdir("controller", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return errArtifactSpool
	}
	info, err = base.Lstat("controller")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedSpoolFile(info) {
		return errArtifactSpool
	}
	child, err := base.OpenRoot("controller")
	if err != nil {
		return errArtifactSpool
	}
	defer child.Close()
	actual, err = child.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return errArtifactSpool
	}
	return nil
}

// artifactSpool owns a rooted private directory, serialized create/close access
// and a bounded acquisition semaphore. Successful files are immediately unlinked;
// in-flight descriptors belong to their release callbacks, not directory close.
// artifactSpool владеет rooted private directory,serialized create/close access
// и bounded acquisition semaphore. Успешные files сразу unlinked; in-flight
// descriptors принадлежат release callbacks, а не directory close.
type artifactSpool struct {
	mu    sync.Mutex
	root  *os.Root
	slots chan struct{}
}

// openArtifactSpool rejects noncanonical/symlink/world-writable mount roots, creates
// or verifies a current-UID 0700 private child and checks inode identity across rooted
// opens. It returns a two-slot spool; it does not fix unsafe permissions or install
// mount isolation, and existing group-writable mount roots are not rejected here.
// openArtifactSpool отвергает noncanonical/symlink/world-writable mount roots,создаёт
// либо проверяет current-UID 0700 private child и inode identity rooted opens.
// Возвращает two-slot spool,не исправляя permissions и не устанавливая mount isolation;
// existing group-writable mount roots здесь не отвергаются.
func openArtifactSpool(directory string) (*artifactSpool, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errArtifactSpool
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0002 != 0 {
		return nil, errArtifactSpool
	}
	base, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errArtifactSpool
	}
	defer base.Close()
	actual, err := base.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return nil, errArtifactSpool
	}
	if err := base.Mkdir("private", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, errArtifactSpool
	}
	info, err = base.Lstat("private")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedSpoolFile(info) {
		return nil, errArtifactSpool
	}
	root, err := base.OpenRoot("private")
	if err != nil {
		return nil, errArtifactSpool
	}
	actual, err = root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		_ = root.Close()
		return nil, errArtifactSpool
	}
	return &artifactSpool{root: root, slots: make(chan struct{}, maximumConcurrentArtifactTransfers)}, nil
}

// ownedSpoolFile requires Unix stat metadata with owner UID equal to the current
// effective UID; it does not compare group ownership or inspect parent directories.
// ownedSpoolFile требует Unix stat metadata с owner UID равным current effective UID,
// не сравнивая group ownership и не проверяя parent directories.
func ownedSpoolFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// create serializes rooted exclusive 0600 file creation, immediately unlinks the
// random name and verifies regular/current-UID/zero-link metadata before returning
// the descriptor. Failure closes it without returning bytes; an unsuccessful unlink
// is rejected, not claimed to have removed the temporary pathname.
// create сериализует rooted exclusive 0600 file creation,сразу unlink random name
// и проверяет regular/current-UID/zero-link metadata до возврата descriptor.
// Failure закрывает его без bytes; unsuccessful unlink отвергается,
// а не объявляется успешным удалением temporary pathname.
func (spool *artifactSpool) create() (*os.File, error) {
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.root == nil {
		return nil, errArtifactSpool
	}
	name := "transfer-" + rand.Text()
	file, err := spool.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errArtifactSpool
	}
	if err := spool.root.Remove(name); err != nil {
		_ = file.Close()
		return nil, errArtifactSpool
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ownedSpoolFile(info) {
		_ = file.Close()
		return nil, errArtifactSpool
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 0 {
		_ = file.Close()
		return nil, errArtifactSpool
	}
	return file, nil
}

// acquire checks cancellation before taking a nonblocking bounded slot, creates
// a private descriptor and returns an idempotent close/release callback. Creation
// failure returns capacity immediately; later cancellation is handled by the transfer
// caller, not by a background goroutine or an automatic descriptor close.
// acquire проверяет cancellation до nonblocking bounded slot,создаёт private descriptor
// и возвращает idempotent close/release callback. Creation failure сразу возвращает
// capacity; последующую cancellation обрабатывает transfer caller,не background
// goroutine и не automatic descriptor close.
func (spool *artifactSpool) acquire(ctx context.Context) (*os.File, func(), error) {
	if spool == nil {
		return nil, nil, errArtifactSpool
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	select {
	case spool.slots <- struct{}{}:
	default:
		return nil, nil, errArtifactCapacity
	}
	file, err := spool.create()
	if err != nil {
		<-spool.slots
		return nil, nil, err
	}
	var once sync.Once
	return file, func() { once.Do(func() { _ = file.Close(); <-spool.slots }) }, nil
}

// check writes and rereads a fixed two-byte canary in an unlinked private file,
// checking context before/after I/O and bypassing transfer-slot occupancy. It closes
// the descriptor on return and exposes fixed readiness errors, not artifact content.
// check записывает/читает fixed two-byte canary в unlinked private file,проверяя
// context до/после I/O независимо от transfer-slot occupancy. Descriptor закрывается
// при возврате; выдаются fixed readiness errors, не artifact content.
func (spool *artifactSpool) check(ctx context.Context) error {
	if spool == nil || ctx.Err() != nil {
		return errArtifactSpool
	}
	file, err := spool.create()
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write([]byte{0x6b, 0x78}); err != nil {
		return errArtifactSpool
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return errArtifactSpool
	}
	var actual [2]byte
	if _, err := io.ReadFull(file, actual[:]); err != nil || actual != [2]byte{0x6b, 0x78} || ctx.Err() != nil {
		return errArtifactSpool
	}
	return nil
}

// close idempotently closes and clears the directory handle under its mutex;
// it neither waits for nor invalidates already-open transfer files, whose release
// callbacks still own descriptor cleanup and slot return.
// close идемпотентно закрывает/очищает directory handle под mutex,не ожидая
// и не инвалидируя уже открытые transfer files; их release callbacks по-прежнему
// владеют descriptor cleanup и slot return.
func (spool *artifactSpool) close() error {
	if spool == nil {
		return nil
	}
	spool.mu.Lock()
	defer spool.mu.Unlock()
	if spool.root == nil {
		return nil
	}
	err := spool.root.Close()
	spool.root = nil
	if err != nil {
		return errArtifactSpool
	}
	return nil
}
