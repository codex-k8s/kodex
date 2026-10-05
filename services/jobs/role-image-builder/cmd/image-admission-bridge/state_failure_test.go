package main

import (
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/imageowner"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestReadOwnerStateIsPrivateBoundedAndClosed(t *testing.T) {
	for _, scenario := range []string{"valid", "symlink", "public_mode", "unknown_field", "extra_json", "oversized", "fifo"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner-claim.json")
			raw := `{"artifactId":"imgart_12345678","authorityGeneration":1}`
			if scenario == "unknown_field" {
				raw = `{"artifactId":"imgart_12345678","privateUnknown":1}`
			}
			if scenario == "extra_json" {
				raw += `{}`
			}
			if scenario == "oversized" {
				raw = strings.Repeat("x", maximumStateBytes+1)
			}
			if scenario == "fifo" {
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "public_mode" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "symlink" {
				link := filepath.Join(filepath.Dir(path), "alias.json")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			}
			var claim imageowner.Claim
			err := readState(path, &claim)
			if (err == nil) != (scenario == "valid") {
				t.Fatal("owner state violated private bounded closed contract")
			}
		})
	}
}
