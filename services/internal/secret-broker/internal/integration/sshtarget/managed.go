package sshtarget

import (
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// managedHelperCommand is fixed by the operator installation; neither a capability payload nor remote output may replace the executable or add shell arguments.
// managedHelperCommand фиксируется operator installation; capability payload либо remote output не могут заменить executable либо добавить shell arguments.
const managedHelperCommand = "/usr/local/libexec/kodex-ssh-target"

// RecoveryExpectation comes from the owner's immutable original attempt and prevents receipt observation from adopting another operation or input digest.
// RecoveryExpectation берётся из immutable original attempt владельца и не позволяет receipt observation принять другую operation либо input digest.
type RecoveryExpectation struct {
	Operation managedssh.Operation
	Digest    string
}

// ManagedRequest carries one closed helper request plus mandatory original pins for recovery; it contains no permission, credential locator or caller-selected remote program.
// ManagedRequest содержит один closed helper request и обязательные original pins для recovery; permission, credential locator либо caller-selected remote program отсутствуют.
type ManagedRequest struct {
	Wire     managedssh.Request
	Recovery *RecoveryExpectation
}

// Managed executes only the fixed installed helper with canonical stdin; the caller must already own fresh consent, exact target/profile qualification and broker-only material selection.
// Managed выполняет только fixed installed helper с canonical stdin; caller обязан заранее получить fresh consent, exact target/profile qualification и broker-only material selection.
func (client Client) Managed(ctx context.Context, target Target, privateKey []byte, profile managedssh.Profile, request ManagedRequest) (managedssh.Reply, error) {
	return client.managed(ctx, target, privateKey, profile, request, nil)
}

// managed shares the finite helper codec while allowing the qualified entrypoint to reread trusted deployment evidence immediately before SSH Start, after channel-opening I/O.
// managed использует общий finite helper codec; qualified entrypoint перечитывает trusted deployment evidence непосредственно перед SSH Start, после channel-opening I/O.
func (client Client) managed(ctx context.Context, target Target, privateKey []byte, profile managedssh.Profile, request ManagedRequest, beforeStart func() error) (managedssh.Reply, error) {
	digest, err := profile.Digest()
	if err != nil || request.Wire.ProfileDigest != digest || request.Wire.Validate() != nil || profile.ValidUntil <= time.Now().Unix() || !managedScope(profile, request) {
		return managedssh.Reply{}, ErrRejected
	}
	raw, err := managedssh.EncodeRequest(request.Wire)
	if err != nil {
		return managedssh.Reply{}, ErrRejected
	}
	defer clear(raw)
	result, err := client.run(ctx, target, privateKey, Command{Text: managedHelperCommand, Input: raw, MaximumOutput: managedssh.MaximumReply}, nil, beforeStart)
	defer clear(result.Stdout)
	defer clear(result.Stderr)
	if err != nil {
		return managedssh.Reply{}, err
	}
	if !result.SessionExited || result.ExitStatus != 0 || len(result.Stderr) != 0 {
		return managedssh.Reply{}, &Failure{cause: ErrRejected, unknown: true}
	}
	reply, err := managedssh.DecodeReply(request.Wire, result.Stdout)
	if err != nil {
		return managedssh.Reply{}, &Failure{cause: ErrRejected, unknown: true}
	}
	if request.Wire.Operation == managedssh.Inspect && reply.Status == managedssh.Observed &&
		(reply.Identity.UID != profile.UID || reply.Identity.GID != profile.GID || !slices.Equal(reply.Identity.Groups, profile.Groups) || reply.Identity.Executable != profile.ExecutableDigest) {
		return managedssh.Reply{}, &Failure{cause: ErrRejected, unknown: true}
	}
	if request.Wire.Operation == managedssh.ReceiptRead && reply.Status == managedssh.Observed &&
		(reply.Receipt.Operation != request.Recovery.Operation || reply.Receipt.RequestDigest != request.Recovery.Digest) {
		return managedssh.Reply{}, &Failure{cause: ErrRejected, unknown: true}
	}
	return reply, nil
}

// managedScope narrows resources to the operator-pinned finite aliases and requires exact original effect metadata for every recovery read; strings never classify shell commands as reads.
// managedScope ограничивает resources конечными operator-pinned aliases и требует exact original effect metadata для каждого recovery read; строки не классифицируют shell commands как чтение.
func managedScope(profile managedssh.Profile, request ManagedRequest) bool {
	if request.Wire.Operation == managedssh.ReceiptRead {
		if request.Recovery == nil || !managedssh.Digest(request.Recovery.Digest) {
			return false
		}
		effect, err := request.Recovery.Operation.Effect()
		return err == nil && effect != managedssh.ReadOnly
	}
	if request.Recovery != nil {
		return false
	}
	switch request.Wire.Operation {
	case managedssh.FileList, managedssh.FileStat, managedssh.FileRead, managedssh.FileCreate, managedssh.FileReplace, managedssh.FileDelete:
		for _, root := range profile.Roots {
			if root.Alias == request.Wire.Resource {
				return true
			}
		}
		return false
	case managedssh.ServiceRead, managedssh.ServiceRestart, managedssh.ServiceReload, managedssh.JournalRead:
		for _, service := range profile.Services {
			if service.Alias == request.Wire.Resource {
				return true
			}
		}
		return false
	case managedssh.Inspect, managedssh.SystemRead, managedssh.ProcessList:
		return true
	default:
		return false
	}
}
