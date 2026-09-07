package runtimeorphan

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type Store struct {
	Path string
	lock *os.File
}

func private(info os.FileInfo, mode os.FileMode) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && (info.IsDir() || stat.Nlink == 1) && info.Mode().Perm() == mode && info.Mode()&os.ModeSymlink == 0
}
func OpenStore(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrGuard
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || resolved != filepath.Clean(filepath.Dir(path)) {
		return nil, ErrGuard
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || !private(dir, 0700) {
		return nil, ErrGuard
	}
	fd, err := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, ErrGuard
	}
	f := os.NewFile(uintptr(fd), path+".lock")
	info, err := f.Stat()
	if err != nil || !private(info, 0600) || syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, ErrGuard
	}
	return &Store{Path: path, lock: f}, nil
}
func (s *Store) Close() { _ = s.lock.Close() }
func (s *Store) Read() (Plan, error) {
	fd, err := syscall.Open(s.Path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Plan{}, ErrGuard
	}
	f := os.NewFile(uintptr(fd), s.Path)
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || !private(i, 0600) || i.Size() > 64<<10 {
		return Plan{}, ErrGuard
	}
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	decoder.DisallowUnknownFields()
	var p Plan
	if decoder.Decode(&p) != nil {
		return Plan{}, ErrGuard
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Plan{}, ErrGuard
	}
	return p, nil
}
func (s *Store) Save(p Plan, initial bool) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return ErrGuard
	}
	if initial {
		fd, err := syscall.Open(s.Path, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return ErrGuard
		}
		f := os.NewFile(uintptr(fd), s.Path)
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return ErrGuard
		}
	} else {
		i, err := os.Lstat(s.Path)
		if err != nil || !i.Mode().IsRegular() || !private(i, 0600) {
			return ErrGuard
		}
		f, err := os.CreateTemp(filepath.Dir(s.Path), ".orphan-receipt-")
		if err != nil {
			return ErrGuard
		}
		defer os.Remove(f.Name())
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil || closeErr != nil || os.Rename(f.Name(), s.Path) != nil {
			return ErrGuard
		}
	}
	d, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return ErrGuard
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrGuard
	}
	return nil
}
