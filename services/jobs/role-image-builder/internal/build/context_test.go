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

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestExtractContextAcceptsExactRegularArchive accepts a synthetic regular-file tar
// containing the expected source binding and a Go source file, using the digest of
// exactly the written archive bytes without starting BuildKit or compiling the file.
// TestExtractContextAcceptsExactRegularArchive принимает искусственный tar обычных
// файлов с нужным source binding и Go-файлом по хешу точных записанных байтов,
// без запуска BuildKit и компиляции содержимого.
func TestExtractContextAcceptsExactRegularArchive(t *testing.T) {
	sourceSHA := strings.Repeat("a", 64)
	raw := tarBytes(t, []tarEntry{{name: ".kodex/source.sha256", body: sourceSHA + "\n", kind: tar.TypeReg},
		{name: "cmd/runtime/main.go", body: "package main\n", kind: tar.TypeReg}})
	archive, destination, digest := contextArchiveFixture(t, raw)
	if err := ExtractContext(archive, destination, digest, sourceSHA); err != nil {
		t.Fatalf("exact context rejected: %v", err)
	}
}

// TestExtractContextRejectsTraversalLinksAndSourceMismatch rejects separate lexical
// escape, symlink and wrong-source fixtures even though their archive digests match.
// The symlink is only a tar header and never reads its referenced host path.
// TestExtractContextRejectsTraversalLinksAndSourceMismatch отклоняет отдельные
// escape, symlink и wrong-source fixtures даже с правильным хешем архива.
// Symlink существует только как tar header и не читает указанный путь хоста.
func TestExtractContextRejectsTraversalLinksAndSourceMismatch(t *testing.T) {
	for name, entries := range map[string][]tarEntry{
		"traversal": {{name: "../escape", body: "x", kind: tar.TypeReg}},
		"symlink":   {{name: "link", link: "/etc/passwd", kind: tar.TypeSymlink}},
		"source":    {{name: ".kodex/source.sha256", body: strings.Repeat("b", 64), kind: tar.TypeReg}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := tarBytes(t, entries)
			archive, destination, digest := contextArchiveFixture(t, raw)
			if err := ExtractContext(archive, destination, digest, strings.Repeat("a", 64)); err == nil {
				t.Fatal("unsafe context accepted")
			}
		})
	}
}

// TestDockerfileKeepsInstallationStepPhysicallySecretFreeAndRestoresRuntimeABI checks
// generated Dockerfile text for separated install input, no secret mount or broad
// COPY, a readonly source mount and trusted runtime init/entrypoint restoration.
// It is a source-text assertion, not an actual Docker build or secret-exfiltration test.
// TestDockerfileKeepsInstallationStepPhysicallySecretFreeAndRestoresRuntimeABI
// проверяет текст Dockerfile: отдельный install input, отсутствие secret mount
// и широкого COPY, readonly source и восстановление trusted init/entrypoint.
// Это проверка текста, не настоящая Docker-сборка либо secret-exfiltration test.
func TestDockerfileKeepsInstallationStepPhysicallySecretFreeAndRestoresRuntimeABI(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{
		FrontendSha256: strings.Repeat("a", 64), BaseImageReference: "registry.example/base/runtime",
		BaseImageDigest: "sha256:" + strings.Repeat("b", 64), SpecSha256: strings.Repeat("c", 64),
		RoleRuntimeContractSha256: strings.Repeat("d", 64),
		InstallationBlock:         "echo first",
	}
	first := installationScript(input)
	dockerfileRaw := dockerfile(
		input,
		"registry.example.test/kodex/dockerfile",
		"registry.example.test/kodex/agent-runner",
		"sha256:"+strings.Repeat("e", 64),
	)
	input.InstallationBlock = "echo second"
	second := installationScript(input)
	if bytes.Equal(first, second) {
		t.Fatal("installation block did not affect BuildKit input")
	}
	if bytes.Contains(dockerfileRaw, []byte("type=secret")) ||
		bytes.Contains(dockerfileRaw, first) || !bytes.Contains(dockerfileRaw, []byte("from=kodex-install")) ||
		bytes.Contains(dockerfileRaw, []byte("COPY .")) || !bytes.Contains(dockerfileRaw, []byte("target=/workspace/source,readonly")) ||
		!bytes.Contains(dockerfileRaw, []byte("COPY --from=trusted-runtime /usr/local/bin/kodex-init")) ||
		!bytes.Contains(dockerfileRaw, []byte("ENTRYPOINT [\"/usr/local/bin/kodex-init\",\"entrypoint\",\"/usr/local/bin/kodex-agent-runner\"]")) {
		t.Fatal("Dockerfile exposed credentials or omitted the protected runtime ABI")
	}
}

