package opencode

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// TestJoinNeverSignalsForeignProcessGroup rejects missing ownership and an explicitly selected foreign PGID before any signal.
// TestJoinNeverSignalsForeignProcessGroup отклоняет отсутствие ownership и явный чужой PGID до любого signal.
func TestJoinNeverSignalsForeignProcessGroup(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, attribute := range []*syscall.SysProcAttr{nil, {}, {Setpgid: true, Pgid: syscall.Getpgrp()}} {
		command := &exec.Cmd{Process: process, SysProcAttr: attribute}
		if !errors.Is(joinCommand(command), ErrWriterActive) {
			t.Fatal("foreign group became a joined writer")
		}
	}
	if process.Signal(syscall.Signal(0)) != nil {
		t.Fatal("ownership guard signaled caller")
	}
}

// TestStopFailureCannotTurnIntoExportPermission keeps an uncertain join closed across a second call without manufacturing process evidence.
// TestStopFailureCannotTurnIntoExportPermission сохраняет uncertain join закрытым при втором вызове без выдуманного process evidence.
func TestStopFailureCannotTurnIntoExportPermission(t *testing.T) {
	server := &Server{joinFailure: ErrWriterActive}
	if !errors.Is(server.Stop(), ErrWriterActive) || server.joined {
		t.Fatal("uncertain join became export permission")
	}
}
