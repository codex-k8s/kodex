package archive

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// workspaceTreeScanner walks only beneath an already owner-bound work descriptor and retains aggregate limits.
// Listing is bounded globally, including directories still waiting to be encoded; there is no full-path catalog.
// workspaceTreeScanner обходит только owner-bound work descriptor с общими прежними limits.
// Listing ограничен глобально, включая ещё не encoded directories; full-path catalog не создаётся.
type workspaceTreeScanner struct {
	rootFD             int
	ownerUID, ownerGID uint32
	providerUID        uint32
	providerOpen       func(string, unix.Stat_t) (*os.File, error)
	listed             int64
	encoder            *workspaceStreamEncoder
}

// workspaceTreeEntry stores one bounded component and type, excluding private absolute source paths.
// workspaceTreeEntry хранит один bounded component/type без private absolute source paths.
type workspaceTreeEntry struct {
	name      string
	directory bool
}

// scanWorkspaceTree encodes exact owned files without crossing descendant mounts or following links.
// The caller owns rootFD, stopped-writer proof and fixed production IDs; fixture IDs confer no authority.
// scanWorkspaceTree кодирует exact owned files без crossing descendant mounts и без following links.
// Caller владеет rootFD, stopped-writer proof и production IDs; fixture IDs не дают authority.
func scanWorkspaceTree(ctx context.Context, rootFD int, destination io.Writer, policy runtimecontract.RuntimeWorkspacePolicy, ownerUID, ownerGID uint32) (string, int64, int64, int64, error) {
	return scanWorkspaceOwnedTree(ctx, rootFD, destination, policy, ownerUID, ownerGID, 0, nil)
}

