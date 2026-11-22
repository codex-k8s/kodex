package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// maximumToolOutput bounds fixed systemd command output before parsing; tool execution separately has an inherited five-second deadline.
// maximumToolOutput ограничивает fixed systemd command output до parsing; tool execution отдельно имеет inherited five-second deadline.
const maximumToolOutput = 49152

// service resolves one pinned system unit under a distinct non-root identity; changing its state uses a delegated privilege and always requires EACH consent at the owner.
// service разрешает pinned system unit под отдельной непривилегированной identity; изменение состояния использует делегированную привилегию и всегда требует EACH consent у owner.
func (runtime *Runtime) service(ctx context.Context, request managedssh.Request) managedssh.Reply {
	var selected *managedssh.ServiceScope
	for index := range runtime.profile.Services {
		if runtime.profile.Services[index].Alias == request.Resource {
			selected = &runtime.profile.Services[index]
		}
	}
	if selected == nil {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	state, ok := runtime.serviceState(ctx, *selected)
	if !ok {
		return rejected(request, "SCOPE_UNAVAILABLE")
	}
	if request.Operation == managedssh.ServiceRead {
		reply, _ := managedssh.NewReply(request, managedssh.Observed)
		reply.Service = &managedssh.Service{State: state}
		return reply
	}
	if request.Operation == managedssh.JournalRead {
		return runtime.journal(ctx, request, *selected)
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
	verb := "restart"
	if request.Operation == managedssh.ServiceReload {
		verb = "reload"
	}
	// A timeout after systemctl dispatch cannot prove that its systemd job did not run or that it joined.
	// Timeout после systemctl dispatch не доказывает, что systemd job не выполнялся либо завершился.
	if _, err := runtime.tool(ctx, "/usr/bin/systemctl", runtime.profile.SystemctlDigest, "--system", "--no-pager", "--no-ask-password", verb, selected.Unit); err != nil {
		return reply
	}
	intent.Status = managedssh.Applied
	if !runtime.finish(request, intent) {
		return reply
	}
	reply.Status, reply.Receipt = managedssh.Applied, &intent
	return reply
}

// serviceState rejects same-identity services, capability-bearing units, changed operator authorization, transient definitions and reload drift before admitting fixed service operations.
// serviceState отклоняет сервисы той же identity, units с capabilities, изменённую operator authorization, transient definitions и reload drift до фиксированных service operations.
func (runtime *Runtime) serviceState(ctx context.Context, selected managedssh.ServiceScope) (string, bool) {
	if !runtime.controlPolicy() {
		return "", false
	}
	parent, err := operatorDirectory("/etc/systemd/system")
	if err != nil {
		return "", false
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), selected.Unit, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", false
	}
	file := os.NewFile(uintptr(fd), "selected-unit")
	defer file.Close()
	if !operatorFile(file, false) || fileDigest(file, 65536) != selected.UnitDigest {
		return "", false
	}
	raw, err := runtime.tool(ctx, "/usr/bin/systemctl", runtime.profile.SystemctlDigest, "--system", "--no-pager", "--no-ask-password", "show", "--property=ActiveState,FragmentPath,DropInPaths,LoadState,NeedDaemonReload,Transient,User,DynamicUser,NoNewPrivileges,CapabilityBoundingSet,AmbientCapabilities", selected.Unit)
	if err != nil {
		return "", false
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", false
		}
		if _, exists := fields[name]; exists {
			return "", false
		}
		fields[name] = value
	}
	if len(fields) != 11 || fields["FragmentPath"] != "/etc/systemd/system/"+selected.Unit || fields["DropInPaths"] != "" || fields["LoadState"] != "loaded" || fields["NeedDaemonReload"] != "no" || fields["Transient"] != "no" || fields["User"] != strconv.FormatUint(uint64(selected.UID), 10) || selected.UID == 0 || selected.UID == runtime.profile.UID || fields["DynamicUser"] != "no" || fields["NoNewPrivileges"] != "yes" || fields["CapabilityBoundingSet"] != "" || fields["AmbientCapabilities"] != "" {
		return "", false
	}
	for name := range fields {
		switch name {
		case "ActiveState", "FragmentPath", "DropInPaths", "LoadState", "NeedDaemonReload", "Transient", "User", "DynamicUser", "NoNewPrivileges", "CapabilityBoundingSet", "AmbientCapabilities":
		default:
			return "", false
		}
	}
	switch fields["ActiveState"] {
	case "active", "inactive", "failed", "activating", "deactivating", "reloading":
		return strings.ToUpper(fields["ActiveState"]), true
	default:
		return "UNKNOWN", true
	}
}

