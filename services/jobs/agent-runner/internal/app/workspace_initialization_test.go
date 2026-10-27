package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestInitializationTerminationUsesExactClosedBytes performs real local descriptor writes.
// No container, callback, model, credential read or external filesystem path is used.
// TestInitializationTerminationUsesExactClosedBytes пишет через реальные local descriptors,
// без контейнера, callback, модели, credentials либо внешних filesystem paths.
func TestInitializationTerminationUsesExactClosedBytes(t *testing.T) {
	input := runtimecontract.RunnerInput{RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64), SessionRef: "ses_fixture123", TurnRef: "trn_fixture123", Attempt: 1}
	raw, err := runtimecontract.EncodeInitializationFailure(runtimecontract.NewInitializationObservation(input, "INPUT_MANIFEST", "INPUT_INVALID"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "termination")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeInitializationTermination(file, raw); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(file)
	if err != nil || string(actual) != string(raw) {
		t.Fatal("termination record changed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if writeInitializationTermination(link, []byte("PRIVATE_SENTINEL")) == nil {
		t.Fatal("termination symlink followed")
	}
	if actual, _ := os.ReadFile(file); string(actual) != string(raw) {
		t.Fatal("symlink failure changed target")
	}
	if writeInitializationTermination(file, make([]byte, runtimecontract.MaximumInitializationFailureBytes+1)) == nil {
		t.Fatal("unbounded record written")
	}
	if writeInitializationTermination(file+"missing", raw) == nil {
		t.Fatal("termination path was created")
	}
	failure := publishInitializationFailure(runtimecontract.RunnerInput{}, errors.New("PRIVATE_SENTINEL"))
	if failure == nil || strings.Contains(fmt.Sprintf("%#v", failure), "PRIVATE_SENTINEL") {
		t.Fatal("missing input exposed cause or became success")
	}
}

// TestInitializationFilesystemReasonUsesTypedCauses never infers diagnosis from message words.
// TestInitializationFilesystemReasonUsesTypedCauses не выводит диагноз из слов сообщения.
func TestInitializationFilesystemReasonUsesTypedCauses(t *testing.T) {
	for _, item := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "CONTEXT_CANCELLED"}, {context.DeadlineExceeded, "DEADLINE_EXCEEDED"},
		{&os.PathError{Op: "write", Path: "PRIVATE", Err: syscall.EROFS}, "READ_ONLY"},
		{&os.PathError{Op: "write", Path: "PRIVATE", Err: syscall.ENOSPC}, "QUOTA_EXCEEDED"},
		{&os.PathError{Op: "write", Path: "PRIVATE", Err: syscall.EACCES}, "FILESYSTEM_IO_FAILED"},
		{errors.New("disk full DNS timeout"), "UNKNOWN"},
	} {
		if got := initializationFilesystemReason(item.err); got != item.want {
			t.Fatal("typed initialization reason mismatch")
		}
	}
}

// TestInitializationTerminationRefusesLinksAndFIFO checks descriptor safety before truncation.
// TestInitializationTerminationRefusesLinksAndFIFO проверяет descriptor safety до truncation.
func TestInitializationTerminationRefusesLinksAndFIFO(t *testing.T) {
	directory := t.TempDir()
	original := filepath.Join(directory, "original")
	link := filepath.Join(directory, "hardlink")
	if err := os.WriteFile(original, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, link); err != nil {
		t.Fatal(err)
	}
	if writeInitializationTermination(link, []byte("closed")) == nil {
		t.Fatal("termination hardlink accepted")
	}
	if raw, _ := os.ReadFile(original); string(raw) != "preserve" {
		t.Fatal("hardlink target truncated")
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if writeInitializationTermination(fifo, []byte("closed")) == nil {
		t.Fatal("termination FIFO accepted")
	}
}
