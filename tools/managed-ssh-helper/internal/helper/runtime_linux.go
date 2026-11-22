package helper

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// Runtime owns open namespace descriptors for one SSH request and starts no background workers or retries.
// Runtime владеет open namespace descriptors для одного SSH request и не запускает background workers либо retries.
type Runtime struct {
	profile managedssh.Profile
	digest  string
	state   *os.File
	roots   map[string]*os.File
}

// openRuntime opens the finite selected roots after configuration provenance is checked by OpenInstalled; tests exercise the same descriptor boundary with synthetic owned directories.
// openRuntime открывает конечный набор выбранных roots после проверки provenance в OpenInstalled; tests проверяют ту же descriptor boundary на synthetic owned directories.
func openRuntime(ctx context.Context, profile managedssh.Profile) (*Runtime, error) {
	digest, err := profile.Digest()
	if err != nil || ctx.Err() != nil || profile.ValidUntil <= time.Now().Unix() {
		return nil, ErrRejected
	}
	runtime := &Runtime{profile: profile, digest: digest, roots: make(map[string]*os.File)}
	runtime.state, err = ownedDirectory(profile.StateDirectory, profile.UID, profile.GID)
	if err != nil {
		return nil, ErrRejected
	}
	for _, root := range profile.Roots {
		file, err := ownedDirectory(root.Path, profile.UID, profile.GID)
		if err != nil {
			runtime.Close()
			return nil, ErrRejected
		}
		runtime.roots[root.Alias] = file
	}
	return runtime, nil
}

// Close releases only local descriptors and does not claim that a previously dispatched service job or remote descendant joined.
// Close освобождает только local descriptors и не утверждает завершение ранее отправленного service job либо remote descendant.
func (runtime *Runtime) Close() {
	if runtime.state != nil {
		runtime.state.Close()
	}
	for _, file := range runtime.roots {
		file.Close()
	}
}

// Serve handles exactly one bounded canonical frame and writes one typed reply; malformed transport data is never echoed.
// Serve обрабатывает ровно один bounded canonical frame и пишет один typed reply; malformed transport data никогда не отражается в ответе.
func (runtime *Runtime) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(input, managedssh.MaximumFrame+1))
	if err != nil {
		return ErrRejected
	}
	request, err := managedssh.DecodeRequest(raw)
	if err != nil {
		return ErrRejected
	}
	reply, err := runtime.Execute(ctx, request)
	if err != nil {
		return err
	}
	encoded, err := managedssh.EncodeBoundedReply(request, reply)
	if err != nil {
		return ErrRejected
	}
	if _, err := output.Write(encoded); err != nil {
		return ErrRejected
	}
	return nil
}

// Execute validates current pins before acquiring one helper-wide lock and dispatches only built-in recipes; recovery never resends a managed change.
// Execute до единого helper-wide lock проверяет current pins и запускает только built-in recipes; recovery не отправляет managed change повторно.
func (runtime *Runtime) Execute(ctx context.Context, request managedssh.Request) (managedssh.Reply, error) {
	if request.Validate() != nil || ctx.Err() != nil {
		return managedssh.Reply{}, ErrRejected
	}
	if request.ProfileDigest != runtime.digest {
		return rejected(request, "PROFILE_MISMATCH"), nil
	}
	if runtime.profile.ValidUntil <= time.Now().Unix() {
		return rejected(request, "PROFILE_EXPIRED"), nil
	}
	fd, err := unix.Openat(int(runtime.state.Fd()), "helper.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return managedssh.Reply{}, ErrRejected
	}
	lock := os.NewFile(uintptr(fd), "helper-lock")
	defer lock.Close()
	if !ownedRegular(lock, runtime.profile.UID, runtime.profile.GID) || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return rejected(request, "RECIPE_REJECTED"), nil
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if ctx.Err() != nil || runtime.profile.ValidUntil <= time.Now().Unix() {
		return rejected(request, "PROFILE_EXPIRED"), nil
	}
	switch request.Operation {
	case managedssh.Inspect:
		reply, _ := managedssh.NewReply(request, managedssh.Observed)
		reply.Identity = &managedssh.Identity{UID: runtime.profile.UID, GID: runtime.profile.GID, Groups: runtime.profile.Groups, Executable: runtime.profile.ExecutableDigest}
		return reply, nil
	case managedssh.SystemRead:
		return runtime.system(request), nil
	case managedssh.ProcessList:
		return runtime.processes(ctx, request), nil
	case managedssh.FileList, managedssh.FileStat, managedssh.FileRead:
		return runtime.readFile(ctx, request), nil
	case managedssh.FileCreate, managedssh.FileReplace, managedssh.FileDelete:
		return runtime.changeFile(ctx, request), nil
	case managedssh.ReceiptRead:
		return runtime.receipt(request), nil
	case managedssh.ServiceRead, managedssh.ServiceRestart, managedssh.ServiceReload, managedssh.JournalRead:
		return runtime.service(ctx, request), nil
	default:
		return managedssh.Reply{}, ErrRejected
	}
}

// rejected emits only a fixed failure code and no remote path, content, private key or operating-system diagnostic.
// rejected возвращает только fixed failure code без remote path, content, private key либо operating-system diagnostic.
func rejected(request managedssh.Request, code string) managedssh.Reply {
	reply, _ := managedssh.NewReply(request, managedssh.Rejected)
	reply.Code = code
	return reply
}

// ownedDirectory admits a private selected directory and requires kernel openat2 support before any request can reach the filesystem recipes.
// ownedDirectory допускает private selected directory и требует kernel openat2 support до доступа request к filesystem recipes.
func ownedDirectory(name string, uid, gid uint32) (*os.File, error) {
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRejected
	}
	file := os.NewFile(uintptr(fd), "managed-directory")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != uid || stat.Gid != gid || stat.Mode&07777 != 0700 {
		file.Close()
		return nil, ErrRejected
	}
	probe, err := beneath(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		file.Close()
		return nil, ErrRejected
	}
	unix.Close(probe)
	return file, nil
}

// beneath uses kernel-enforced traversal without symlinks, magic links or mount crossing; unsupported kernels have no pathname fallback.
// beneath использует kernel-enforced traversal без symlinks, magic links либо mount crossing; у unsupported kernels нет pathname fallback.
func beneath(directory int, name string, flags int) (int, error) {
	return unix.Openat2(directory, name, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
}

// ownedRegular excludes hard links, devices, FIFOs and writable peer identities before reading or changing a managed file or receipt.
// ownedRegular исключает hard links, devices, FIFOs и writable peer identities до чтения либо изменения managed file или receipt.
func ownedRegular(file *os.File, uid, gid uint32) bool {
	var stat unix.Stat_t
	return unix.Fstat(int(file.Fd()), &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&07777 == 0600 && stat.Uid == uid && stat.Gid == gid && stat.Nlink == 1
}
