package kubernetestarget

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestKubernetesIdentityExportProcess feeds an actual kubectl/Python export into the real broker parser.
// Only synthetic credentials and the explicitly selected native binary are used; no API is contacted.
// TestKubernetesIdentityExportProcess передаёт actual kubectl/Python export настоящему broker parser.
// Используются только synthetic credentials и явно выбранный native binary; API не вызывается.
func TestKubernetesIdentityExportProcess(t *testing.T) {
	binary := os.Getenv("KODEX_TEST_KUBECTL_BINARY")
	if binary == "" {
		t.Skip("run make test-managed-kubernetes-identity-export with KODEX_TEST_KUBECTL_BINARY for native qualification")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("native kubectl must be selected by absolute path")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("native Python is unavailable")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("native test source location unavailable")
	}
	script := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", "..", "..", "tools", "dev", "managed-kubernetes-identity.py"))
	for _, mode := range []string{"token", "certificate", "certificate-files"} {
		t.Run(mode, func(t *testing.T) {
			config, target, now := credentialFixture(t)
			config.Contexts[0].Name = "owner-local-context"
			other := config.Contexts[0]
			other.Name = "unselected-context"
			config.Contexts = append(config.Contexts, other)
			config.CurrentContext = other.Name
			if mode == "token" {
				config.Users[0].User.Certificate, config.Users[0].User.Key = "", ""
				config.Users[0].User.Token = "synthetic-export-token-never-print"
			}
			if _, err := ParseCredential(encodeCredential(t, config), target, now); err == nil {
				t.Fatal("unminified fixture unexpectedly passed the broker")
			}
			root := t.TempDir()
			input, output := filepath.Join(root, "input"), filepath.Join(root, "embedded")
			encoded := encodeCredential(t, config)
			if mode == "certificate-files" {
				for _, file := range []struct {
					name, field, content string
					mode                 os.FileMode
				}{
					{"ca.pem", "certificate-authority-data", config.Clusters[0].Cluster.Authority, 0o644},
					{"client.pem", "client-certificate-data", config.Users[0].User.Certificate, 0o644},
					{"client.key", "client-key-data", config.Users[0].User.Key, 0o600},
				} {
					material, err := base64.StdEncoding.DecodeString(file.content)
					if err != nil || os.WriteFile(filepath.Join(root, file.name), material, file.mode) != nil {
						t.Fatal("write disposable credential reference")
					}
					clear(material)
					encoded = bytes.Replace(encoded, []byte(file.field+": "+file.content), []byte(strings.TrimSuffix(file.field, "-data")+": "+file.name), 1)
				}
			}
			if os.WriteFile(input, encoded, 0o600) != nil {
				t.Fatal("write private synthetic source")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, python, "-I", "-B", script, "export", "--kubectl", binary, "--kubeconfig", input, "--context", "owner-local-context", "--target-context", target.Context, "--output", output)
			command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if command.Run() != nil || ctx.Err() != nil || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatal("owner-local export failed or emitted material/diagnostics")
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal("read private exported fixture")
			}
			defer clear(raw)
			info, err := os.Stat(output)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("exported fixture is not owner-private")
			}
			credential, err := ParseCredential(raw, target, now)
			if err != nil || mode == "token" && credential.token != config.Users[0].User.Token || mode != "token" && len(credential.cert.Certificate) != 1 {
				t.Fatal("actual export is incompatible with the broker's strict embedded profile")
			}
		})
	}
}
