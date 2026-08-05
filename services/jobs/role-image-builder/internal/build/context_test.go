package build

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestExtractContextAcceptsExactRegularArchive(t *testing.T) {
	sourceSHA := strings.Repeat("a", 64)
	raw := tarBytes(t, []tarEntry{{name: ".mattercodex/source.sha256", body: sourceSHA + "\n", kind: tar.TypeReg},
		{name: "cmd/runtime/main.go", body: "package main\n", kind: tar.TypeReg}})
	archive := filepath.Join(t.TempDir(), "context.tar")
	if err := os.WriteFile(archive, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	destination := filepath.Join(t.TempDir(), "context")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ExtractContext(archive, destination, hex.EncodeToString(digest[:]), sourceSHA); err != nil {
		t.Fatalf("exact context rejected: %v", err)
	}
}

func TestExtractContextRejectsTraversalLinksAndSourceMismatch(t *testing.T) {
	for name, entries := range map[string][]tarEntry{
		"traversal": {{name: "../escape", body: "x", kind: tar.TypeReg}},
		"symlink":   {{name: "link", link: "/etc/passwd", kind: tar.TypeSymlink}},
		"source":    {{name: ".mattercodex/source.sha256", body: strings.Repeat("b", 64), kind: tar.TypeReg}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := tarBytes(t, entries)
			archive := filepath.Join(t.TempDir(), "context.tar")
			if err := os.WriteFile(archive, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			destination := filepath.Join(t.TempDir(), "context")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := ExtractContext(archive, destination, hex.EncodeToString(digest[:]), strings.Repeat("a", 64)); err == nil {
				t.Fatal("unsafe context accepted")
			}
		})
	}
}

func TestDockerfileKeepsInstallationStepPhysicallySecretFreeAndRestoresRuntimeABI(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{
		FrontendSha256: strings.Repeat("a", 64), BaseImageReference: "registry.example/base/runtime",
		BaseImageDigest: "sha256:" + strings.Repeat("b", 64), SpecSha256: strings.Repeat("c", 64),
		RoleRuntimeContractSha256: strings.Repeat("d", 64),
		InstallationBlock:         "echo first", BuildSecretRefs: []string{"vault-versioned://builder/token/v1"},
	}
	first := installationScript(input)
	dockerfileRaw := dockerfile(input, "registry.example.test/mattercodex/dockerfile")
	input.InstallationBlock = "echo second"
	second := installationScript(input)
	if bytes.Equal(first, second) {
		t.Fatal("installation block did not affect BuildKit input")
	}
	if bytes.Contains(dockerfileRaw, []byte(input.BuildSecretRefs[0])) || bytes.Contains(dockerfileRaw, []byte("type=secret")) ||
		bytes.Contains(dockerfileRaw, first) || !bytes.Contains(dockerfileRaw, []byte("from=mattercodex-install")) ||
		bytes.Contains(dockerfileRaw, []byte("COPY .")) || !bytes.Contains(dockerfileRaw, []byte("target=/workspace/source,readonly")) ||
		!bytes.Contains(dockerfileRaw, []byte("COPY --from=trusted-runtime /usr/local/bin/mattercodex-init")) ||
		!bytes.Contains(dockerfileRaw, []byte("ENTRYPOINT [\"/usr/local/bin/mattercodex-init\",\"entrypoint\",\"/usr/local/bin/matter-codex-agent-runner\"]")) {
		t.Fatal("Dockerfile exposed credentials or omitted the protected runtime ABI")
	}
}

func TestExtractContextHashesTrailingAndMutatedBytesFromTheSameStream(t *testing.T) {
	sourceSHA := strings.Repeat("a", 64)
	raw := tarBytes(t, []tarEntry{{name: ".mattercodex/source.sha256", body: sourceSHA, kind: tar.TypeReg}})
	digest := sha256.Sum256(raw)
	for name, input := range map[string][]byte{
		"trailing": append(append([]byte(nil), raw...), []byte("tampered")...),
		"mutated":  append(append([]byte(nil), raw[:len(raw)/2]...), bytes.Repeat([]byte{'x'}, len(raw)-len(raw)/2)...),
	} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "context")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := ExtractContextReader(bytes.NewReader(input), destination, hex.EncodeToString(digest[:]), sourceSHA); err == nil {
				t.Fatal("changed bytes were accepted after streaming extraction")
			}
		})
	}
}

func TestPinnedPackageAndToolBlobsAreVerifiedAndInstalledOffline(t *testing.T) {
	packageBody, toolBody := "package-bytes", "tool-bytes"
	packageDigest := sha256.Sum256([]byte(packageBody))
	toolDigest := sha256.Sum256([]byte(toolBody))
	packageHex, toolHex := hex.EncodeToString(packageDigest[:]), hex.EncodeToString(toolDigest[:])
	root := t.TempDir()
	for path, body := range map[string]string{
		filepath.Join(root, ".mattercodex", "packages", packageHex): packageBody,
		filepath.Join(root, ".mattercodex", "tools", toolHex):       toolBody,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	input := &controlplanev1.RoleImageBuildInput{
		Packages:          []*controlplanev1.RoleImagePackage{{Manager: "apt", Name: "example", Version: "1", Digest: "sha256:" + packageHex}},
		Tools:             []*controlplanev1.RoleImageTool{{Name: "example-tool", Version: "1", SourceRef: "oci://example/tool", Sha256: toolHex}},
		InstallationBlock: "true",
	}
	if err := verifyPinnedInputs(root, input); err != nil {
		t.Fatalf("exact pinned inputs rejected: %v", err)
	}
	script := installationScript(input)
	if !bytes.Contains(script, []byte("dpkg --install")) || !bytes.Contains(script, []byte("install -m 0555")) {
		t.Fatal("typed package or tool was omitted from the offline installation script")
	}
	if err := os.WriteFile(filepath.Join(root, ".mattercodex", "tools", toolHex), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPinnedInputs(root, input); err == nil {
		t.Fatal("tampered pinned tool accepted")
	}
}

func TestProvenanceBindingCoversImmutableTuple(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{
		SpecSha256: strings.Repeat("a", 64), ImmutableBuildSha256: strings.Repeat("b", 64),
		PolicyRevision: 7, PolicySha256: strings.Repeat("c", 64),
	}
	first, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first != "d7312284b2e409541ad86d086a275e651154664b812f4bfb6411f2ffee1db006" {
		t.Fatalf("unexpected canonical provenance binding: %s", first)
	}
	input.ImmutableBuildSha256 = strings.Repeat("e", 64)
	second, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("immutable build tuple did not change provenance binding")
	}
}

type tarEntry struct {
	name, body, link string
	kind             byte
}

func tarBytes(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Linkname: entry.link, Mode: 0o600, Size: int64(len(entry.body))}
		if entry.kind != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.kind == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
