package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestKubernetesCaptureSpoolOwnsAnonymousLifetime checks closure, cancellation and descriptor
// ownership without claiming durable stage deletion or any artifact publication authority.
// TestKubernetesCaptureSpoolOwnsAnonymousLifetime проверяет closure, cancellation и descriptor
// ownership, не утверждая durable stage deletion либо artifact publication authority.
func TestKubernetesCaptureSpoolOwnsAnonymousLifetime(t *testing.T) {
	repository := &Repository{}
	root := t.TempDir()
	directory, err := repository.ConfigureKubernetesCaptureSpool(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	spool, err := repository.kubernetesCaptureSpool.open(ctx, "STDOUT")
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	for index := 0; index < 4097; index++ {
		if _, err := spool.Write([]byte{'x'}); err != nil {
			t.Fatal("physical spool acquired a lifetime write cap", err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "private"))
	if err != nil || len(entries) != 0 {
		t.Fatal("capture exposed a named plaintext spool", err)
	}
	if directory.Close() == nil {
		t.Fatal("directory claimed teardown before its writer joined")
	}
	reader := &kubernetesCaptureSpoolReader{spool: spool}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Write([]byte{'y'}); err == nil {
		t.Fatal("inspection rewind allowed prefix overwrite")
	}
	body, err := io.ReadAll(reader)
	if err != nil || len(body) != 4097 || !bytes.Equal(body, bytes.Repeat([]byte{'x'}, 4097)) {
		t.Fatal("anonymous spool changed accepted bytes", err)
	}
	clear(body)
	cancel()
	if _, err := reader.Read(make([]byte, 1)); err == nil {
		t.Fatal("cancelled owner kept reading private spool")
	}
	if err := spool.Close(); err != nil || repository.kubernetesCaptureSpool.bytes != 0 {
		t.Fatal("spool physical quota was not released", err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal("joined spool directory remained open", err)
	}
}

// TestKubernetesCaptureSpoolRejectsSymlinkRoots prevents a deployment-path alias from selecting
// another directory, including a forged private child below an otherwise trusted mount.
// TestKubernetesCaptureSpoolRejectsSymlinkRoots не допускает выбора другого directory через
// deployment-path alias, включая forged private child под trusted mount.
func TestKubernetesCaptureSpoolRejectsSymlinkRoots(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "alias")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if closer, err := (&Repository{}).ConfigureKubernetesCaptureSpool(t.Context(), link); err == nil {
		_ = closer.Close()
		t.Fatal("capture accepted a symlink mount")
	}
	if err := os.Symlink(outside, filepath.Join(root, "private")); err != nil {
		t.Fatal(err)
	}
	if closer, err := (&Repository{}).ConfigureKubernetesCaptureSpool(t.Context(), root); err == nil {
		_ = closer.Close()
		t.Fatal("capture followed a forged private child")
	}
}

// TestKubernetesCaptureSpoolMaximumWireBudget writes and re-reads the complete existing
// 256-MiB ceiling using one64-KiB synthetic incompressible buffer. No transcript-sized allocation
// is needed and the next physical byte is refused without truncating the retained source.
// TestKubernetesCaptureSpoolMaximumWireBudget записывает и читает полные existing256 MiB
// через один64-KiB synthetic incompressible buffer. Transcript-sized allocation не требуется;
// следующий physical byte отклоняется без обрезания retained source.
func TestKubernetesCaptureSpoolMaximumWireBudget(t *testing.T) {
	repository := &Repository{}
	directory, err := repository.ConfigureKubernetesCaptureSpool(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	spool, err := repository.kubernetesCaptureSpool.open(t.Context(), "STDOUT")
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	buffer := make([]byte, 64<<10)
	defer clear(buffer)
	// Synthetic randomness measures incompressible storage; it never generates credentials.
	// Synthetic randomness измеряет incompressible storage; credentials не создаёт.
	random := rand.New(rand.NewSource(2145))
	expected := sha256.New()
	for written := 0; written < managedkubernetes.MaximumStreamBytes; written += len(buffer) {
		if _, err := random.Read(buffer); err != nil {
			t.Fatal(err)
		}
		_, _ = expected.Write(buffer)
		if count, err := spool.Write(buffer); err != nil || count != len(buffer) {
			t.Fatal("full declared spool budget was truncated", err)
		}
	}
	if _, err := spool.Write([]byte{1}); err == nil {
		t.Fatal("spool exceeded the original wire-byte ceiling")
	}
	reader := &kubernetesCaptureSpoolReader{spool: spool}
	if reader.Size() != managedkubernetes.MaximumStreamBytes {
		t.Fatal("spool size differs from the declared wire budget")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	actual := sha256.New()
	count, err := io.CopyBuffer(actual, reader, buffer)
	if err != nil || count != managedkubernetes.MaximumStreamBytes || !bytes.Equal(actual.Sum(nil), expected.Sum(nil)) {
		t.Fatal("maximum spool content did not survive exact readback", err)
	}
}
