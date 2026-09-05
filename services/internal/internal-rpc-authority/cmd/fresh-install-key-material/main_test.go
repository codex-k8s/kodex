package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func TestFreshKeyMaterialProcess(t *testing.T) {
	if os.Getenv("KODEX_TEST_FRESH_KEYS_OUTPUT") != "" {
		os.Args = []string{"fresh-install-key-material", os.Getenv("KODEX_TEST_FRESH_KEYS_OUTPUT")}
		main()
		return
	}
	t.Parallel()
	output := t.TempDir()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFreshKeyMaterialProcess$")
	command.Env = append(os.Environ(), "KODEX_TEST_FRESH_KEYS_OUTPUT="+output)
	if err := command.Run(); err != nil {
		t.Fatalf("fresh key generator: %v", err)
	}
	keys := map[string]bool{}
	for _, workload := range []string{
		"automation-scheduler", "session-archive", "integration-gateway", "interaction-gateway",
		"email-bridge", "runtime-controller", "role-image-builder", "image-admission",
		"image-promotion", "secret-broker", "control-plane",
	} {
		directory := filepath.Join(output, "platform-worker", workload)
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("worker key directory permissions mismatch")
		}
		read := func(name string) []byte {
			t.Helper()
			file := filepath.Join(directory, name)
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatal("worker key file permissions mismatch")
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal("worker key file cannot be read")
			}
			return data
		}
		private, err := internalrpcauth.ParsePrivateJWK(read("private.jwk"))
		if err != nil || private.KeyID != workload+"-platform-worker-g1" {
			t.Fatal("worker private key identity mismatch")
		}
		public := read("public.jwk")
		parsed, err := internalrpcauth.ParsePublicJWK(public)
		if err != nil || parsed.KeyID != private.KeyID {
			t.Fatal("worker public key identity mismatch")
		}
		derived, err := internalrpcauth.MarshalPublicJWK(private)
		if err != nil || !bytes.Equal(derived, public) {
			t.Fatal("worker public key does not match private key")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(public, &fields); err != nil {
			t.Fatal("public key JSON is invalid")
		}
		if _, present := fields["d"]; present {
			t.Fatal("public trust includes private material")
		}
		identity := string(fields["x"]) + ":" + string(fields["y"])
		if keys[identity] {
			t.Fatal("different workloads share a signing key")
		}
		keys[identity] = true
	}
	entries, err := os.ReadDir(filepath.Join(output, "platform-worker"))
	if err != nil || len(entries) != len(keys) {
		t.Fatal("fresh worker key registry differs from the closed workload set")
	}
}
