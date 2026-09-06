package callback

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactSpoolUnlinksImmediatelyAndBoundsConcurrentTransfers(t *testing.T) {
	directory := t.TempDir()
	spool, err := openArtifactSpool(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.close() })
	first, releaseFirst, err := spool.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	second, releaseSecond, err := spool.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	if _, _, err := spool.acquire(t.Context()); !errors.Is(err, errArtifactCapacity) {
		t.Fatal("unbounded transfer concurrency")
	}
	if _, err := first.Write([]byte("synthetic fixture")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "private"))
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary content has a reachable pathname")
	}
	if err := spool.check(t.Context()); err != nil {
		t.Fatal("bounded canary rejected while slots are occupied")
	}
	releaseFirst()
	releaseFirst()
	if _, err := first.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal("released file remains open")
	}
	third, releaseThird, err := spool.acquire(t.Context())
	if err != nil {
		t.Fatal("completed transfer did not return capacity")
	}
	defer releaseThird()
	if err := spool.close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := spool.acquire(t.Context()); err == nil {
		t.Fatal("closed spool accepted a transfer")
	}
	if _, err := second.Seek(0, io.SeekStart); err != nil {
		t.Fatal("closing directory invalidated an in-flight private descriptor")
	}
	releaseThird()
	if _, err := third.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal("terminal descriptor not released")
	}
}

func TestArtifactSpoolRejectsUnsafeDirectoryAndCanceledAcquisition(t *testing.T) {
	for _, scenario := range []string{"symlink mount", "symlink private", "public private", "world writable"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			switch scenario {
			case "symlink mount":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(directory, link); err != nil {
					t.Fatal(err)
				}
				directory = link
			case "symlink private":
				if err := os.Symlink(t.TempDir(), filepath.Join(directory, "private")); err != nil {
					t.Fatal(err)
				}
			case "public private":
				private := filepath.Join(directory, "private")
				if err := os.Mkdir(private, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(private, 0755); err != nil {
					t.Fatal(err)
				}
				if info, err := os.Stat(private); err != nil || info.Mode().Perm() != 0755 {
					t.Fatal("unsafe directory fixture has unexpected permissions")
				}
			case "world writable":
				if err := os.Chmod(directory, 0777); err != nil {
					t.Fatal(err)
				}
			}
			spool, err := openArtifactSpool(directory)
			if err == nil {
				_ = spool.close()
				t.Fatal("unsafe spool directory accepted")
			}
		})
	}
	spool := fixtureArtifactSpool(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := spool.acquire(ctx); !errors.Is(err, context.Canceled) || len(spool.slots) != 0 {
		t.Fatal("canceled acquisition consumed capacity")
	}
}