// controlPolicy verifies the exact operator-installed rule file before consulting systemd; a connection checkbox or editable profile cannot supply this authorization.
// controlPolicy проверяет exact operator-installed rule file до обращения к systemd; connection checkbox либо редактируемый profile не заменяют authorization.
func (runtime *Runtime) controlPolicy() bool {
	parent, err := operatorDirectory("/etc/polkit-1/rules.d")
	if err != nil {
		return false
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), "10-kodex-ssh-target.rules", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(fd), "control-policy")
	defer file.Close()
	return operatorFile(file, false) && fileDigest(file, 65536) == runtime.profile.ControlPolicyDigest
}

// journal reads a bounded twenty-four-hour tail matching the exact selected system unit; journalctl supplies JSON escaping and caller data never selects fields or expressions.
// journal читает bounded twenty-four-hour tail exact выбранного system unit; journalctl обеспечивает JSON escaping, caller data не выбирают fields либо expressions.
func (runtime *Runtime) journal(ctx context.Context, request managedssh.Request, selected managedssh.ServiceScope) managedssh.Reply {
	raw, err := runtime.tool(ctx, "/usr/bin/journalctl", runtime.profile.JournalctlDigest, "--system", "--no-pager", "--quiet", "--output=json", "--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "--since=-24h", "--lines="+strconv.FormatUint(uint64(request.Limit)+1, 10), "_SYSTEMD_UNIT="+selected.Unit)
	if err != nil {
		return rejected(request, "RESULT_LIMIT")
	}
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	total := 0
	for _, line := range bytes.Split(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		// These fixed keys are emitted by pinned journalctl, not interpolated from a message containing JSON.
		// Эти fixed keys создаёт pinned journalctl, а не интерполяция message, содержащего JSON.
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) != nil || fields == nil {
			return rejected(request, "RECIPE_REJECTED")
		}
		for name := range fields {
			switch name {
			case "MESSAGE", "__REALTIME_TIMESTAMP", "__MONOTONIC_TIMESTAMP", "__CURSOR", "_BOOT_ID":
			default:
				return rejected(request, "RECIPE_REJECTED")
			}
		}
		var message, timestamp string
		if json.Unmarshal(fields["MESSAGE"], &message) != nil || json.Unmarshal(fields["__REALTIME_TIMESTAMP"], &timestamp) != nil || message == "" {
			return rejected(request, "RECIPE_REJECTED")
		}
		microseconds, err := strconv.ParseUint(timestamp, 10, 64)
		if err != nil || microseconds == 0 || strconv.FormatUint(microseconds, 10) != timestamp {
			return rejected(request, "RECIPE_REJECTED")
		}
		bounded := display(message, 4096)
		if bounded != message {
			reply.Truncated = true
		}
		if len(reply.Logs) == int(request.Limit) || total+len(bounded) > managedssh.MaximumContent {
			reply.Truncated = true
			break
		}
		total += len(bounded)
		reply.Logs = append(reply.Logs, managedssh.Log{Microseconds: microseconds, Message: bounded})
	}
	return reply
}

// tool executes an opened root-owned digest-pinned binary by descriptor with a fixed minimal environment and no shell, pager, inherited bus address or caller argv extension.
// tool выполняет opened root-owned digest-pinned binary через descriptor с fixed minimal environment, без shell, pager, inherited bus address либо caller argv extension.
func (runtime *Runtime) tool(ctx context.Context, name, digest string, arguments ...string) ([]byte, error) {
	if !managedssh.Digest(digest) || ctx.Err() != nil {
		return nil, ErrRejected
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, ErrRejected
	}
	defer file.Close()
	if !operatorFile(file, true) || fileDigest(file, 64<<20) != digest {
		return nil, ErrRejected
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(operation, "/proc/self/fd/3", arguments...)
	command.ExtraFiles = []*os.File{file}
	command.Args[0] = name
	command.Env = []string{"LANG=C", "LC_ALL=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=cat", "SYSTEMD_PAGERSECURE=1"}
	capture := &boundedOutput{}
	command.Stdout, command.Stderr = capture, io.Discard
	command.WaitDelay = time.Second
	if command.Run() != nil || capture.overflow || operation.Err() != nil {
		return nil, ErrRejected
	}
	return capture.bytes.Bytes(), nil
}

// boundedOutput caps fixed-tool stdout before any parser sees it and never retains stderr or unbounded external output.
// boundedOutput ограничивает fixed-tool stdout до parser и не сохраняет stderr либо unbounded external output.
type boundedOutput struct {
	bytes    bytes.Buffer
	overflow bool
}

// Write accepts only the remaining fixed budget and returns a safe error after overflow; the command context still bounds process lifetime.
// Write принимает только остаток fixed budget и возвращает safe error после overflow; command context отдельно ограничивает process lifetime.
func (output *boundedOutput) Write(raw []byte) (int, error) {
	remaining := maximumToolOutput - output.bytes.Len()
	if len(raw) > remaining {
		output.bytes.Write(raw[:remaining])
		output.overflow = true
		return remaining, ErrRejected
	}
	return output.bytes.Write(raw)
}
