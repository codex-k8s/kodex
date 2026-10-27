package app

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// initializationTerminationPath is a kubelet-owned per-container file outside model workspace.
// initializationTerminationPath задаёт kubelet-owned файл контейнера вне model workspace.
const initializationTerminationPath = "/dev/termination-log"

// initializationFilesystemReason recognizes typed local failures without parsing path/error text.
// initializationFilesystemReason распознаёт typed local failures без разбора path/error text.
func initializationFilesystemReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "CONTEXT_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	case errors.Is(err, syscall.EROFS):
		return "READ_ONLY"
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "QUOTA_EXCEEDED"
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return "FILESYSTEM_IO_FAILED"
	}
	return "UNKNOWN"
}

// publishInitializationFailure reports only valid commitments and always preserves failure.
// Missing input or an unwritable termination file cannot fabricate a trusted record or success.
// publishInitializationFailure сообщает лишь valid commitments и всегда сохраняет отказ.
// Отсутствующий input либо недоступный файл не создают trusted record или success.
func publishInitializationFailure(input runtimecontract.RunnerInput, cause error) error {
	stage, reason := runtimecontract.InitializationErrorDetails(cause)
	observation := runtimecontract.NewInitializationObservation(input, stage, reason)
	if raw, err := runtimecontract.EncodeInitializationFailure(observation); err == nil {
		_ = writeInitializationTermination(initializationTerminationPath, raw)
	}
	return runtimecontract.InitializationError(stage, reason, cause)
}

// writeInitializationTermination writes one bounded record through a no-follow regular descriptor.
// It creates no paths, changes no modes and never falls back to raw logs or workspace storage.
// writeInitializationTermination пишет bounded record через no-follow regular descriptor.
// Пути и modes не меняются; fallback к raw logs либо workspace storage отсутствует.
func writeInitializationTermination(path string, raw []byte) error {
	if len(raw) == 0 || len(raw) > runtimecontract.MaximumInitializationFailureBytes {
		return io.ErrShortBuffer
	}
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "runtime-initialization-termination")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return io.ErrUnexpectedEOF
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return io.ErrUnexpectedEOF
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if count, err := file.Write(raw); err != nil {
		return err
	} else if count != len(raw) {
		return io.ErrShortWrite
	}
	return file.Sync()
}
