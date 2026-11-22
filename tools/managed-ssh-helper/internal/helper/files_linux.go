package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// readFile resolves one selected alias and returns only bounded native file metadata or content under the helper-wide lock.
// readFile разрешает один selected alias и возвращает только bounded native file metadata либо content под helper-wide lock.
func (runtime *Runtime) readFile(ctx context.Context, request managedssh.Request) managedssh.Reply {
	root := runtime.roots[request.Resource]
	if root == nil || !managedPath(request.Path, request.Operation == managedssh.FileList) {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	if request.Operation == managedssh.FileList {
		return runtime.list(ctx, request, root)
	}
	file, err := openManaged(root, request.Path, runtime.profile.UID, runtime.profile.GID)
	if err != nil {
		return rejected(request, fileCode(err))
	}
	defer file.Close()
	metadata, content, err := readManaged(file)
	if err != nil || ctx.Err() != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	if request.Operation == managedssh.FileStat {
		reply.File = &metadata
		return reply
	}
	if metadata.Digest != request.Expected || request.Offset > metadata.Size {
		return rejected(request, "VERSION_MISMATCH")
	}
	end := min(metadata.Size, request.Offset+uint64(request.Limit))
	reply.Fragment = &managedssh.Fragment{File: metadata, Offset: request.Offset, Content: content[request.Offset:end], Truncated: end < metadata.Size}
	return reply
}

// list reads a bounded number of direct children, sorts the observed page and marks truncation without following special entries or symlinks.
// list читает bounded число direct children, сортирует observed page и отмечает truncation без следования special entries либо symlinks.
func (runtime *Runtime) list(ctx context.Context, request managedssh.Request, root *os.File) managedssh.Reply {
	name := request.Path
	if name == "" {
		name = "."
	}
	fd, err := beneath(int(root.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return rejected(request, fileCode(err))
	}
	directory := os.NewFile(uintptr(fd), "managed-list")
	defer directory.Close()
	if !privateDirectory(directory, runtime.profile.UID, runtime.profile.GID) {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	names, err := directory.Readdirnames(4097)
	if err != nil && err != io.EOF || ctx.Err() != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Truncated = len(names) > 4096
	names = names[:min(len(names), 4096)]
	slices.Sort(names)
	for _, name := range names {
		if !managedPath(name, false) || strings.Contains(name, "/") {
			reply.Truncated = true
			continue
		}
		if len(reply.Entries) == int(request.Limit) {
			reply.Truncated = true
			break
		}
		var stat unix.Stat_t
		if unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil {
			reply.Truncated = true
			continue
		}
		kind := "BLOCKED"
		if stat.Uid == runtime.profile.UID && stat.Gid == runtime.profile.GID {
			if stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&07777 == 0600 && stat.Nlink == 1 {
				kind = "FILE"
			} else if stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&07777 == 0700 {
				kind = "DIRECTORY"
			}
		}
		reply.Entries = append(reply.Entries, managedssh.Entry{Name: name, Kind: kind})
	}
	return reply
}

// changeFile checks scope and the current complete content version before creating a durable UNKNOWN intent; only then may one atomic filesystem effect occur.
// changeFile проверяет scope и current complete content version до durable UNKNOWN intent; только затем допускается один atomic filesystem effect.
func (runtime *Runtime) changeFile(ctx context.Context, request managedssh.Request) managedssh.Reply {
	root := runtime.roots[request.Resource]
	if root == nil || !managedPath(request.Path, false) {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	parentName, base := path.Split(request.Path)
	parentName = strings.TrimSuffix(parentName, "/")
	if parentName == "" {
		parentName = "."
	}
	fd, err := beneath(int(root.Fd()), parentName, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return rejected(request, fileCode(err))
	}
	parent := os.NewFile(uintptr(fd), "managed-parent")
	defer parent.Close()
	if !privateDirectory(parent, runtime.profile.UID, runtime.profile.GID) {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	current, currentError := openManaged(parent, base, runtime.profile.UID, runtime.profile.GID)
	if request.Operation == managedssh.FileCreate {
		if current != nil {
			current.Close()
			return rejected(request, "ALREADY_EXISTS")
		}
		if !errors.Is(currentError, unix.ENOENT) {
			return rejected(request, "SCOPE_UNAVAILABLE")
		}
	} else {
		if currentError != nil {
			return rejected(request, fileCode(currentError))
		}
		metadata, _, err := readManaged(current)
		current.Close()
		if err != nil {
			return rejected(request, "RECIPE_REJECTED")
		}
		if metadata.Digest != request.Expected {
			return rejected(request, "VERSION_MISMATCH")
		}
	}
	if ctx.Err() != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	intent, ok := runtime.begin(request)
	if !ok {
		return rejected(request, "ATTEMPT_CONFLICT")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Unknown)
	reply.Receipt = &intent
	if ctx.Err() != nil {
		return reply
	}
	if request.Operation == managedssh.FileDelete {
		err = unix.Unlinkat(fd, base, 0)
	} else {
		temporary := ".kodex-" + request.AttemptID
		err = atomicContent(parent, temporary, base, request.Content, request.Operation == managedssh.FileCreate)
	}
	if err != nil || parent.Sync() != nil {
		return reply
	}
	if request.Operation != managedssh.FileDelete {
		digest := sha256.Sum256(request.Content)
		intent.Version = hex.EncodeToString(digest[:])
	}
	intent.Status = managedssh.Applied
	if !runtime.finish(request, intent) {
		return reply
	}
	reply.Status, reply.Receipt = managedssh.Applied, &intent
	return reply
}

// atomicContent syncs a newly created private inode before one descriptor-relative rename; no caller-selected permissions or ownership changes are accepted.
// atomicContent синхронизирует новый private inode до одного descriptor-relative rename; caller-selected permissions либо ownership changes не принимаются.
func atomicContent(parent *os.File, temporary, destination string, content []byte, absent bool) error {
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrRejected
	}
	file := os.NewFile(uintptr(fd), "managed-temporary")
	length, writeError := file.Write(content)
	syncError := file.Sync()
	closeError := file.Close()
	if writeError != nil || syncError != nil || closeError != nil || length != len(content) {
		return ErrRejected
	}
	flags := uint(0)
	if absent {
		flags = unix.RENAME_NOREPLACE
	}
	return unix.Renameat2(int(parent.Fd()), temporary, int(parent.Fd()), destination, flags)
}

// openManaged opens a regular selected file without blocking on FIFOs or following symlinks/mounts; every parent component is resolved by openat2.
// openManaged открывает selected regular file без блокировки на FIFO либо следования symlinks/mounts; каждый parent component разрешает openat2.
func openManaged(root *os.File, name string, uid, gid uint32) (*os.File, error) {
	fd, err := beneath(int(root.Fd()), name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "managed-file")
	if !ownedRegular(file, uid, gid) {
		file.Close()
		return nil, ErrRejected
	}
	return file, nil
}

// readManaged returns a complete bounded content version only if inode metadata remains unchanged through the read.
// readManaged возвращает complete bounded content version только при неизменных inode metadata во время read.
func readManaged(file *os.File) (managedssh.File, []byte, error) {
	var before, after unix.Stat_t
	if unix.Fstat(int(file.Fd()), &before) != nil || before.Size < 0 || before.Size > 1<<20 {
		return managedssh.File{}, nil, ErrRejected
	}
	content, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || int64(len(content)) != before.Size || unix.Fstat(int(file.Fd()), &after) != nil {
		return managedssh.File{}, nil, ErrRejected
	}
	before.Atim = after.Atim
	if before != after {
		return managedssh.File{}, nil, ErrRejected
	}
	digest := sha256.Sum256(content)
	return managedssh.File{Size: uint64(len(content)), Digest: hex.EncodeToString(digest[:]), Modified: before.Mtim.Sec*1e9 + before.Mtim.Nsec}, content, nil
}

// privateDirectory checks the opened directory identity; operator-qualified exclusive access and the helper-wide lock enforce the separate atomic-CAS deployment prerequisite.
// privateDirectory проверяет opened directory identity; operator-qualified exclusive access и helper-wide lock обеспечивают отдельную atomic-CAS deployment prerequisite.
func privateDirectory(file *os.File, uid, gid uint32) bool {
	var stat unix.Stat_t
	return unix.Fstat(int(file.Fd()), &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&07777 == 0700 && stat.Uid == uid && stat.Gid == gid
}

// managedPath excludes helper temporary names in addition to syntactic traversal; a listed blocked entry never becomes readable through another recipe.
// managedPath исключает helper temporary names дополнительно к syntactic traversal; listed blocked entry не становится readable через другой recipe.
func managedPath(name string, rootAllowed bool) bool {
	if name == "" {
		return rootAllowed
	}
	if !managedssh.RelativePath(name) {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if strings.HasPrefix(component, ".kodex-") {
			return false
		}
	}
	return true
}

// fileCode maps local filesystem failures to a fixed safe code without exposing OS messages or the selected path.
// fileCode переводит local filesystem failures в fixed safe code без OS messages либо selected path.
func fileCode(err error) string {
	if errors.Is(err, unix.ENOENT) {
		return "NOT_FOUND"
	}
	return "SCOPE_UNAVAILABLE"
}
