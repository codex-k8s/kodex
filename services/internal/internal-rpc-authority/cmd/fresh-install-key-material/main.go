package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" {
		_, _ = fmt.Fprintln(os.Stderr, "fresh install key output directory is required")
		os.Exit(64)
	}
	output := os.Args[1]
	for _, item := range []struct {
		name string
		kid  string
	}{
		{"publisher/restore-signer", "ira-publisher-restore-g1"},
		{"publisher/readback-signer", "ira-publisher-readback-g1"},
		{"publisher/manifest-signer", "ira-publisher-manifest-g1"},
		{"restore/pitr-evidence", "ira-restore-pitr-evidence-g1"},
		{"platform-worker/automation-scheduler", "automation-scheduler-platform-worker-g1"},
		{"platform-worker/session-archive", "session-archive-platform-worker-g1"},
		{"platform-worker/integration-gateway", "integration-gateway-platform-worker-g1"},
		{"platform-worker/interaction-gateway", "interaction-gateway-platform-worker-g1"},
		{"platform-worker/email-bridge", "email-bridge-platform-worker-g1"},
		{"platform-worker/runtime-controller", "runtime-controller-platform-worker-g1"},
		{"platform-worker/role-image-builder", "role-image-builder-platform-worker-g1"},
		{"platform-worker/image-admission", "image-admission-platform-worker-g1"},
		{"platform-worker/image-promotion", "image-promotion-platform-worker-g1"},
		{"platform-worker/secret-broker", "secret-broker-platform-worker-g1"},
		{"platform-worker/control-plane", "control-plane-platform-worker-g1"},
	} {
		key, err := internalrpcauth.GenerateES256Key(item.kid)
		if err != nil {
			fatal("generate key", err)
		}
		privateJWK, err := internalrpcauth.MarshalPrivateJWK(key)
		if err != nil {
			fatal("marshal private JWK", err)
		}
		publicJWK, err := internalrpcauth.MarshalPublicJWK(key)
		if err != nil {
			fatal("marshal public JWK", err)
		}
		directory := filepath.Join(output, item.name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			fatal("create key directory", err)
		}
		if err := os.WriteFile(filepath.Join(directory, "private.jwk"), privateJWK, 0o600); err != nil {
			fatal("write private JWK", err)
		}
		if err := os.WriteFile(filepath.Join(directory, "public.jwk"), publicJWK, 0o600); err != nil {
			fatal("write public JWK", err)
		}
	}
}

func fatal(operation string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fresh install key generation failed: %s: %v\n", operation, err)
	os.Exit(1)
}
