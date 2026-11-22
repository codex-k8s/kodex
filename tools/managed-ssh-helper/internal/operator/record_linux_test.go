package operator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// recordFixture prepares a synthetic current qualification in an owned private directory; no selected host or real deployment is contacted.
// recordFixture подготавливает synthetic current qualification в owned private directory; selected host либо real deployment не вызываются.
func recordFixture(t *testing.T) (string, []string) {
	t.Helper()
	directory := t.TempDir()
	if os.Chmod(directory, 0700) != nil {
		t.Fatal("private fixture directory failed")
	}
	document, _ := operatorFixture(t)
	now := time.Now().Unix()
	document.Profiles[0].QualifiedAt = now - 1
	document.Profiles[0].ValidUntil = now + 3600
	raw, err := managedssh.EncodeQualifications(document, now)
	if err != nil {
		t.Fatal(err)
	}
	if os.WriteFile(filepath.Join(directory, "input.json"), raw, 0440) != nil {
		t.Fatal("private artifact fixture failed")
	}
	return directory, []string{"render", "--directory", directory, "--input", "input.json", "--output", "overlay.json"}
}

// TestOwnedProjectionRecordRejoin proves successful local writes are exclusive, exact checks rejoin the same content, and uncertain partial records remain untouched rather than being replayed.
// TestOwnedProjectionRecordRejoin доказывает exclusive local writes, exact content rejoin и сохранение uncertain partial records без replay либо overwrite.
func TestOwnedProjectionRecordRejoin(t *testing.T) {
	directory, args := recordFixture(t)
	result, err := Run(t.Context(), args)
	if err != nil || result.Status != "RENDERED_LOCAL_RECORD" {
		t.Fatal("local render failed")
	}
	path := filepath.Join(directory, "overlay.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0440 {
		t.Fatal("owned projection mode changed")
	}
	if _, err := Run(t.Context(), args); err != ErrRejected {
		t.Fatal("existing record was overwritten")
	}
	check := append([]string(nil), args...)
	check[0] = "check"
	rejoined, err := Run(t.Context(), check)
	if err != nil || rejoined.BundleSHA256 != result.BundleSHA256 || rejoined.Status != "CHECKED_LOCAL_RECORD" {
		t.Fatal("exact record check failed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("record changed during check")
	}
	if os.Chmod(path, 0600) != nil || os.WriteFile(path, before[:len(before)/2], 0440) != nil || os.Chmod(path, 0440) != nil {
		t.Fatal("partial fixture failed")
	}
	if _, err := Run(t.Context(), check); err != ErrRejected {
		t.Fatal("partial record was accepted")
	}
	if _, err := Run(t.Context(), args); err != ErrRejected {
		t.Fatal("partial record was replayed")
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before[:len(before)/2]) {
		t.Fatal("uncertain partial record was changed")
	}
}

// TestOwnedProjectionRejectsForeignFiles covers cancellation, symlink parents, writable or hardlinked input and malformed flags before creating any projection.
// TestOwnedProjectionRejectsForeignFiles проверяет cancellation, symlink parents, writable либо hardlinked input и malformed flags до создания projection.
func TestOwnedProjectionRejectsForeignFiles(t *testing.T) {
	for _, mode := range []string{"cancel", "writable", "hardlink", "parent-mode", "parent-symlink", "unknown-flag", "same-file", "traversal"} {
		t.Run(mode, func(t *testing.T) {
			directory, args := recordFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "cancel":
				cancel()
			case "writable":
				if os.Chmod(filepath.Join(directory, "input.json"), 0600) != nil {
					t.Fatal("chmod fixture failed")
				}
			case "hardlink":
				if os.Link(filepath.Join(directory, "input.json"), filepath.Join(directory, "linked.json")) != nil {
					t.Fatal("link fixture failed")
				}
			case "parent-mode":
				if os.Chmod(directory, 0755) != nil {
					t.Fatal("chmod fixture failed")
				}
			case "parent-symlink":
				link := directory + "-link"
				if os.Symlink(directory, link) != nil {
					t.Fatal("symlink fixture failed")
				}
				t.Cleanup(func() { _ = os.Remove(link) })
				args[2] = link
			case "unknown-flag":
				args[3] = "--foreign"
			case "same-file":
				args[6] = "input.json"
			case "traversal":
				args[6] = "../foreign.json"
			}
			if _, err := Run(ctx, args); err != ErrRejected {
				t.Fatal("unsafe file or invocation accepted")
			}
			if _, err := os.Stat(filepath.Join(directory, "overlay.json")); !os.IsNotExist(err) {
				t.Fatal("rejected operation created projection")
			}
		})
	}
}