// scanWorkspaceOwnedTree accepts a separately authenticated provider reader for regular files only.
// Directories remain local descriptor walks, so inaccessible private directories fail closed.
// scanWorkspaceOwnedTree допускает отдельно аутентифицированный provider reader только для regular files.
// Каталоги обходятся локальными descriptors; недоступный private directory закрыто отклоняется.
func scanWorkspaceOwnedTree(ctx context.Context, rootFD int, destination io.Writer, policy runtimecontract.RuntimeWorkspacePolicy,
	ownerUID, ownerGID, providerUID uint32, providerOpen func(string, unix.Stat_t) (*os.File, error)) (string, int64, int64, int64, error) {
	encoder, err := newWorkspaceStreamEncoder(ctx, destination, policy)
	var before, after unix.Stat_t
	if err != nil || rootFD < 0 || unix.Fstat(rootFD, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFDIR ||
		before.Uid != ownerUID || before.Gid != ownerGID {
		return "", 0, 0, 0, errSourceIdentity
	}
	scanner := workspaceTreeScanner{rootFD: rootFD, ownerUID: ownerUID, ownerGID: ownerGID, providerUID: providerUID, providerOpen: providerOpen, encoder: encoder}
	if scanner.directory(rootFD, "") != nil || unix.Fstat(rootFD, &after) != nil || !workspaceTreeIdentityEqual(before, after) {
		return "", 0, 0, 0, errSourceIdentity
	}
	return encoder.finish()
}

// directory snapshots bounded component names and emits deterministic wire ordering before closing each child.
// Every child open retains BENEATH/NO_SYMLINKS/NO_XDEV and an immutable before/after inode check.
// directory читает bounded component names и сохраняет deterministic wire order до закрытия child.
// Каждый child open сохраняет BENEATH/NO_SYMLINKS/NO_XDEV и before/after inode check.
func (scanner *workspaceTreeScanner) directory(fd int, prefix string) error {
	listFD, err := unix.Openat2(fd, ".", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return errSourceIdentity
	}
	list := os.NewFile(uintptr(listFD), "workspace-checkpoint-directory")
	defer list.Close()
	var entries []workspaceTreeEntry
	for {
		if scanner.encoder.ctx.Err() != nil {
			return errSourceIdentity
		}
		names, readErr := list.Readdirnames(256)
		for _, name := range names {
			scanner.listed++
			if scanner.listed > scanner.encoder.maximumFiles || strings.Contains(name, "/") || !workspaceStreamPath(name) {
				return errSourceIdentity
			}
			var info unix.Stat_t
			if unix.Fstatat(fd, name, &info, unix.AT_SYMLINK_NOFOLLOW) != nil || !scanner.ownedEntry(info) {
				return errSourceIdentity
			}
			directory := info.Mode&unix.S_IFMT == unix.S_IFDIR
			if directory {
				name += "/"
			}
			entries = append(entries, workspaceTreeEntry{name: name, directory: directory})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil || len(names) == 0 {
			return errSourceIdentity
		}
	}
	slices.SortFunc(entries, func(left, right workspaceTreeEntry) int {
		return strings.Compare(left.name, right.name)
	})
	for _, entry := range entries {
		name := prefix + strings.TrimSuffix(entry.name, "/")
		if !workspaceStreamPath(name) || scanner.entry(fd, entry, name) != nil {
			return errSourceIdentity
		}
	}
	return nil
}

// ownedEntry rejects foreign owners, special permissions, hard-linked files and every unsupported file kind.
// ownedEntry отклоняет foreign owners, special permissions, hard-linked files и unsupported kinds.
func (scanner *workspaceTreeScanner) ownedEntry(info unix.Stat_t) bool {
	kind := info.Mode & unix.S_IFMT
	return (info.Uid == scanner.ownerUID || scanner.providerOpen != nil && info.Uid == scanner.providerUID) && info.Gid == scanner.ownerGID &&
		(kind == unix.S_IFDIR && info.Mode&0o5000 == 0 || kind == unix.S_IFREG && info.Nlink == 1 && info.Mode&0o7000 == 0)
}

// entry streams one descriptor and verifies its identity after all bytes or descendants have been observed.
// A read failure poisons the whole checkpoint; no file is skipped to manufacture a partial successful tree.
// entry читает один descriptor и сверяет identity после bytes или descendants.
// Read failure закрывает checkpoint целиком; пропуск файла не создаёт partial successful tree.
func (scanner *workspaceTreeScanner) entry(parentFD int, entry workspaceTreeEntry, name string) error {
	var before, after unix.Stat_t
	component := strings.TrimSuffix(entry.name, "/")
	if unix.Fstatat(parentFD, component, &before, unix.AT_SYMLINK_NOFOLLOW) != nil || !scanner.ownedEntry(before) ||
		(before.Mode&unix.S_IFMT == unix.S_IFDIR) != entry.directory {
		return errSourceIdentity
	}
	file, err := scanner.openEntry(parentFD, component, name, before)
	if err != nil || file == nil {
		if file != nil {
			_ = file.Close()
		}
		return errSourceIdentity
	}
	defer file.Close()
	fd := int(file.Fd())
	if unix.Fstat(fd, &after) != nil || !workspaceTreeIdentityEqual(before, after) {
		return errSourceIdentity
	}
	owner := workspaceStreamRoleOwner
	if scanner.providerOpen != nil && before.Uid == scanner.providerUID {
		owner = workspaceStreamProviderOwner
	}
	if entry.directory {
		if scanner.encoder.appendOwnedEntry(name, true, before.Mode&0o777, 0, nil, owner) != nil || scanner.directory(fd, name+"/") != nil {
			return errSourceIdentity
		}
	} else if scanner.encoder.appendOwnedEntry(name, false, before.Mode&0o777, before.Size, io.NewSectionReader(file, 0, before.Size), owner) != nil {
		return errSourceIdentity
	}
	if unix.Fstat(fd, &after) != nil || !workspaceTreeIdentityEqual(before, after) ||
		unix.Fstatat(parentFD, component, &after, unix.AT_SYMLINK_NOFOLLOW) != nil || !workspaceTreeIdentityEqual(before, after) {
		return errSourceIdentity
	}
	return nil
}

// workspaceTreeIdentityEqual ignores atime changes caused by reads while retaining immutable writer-relevant metadata.
// workspaceTreeIdentityEqual игнорирует atime от чтения, сохраняя writer-relevant metadata.
func workspaceTreeIdentityEqual(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Nlink == after.Nlink &&
		before.Uid == after.Uid && before.Gid == after.Gid && before.Size == after.Size && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

// openEntry preserves the descriptor boundary even when the paired reader supplies private provider bytes.
// No directory, role-owned file or unsupported entry is delegated to the provider reader.
// openEntry сохраняет descriptor boundary при чтении private provider bytes через paired reader.
// Каталоги, role-owned файлы и неподдержанные entries не передаются provider reader.
func (scanner *workspaceTreeScanner) openEntry(parentFD int, component, name string, expected unix.Stat_t) (*os.File, error) {
	if expected.Mode&unix.S_IFMT == unix.S_IFREG && scanner.providerOpen != nil && expected.Uid == scanner.providerUID {
		return scanner.providerOpen(name, expected)
	}
	flags := uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK)
	if expected.Mode&unix.S_IFMT == unix.S_IFDIR {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat2(parentFD, component, &unix.OpenHow{Flags: flags,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, errSourceIdentity
	}
	return os.NewFile(uintptr(fd), "workspace-checkpoint-entry"), nil
}
