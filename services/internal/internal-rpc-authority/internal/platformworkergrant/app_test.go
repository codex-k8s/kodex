package platformworkergrant

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func TestRotateWritesExactBoundedGrant(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	key, err := internalrpcauth.GenerateES256Key("runtime-controller-platform-worker-g1")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{
		WorkloadID: "runtime-controller",
		OutputFile: filepath.Join(directory, "application-grant.jws"),
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatalf("материализовать grant: %v", err)
	}
	if err := readBack(configuration, key, now); err != nil {
		t.Fatalf("проверить grant: %v", err)
	}
	info, err := os.Stat(configuration.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("небезопасные права grant: %o", info.Mode().Perm())
	}
}

func TestWriteAtomicRejectsSymlinkDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(link, "grant.jws"), []byte("signed")); err == nil {
		t.Fatal("symlink output directory был принят")
	}
}
