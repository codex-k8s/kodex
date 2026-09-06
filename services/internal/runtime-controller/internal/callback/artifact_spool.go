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

const maximumConcurrentArtifactTransfers = 2

var (
	errArtifactSpool    = errors.New("runtime artifact spool is unavailable")
	errArtifactCapacity = errors.New("runtime artifact transfer capacity exhausted")
)

// Отдельный mount принадлежит controller. Внутри него только приватный каталог
// текущего UID; открытый временный файл сразу unlink и исчезает при close/crash.
type artifactSpool struct {
	mu    sync.Mutex
	root  *os.Root
	slots chan struct{}
}

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

func ownedSpoolFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

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
