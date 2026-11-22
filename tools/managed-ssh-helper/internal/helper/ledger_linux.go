package helper

import (
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/sys/unix"
)

// begin persists exactly one UNKNOWN intent before external modification; an existing, malformed or unreadable attempt is never overwritten or replayed.
// begin сохраняет ровно один UNKNOWN intent до external modification; существующая, malformed либо unreadable attempt не перезаписывается и не повторяется.
func (runtime *Runtime) begin(request managedssh.Request) (managedssh.Receipt, bool) {
	if !runtime.receiptCapacity() {
		return managedssh.Receipt{}, false
	}
	digest, err := managedssh.RequestDigest(request)
	if err != nil {
		return managedssh.Receipt{}, false
	}
	receipt := managedssh.Receipt{AttemptID: request.AttemptID, RequestDigest: digest, Operation: request.Operation, Status: managedssh.Unknown}
	raw, err := receiptBytes(request, receipt)
	if err != nil {
		return managedssh.Receipt{}, false
	}
	fd, err := unix.Openat(int(runtime.state.Fd()), request.AttemptID+".json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return managedssh.Receipt{}, false
	}
	file := os.NewFile(uintptr(fd), "managed-intent")
	length, writeError := file.Write(raw)
	syncError := file.Sync()
	closeError := file.Close()
	if writeError != nil || syncError != nil || closeError != nil || length != len(raw) || runtime.state.Sync() != nil {
		return managedssh.Receipt{}, false
	}
	return receipt, true
}

// receiptCapacity bounds retained intent/completion storage under the helper lock; interrupted temporary files consume capacity and are never silently pruned to permit replay.
// receiptCapacity ограничивает retained intent/completion storage под helper lock; прерванные temporary files занимают capacity и не удаляются для разрешения replay.
func (runtime *Runtime) receiptCapacity() bool {
	fd, err := unix.Openat(int(runtime.state.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	directory := os.NewFile(uintptr(fd), "receipt-capacity")
	defer directory.Close()
	names, err := directory.Readdirnames(int(runtime.profile.ReceiptLimit) + 2)
	if err != nil && err != io.EOF {
		return false
	}
	count := 0
	for _, name := range names {
		if name != "helper.lock" {
			count++
		}
	}
	return count < int(runtime.profile.ReceiptLimit)
}

// finish replaces a durable UNKNOWN receipt only after the selected effect and directory sync completed; failure leaves conservative UNKNOWN evidence.
// finish заменяет durable UNKNOWN receipt только после selected effect и directory sync; failure сохраняет conservative UNKNOWN evidence.
func (runtime *Runtime) finish(request managedssh.Request, receipt managedssh.Receipt) bool {
	raw, err := receiptBytes(request, receipt)
	if err != nil {
		return false
	}
	if atomicContent(runtime.state, ".kodex-"+request.AttemptID, request.AttemptID+".json", raw, false) != nil {
		return false
	}
	return runtime.state.Sync() == nil
}

// receipt reads existing evidence under the same lock without invoking the original recipe; unknown or malformed bytes are never treated as absence or permission to retry.
// receipt читает существующую evidence под тем же lock без вызова original recipe; unknown либо malformed bytes не считаются absence либо permission to retry.
func (runtime *Runtime) receipt(request managedssh.Request) managedssh.Reply {
	file, err := openManaged(runtime.state, request.AttemptID+".json", runtime.profile.UID, runtime.profile.GID)
	if err != nil {
		return rejected(request, fileCode(err))
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, managedssh.MaximumFrame+1))
	if err != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	reply, err := managedssh.DecodeReply(request, raw)
	if err != nil {
		return rejected(request, "RECIPE_REJECTED")
	}
	return reply
}

// receiptBytes stores the same validated read-only recovery envelope consumed by broker recovery, preserving exact original managed attempt pins.
// receiptBytes сохраняет тот же validated read-only recovery envelope, который читает broker recovery, с exact original managed attempt pins.
func receiptBytes(request managedssh.Request, receipt managedssh.Receipt) ([]byte, error) {
	read := managedssh.Request{Version: managedssh.Version, ProfileDigest: request.ProfileDigest, AttemptID: request.AttemptID, Operation: managedssh.ReceiptRead}
	reply, err := managedssh.NewReply(read, managedssh.Observed)
	if err != nil {
		return nil, err
	}
	reply.Receipt = &receipt
	return managedssh.EncodeReply(read, reply)
}