// TestExtractContextHashesTrailingAndMutatedBytesFromTheSameStream keeps the original
// expected digest while appending or mutating synthetic archive bytes, requiring
// streaming extraction to fail rather than trust a previously hashed different stream.
// TestExtractContextHashesTrailingAndMutatedBytesFromTheSameStream сохраняет исходный
// ожидаемый хеш, добавляя либо меняя байты архива, и требует отказа streaming extractor,
// а не доверия хешу ранее прочитанного другого потока.
func TestExtractContextHashesTrailingAndMutatedBytesFromTheSameStream(t *testing.T) {
	sourceSHA := strings.Repeat("a", 64)
	raw := tarBytes(t, []tarEntry{{name: ".kodex/source.sha256", body: sourceSHA, kind: tar.TypeReg}})
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

// TestPinnedPackageAndToolBlobsAreVerifiedAndInstalledOffline verifies exact local
// package/tool fixture hashes, checks generated offline installation commands and
// rejects a modified tool blob. No package manager, downloaded tool or install script
// is executed; command presence is not proof of a completed real image installation.
// TestPinnedPackageAndToolBlobsAreVerifiedAndInstalledOffline проверяет точные локальные
// хеши package/tool, наличие offline install-команд и отказ изменённому tool blob.
// Package manager, загруженный tool и install script не исполняются; наличие команд
// не доказывает завершённую установку в настоящем образе.
func TestPinnedPackageAndToolBlobsAreVerifiedAndInstalledOffline(t *testing.T) {
	packageBody, toolBody := "package-bytes", "tool-bytes"
	packageDigest := sha256.Sum256([]byte(packageBody))
	toolDigest := sha256.Sum256([]byte(toolBody))
	packageHex, toolHex := hex.EncodeToString(packageDigest[:]), hex.EncodeToString(toolDigest[:])
	root := t.TempDir()
	for path, body := range map[string]string{
		filepath.Join(root, ".kodex", "packages", packageHex): packageBody,
		filepath.Join(root, ".kodex", "tools", toolHex):       toolBody,
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
	if err := os.WriteFile(filepath.Join(root, ".kodex", "tools", toolHex), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPinnedInputs(root, input); err == nil {
		t.Fatal("tampered pinned tool accepted")
	}
}

// TestProvenanceBindingCoversImmutableTuple checks a 64-character provenance hash,
// sensitivity to immutable-build/project changes and organization/project scope
// consistency. It does not sign provenance or prove every possible field's influence.
// TestProvenanceBindingCoversImmutableTuple проверяет 64-символьный provenance hash,
// влияние immutable-build/project и согласованность organization/project scope.
// Подпись provenance и влияние всех возможных полей этим тестом не доказываются.
func TestProvenanceBindingCoversImmutableTuple(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{
		ScopeKind:       controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT,
		OrganizationRef: "org_example123", ProjectRef: "prj_example123",
		SpecSha256: strings.Repeat("a", 64), ImmutableBuildSha256: strings.Repeat("b", 64),
		PolicyRevision: 7, PolicySha256: strings.Repeat("c", 64),
	}
	first, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatal("canonical provenance binding is not SHA256")
	}
	input.ImmutableBuildSha256 = strings.Repeat("e", 64)
	second, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("immutable build tuple did not change provenance binding")
	}
	input.ProjectRef = "prj_other123"
	third, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64))
	if err != nil || third == second {
		t.Fatal("project owner tuple did not change provenance binding")
	}
	input.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	if _, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64)); err == nil {
		t.Fatal("organization input accepted a project locator")
	}
	input.ProjectRef = ""
	if _, err := provenanceBindingSHA256(input, "sha256:"+strings.Repeat("d", 64)); err != nil {
		t.Fatal("organization owner tuple was rejected")
	}
}

// contextArchiveFixture writes the exact supplied bytes into a private temporary
// archive, hashes those bytes and creates a separate empty private destination in
// the same order as the original two test setups. It does not parse or repair tar
// input, choose a source binding or invoke the extractor under test.
// contextArchiveFixture пишет точные переданные байты в закрытый временный архив,
// хеширует их и создаёт отдельный пустой закрытый destination в прежнем порядке
// двух тестовых setup. Tar не разбирается и не исправляется, source binding
// не выбирается, проверяемый extractor не вызывается.
func contextArchiveFixture(t *testing.T, raw []byte) (archive, destination, digest string) {
	t.Helper()
	archive = filepath.Join(t.TempDir(), "context.tar")
	if err := os.WriteFile(archive, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	destination = filepath.Join(t.TempDir(), "context")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	return archive, destination, hex.EncodeToString(sum[:])
}

// TestContextArchiveFixturePreservesBytesDigestAndPrivatePaths checks exact binary
// bytes, bare SHA-256, file/directory permissions and an empty destination, even
// for deliberately non-tar input; fixture construction must not validate or rewrite it.
// TestContextArchiveFixturePreservesBytesDigestAndPrivatePaths проверяет точные
// бинарные байты, SHA-256 без префикса, права файла/каталога и пустой destination
// даже для намеренно не-tar входа: fixture не должна проверять либо переписывать его.
func TestContextArchiveFixturePreservesBytesDigestAndPrivatePaths(t *testing.T) {
	raw := []byte("not-a-tar\x00\n")
	expected := sha256.Sum256(raw)
	archive, destination, digest := contextArchiveFixture(t, raw)
	actual, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(actual, raw) || digest != hex.EncodeToString(expected[:]) {
		t.Fatal("archive fixture changed bytes or digest")
	}
	file, err := os.Stat(archive)
	if err != nil || !file.Mode().IsRegular() || file.Mode().Perm() != 0o600 {
		t.Fatal("archive fixture is not a private regular file")
	}
	directory, err := os.Stat(destination)
	if err != nil || !directory.IsDir() || directory.Mode().Perm() != 0o700 {
		t.Fatal("archive fixture destination is not private")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatal("archive fixture performed hidden extraction")
	}
}

// tarEntry describes synthetic name/body/link/type bytes for extractor tests,
// including intentionally unsafe entries that the fixture must not sanitize.
// tarEntry задаёт искусственные name/body/link/type для тестов extractor,
// включая намеренно опасные записи, которые fixture не должна исправлять.
type tarEntry struct {
	name, body, link string
	kind             byte
}

// tarBytes serializes fixture headers with private mode, writes bodies only for
// regular entries and closes the writer to include archive padding/end markers.
// Unsafe names and link targets are preserved as data for the extractor to reject.
// tarBytes сериализует fixture headers с закрытым режимом, пишет тела только обычных
// записей и закрывает writer для padding/end markers. Опасные имена и link targets
// сохраняются как данные, которые должен отклонить extractor.
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
