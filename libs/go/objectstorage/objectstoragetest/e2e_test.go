package objectstoragetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoopbackHTTPEndpointBoundary pins accepted local origins and rejects URL escape routes.
// RU: TestLoopbackHTTPEndpointBoundary закрепляет допустимые локальные origins и отклоняет обходы через URL.
func TestLoopbackHTTPEndpointBoundary(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8080", "http://localhost:8333/"} {
		if !validLoopbackHTTPEndpoint(raw) || RequireLoopbackHTTPEndpoint(t, raw, "E2E") != raw {
			t.Fatalf("local endpoint rejected: %q", raw)
		}
	}
	for _, raw := range []string{"", "https://localhost:8333", "http://localhost", "http://remote.invalid:8333",
		"http://[::1]:8333", "http://user@localhost:8333", "http://localhost:8333/path",
		"http://localhost:8333/?token=x", "http://localhost:8333/#fragment", "http://localhost:invalid"} {
		if validLoopbackHTTPEndpoint(raw) {
			t.Fatalf("nonlocal or malformed endpoint accepted: %q", raw)
		}
	}
}

// TestE2ECredentialBoundary checks exact fixture file classes without printing secret bytes.
// RU: TestE2ECredentialBoundary проверяет классы файлов реквизитов оснастки, не выводя секретные байты.
func TestE2ECredentialBoundary(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "credential")
	if err := os.WriteFile(path, []byte("  fixture-value  "), 0o600); err != nil {
		t.Fatal(err)
	}
	if value, failure := readE2ECredential(path); failure != credentialValid || value != "fixture-value" {
		t.Fatal("private credential fixture was rejected")
	}
	if value := ReadE2ECredential(t, path, "E2E"); value != "fixture-value" {
		t.Fatal("public credential fixture changed the value")
	}
	if _, failure := readE2ECredential("relative-file"); failure != credentialInvalidFile {
		t.Fatal("relative credential path accepted")
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, failure := readE2ECredential(link); failure != credentialInvalidFile {
		t.Fatal("credential symlink accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, failure := readE2ECredential(path); failure != credentialInvalidFile {
		t.Fatal("world-readable credential accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", strings.Repeat("x", (16<<10)+1)} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, failure := readE2ECredential(path); failure != credentialUnreadable {
			t.Fatal("empty or oversized credential accepted")
		}
	}
	for _, value := range []string{"   ", "first\nsecond"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, failure := readE2ECredential(path); failure != credentialInvalidValue {
			t.Fatal("malformed credential accepted")
		}
	}
}
