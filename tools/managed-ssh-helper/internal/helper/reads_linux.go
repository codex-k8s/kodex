package helper

import (
	"context"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// system returns uname fields through a fixed syscall, excluding hostname and unrelated host configuration.
// system возвращает uname fields через fixed syscall без hostname и посторонней host configuration.
func (runtime *Runtime) system(request managedssh.Request) managedssh.Reply {
	var value unix.Utsname
	if unix.Uname(&value) != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.System = &managedssh.System{Name: display(unix.ByteSliceToString(value.Sysname[:]), 64), Release: display(unix.ByteSliceToString(value.Release[:]), 128), Architecture: display(unix.ByteSliceToString(value.Machine[:]), 64)}
	return reply
}

// processes observes only actual same-UID process names from bounded procfs files; argv and environment are never opened.
// processes наблюдает только actual same-UID process names из bounded procfs files; argv и environment никогда не открываются.
func (runtime *Runtime) processes(ctx context.Context, request managedssh.Request) managedssh.Reply {
	directory, err := os.Open("/proc")
	if err != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	defer directory.Close()
	names, err := directory.Readdirnames(32769)
	if err != nil && err != io.EOF {
		return rejected(request, "RECIPE_REJECTED")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Truncated = len(names) > 32768
	names = names[:min(len(names), 32768)]
	pids := make([]uint32, 0, len(names))
	for _, name := range names {
		pid, err := strconv.ParseUint(name, 10, 32)
		if err == nil && pid > 0 && strconv.FormatUint(pid, 10) == name {
			pids = append(pids, uint32(pid))
		}
	}
	slices.Sort(pids)
	for _, pid := range pids {
		if ctx.Err() != nil {
			return rejected(request, "RECIPE_REJECTED")
		}
		fd, err := unix.Openat(int(directory.Fd()), strconv.FormatUint(uint64(pid), 10), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		process := os.NewFile(uintptr(fd), "process")
		status, err := boundedAt(process, "status", 16384)
		if err != nil || !processUID(status, runtime.profile.UID) {
			process.Close()
			continue
		}
		name, err := boundedAt(process, "comm", 256)
		process.Close()
		if err != nil {
			continue
		}
		if len(reply.Processes) == int(request.Limit) {
			reply.Truncated = true
			break
		}
		reply.Processes = append(reply.Processes, managedssh.Process{PID: pid, Name: display(strings.TrimSuffix(string(name), "\n"), 64)})
	}
	return reply
}

// processUID accepts only four exact same-UID credentials, excluding transitions, root processes and mixed real/effective identities.
// processUID допускает только четыре exact same-UID credentials, исключая transitions, root processes и mixed real/effective identities.
func processUID(status []byte, uid uint32) bool {
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 5 {
			return false
		}
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 32)
			if err != nil || uint32(value) != uid {
				return false
			}
		}
		return true
	}
	return false
}

// boundedAt reads one fixed procfs field without symlink following and excludes malformed or overflowing pseudo-file output.
// boundedAt читает одно fixed procfs field без symlink following и исключает malformed либо overflowing pseudo-file output.
func boundedAt(directory *os.File, name string, maximum int64) ([]byte, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrRejected
	}
	file := os.NewFile(uintptr(fd), "process-field")
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, ErrRejected
	}
	return raw, nil
}

// display preserves bounded untrusted text while replacing malformed UTF-8 and terminal controls; it never parses instructions or uses text as authority.
// display сохраняет bounded untrusted text, заменяя malformed UTF-8 и terminal controls; она не разбирает инструкции и не использует text как authority.
func display(value string, maximum int) string {
	var result strings.Builder
	for _, character := range strings.ToValidUTF8(value, "�") {
		if unicode.IsControl(character) {
			character = ' '
		}
		if result.Len()+len(string(character)) > maximum {
			break
		}
		result.WriteRune(character)
	}
	if result.Len() == 0 {
		return "?"
	}
	return result.String()
}
