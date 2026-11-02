package publisher_test

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	publisher "github.com/codex-k8s/kodex/tools/release/oci-publisher"
	"github.com/olareg/olareg"
	registryconfig "github.com/olareg/olareg/config"
)

// registryStats records only protocol effects needed to prove chunk limits and preflight refusal.
// RU: registryStats фиксирует только protocol effects для проверки chunk limits и preflight refusal.
type registryStats struct{ requests, uploads, patches, oversized, manifestWrites atomic.Int64 }

// fixtureRegistry provides a disposable real OCI registry behind authentication and a strict HTTP body limit.
// RU: fixtureRegistry поднимает disposable настоящий OCI registry с authentication и жёстким HTTP body limit.
func fixtureRegistry(t *testing.T, deny int, loseManifestResponse bool) (*httptest.Server, *registryStats) {
	t.Helper()
	stats := &registryStats{}
	backend := olareg.New(registryconfig.Config{Storage: registryconfig.ConfigStorage{StoreType: registryconfig.StoreMem, RootDir: t.TempDir(), GC: registryconfig.ConfigGC{Frequency: -1}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats.requests.Add(1)
		if deny != 0 {
			w.Header().Set("WWW-Authenticate", `Basic realm="synthetic"`)
			w.WriteHeader(deny)
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-writer" || password != "synthetic-password-v1" {
			w.Header().Set("WWW-Authenticate", `Basic realm="synthetic"`)
			w.WriteHeader(401)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
		if err != nil || len(data) > 128<<10 {
			stats.oversized.Add(1)
			w.WriteHeader(413)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/blobs/uploads") {
			stats.uploads.Add(1)
		}
		if r.Method == http.MethodPatch {
			stats.patches.Add(1)
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
			stats.manifestWrites.Add(1)
			if loseManifestResponse && strings.HasSuffix(r.URL.Path, "/manifests/candidate") {
				recorder := httptest.NewRecorder()
				backend.ServeHTTP(recorder, r)
				if recorder.Code == 201 {
					w.WriteHeader(500)
					return
				}
				for key, values := range recorder.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(recorder.Code)
				w.Write(recorder.Body.Bytes())
				return
			}
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { server.Close(); _ = backend.Close() })
	return server, stats
}

// fixtureArchive creates a deterministic valid OCI archive with a layer exceeding the proxy body limit.
// RU: fixtureArchive создаёт deterministic валидный OCI archive со слоем больше proxy body limit.
func fixtureArchive(t *testing.T, marker string) (string, string) {
	t.Helper()
	var layer bytes.Buffer
	writer := tar.NewWriter(&layer)
	payload := bytes.Repeat([]byte(marker), 180000)
	if err := writer.WriteHeader(&tar.Header{Name: "synthetic.txt", Mode: 0o444, Size: int64(len(payload)), ModTime: time.Unix(0, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := func(data []byte) string { sum := sha256.Sum256(data); return "sha256:" + hex.EncodeToString(sum[:]) }
	layerDigest := digest(layer.Bytes())
	config, _ := json.Marshal(map[string]any{"architecture": "amd64", "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": []string{layerDigest}}, "config": map[string]any{}})
	descriptor := func(kind string, data []byte) map[string]any {
		return map[string]any{"mediaType": kind, "digest": digest(data), "size": len(data)}
	}
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": descriptor("application/vnd.oci.image.config.v1+json", config), "layers": []any{descriptor("application/vnd.oci.image.layer.v1.tar", layer.Bytes())}})
	index, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []any{descriptor("application/vnd.oci.image.manifest.v1+json", manifest)}})
	path := filepath.Join(t.TempDir(), "image.tar")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(file)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`)}, {"index.json", index}, {"blobs/sha256/" + strings.TrimPrefix(digest(config), "sha256:"), config}, {"blobs/sha256/" + strings.TrimPrefix(digest(manifest), "sha256:"), manifest}, {"blobs/sha256/" + strings.TrimPrefix(layerDigest, "sha256:"), layer.Bytes()}} {
		if err := archive.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o444, Size: int64(len(entry.data)), ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path, digest(manifest)
}

// fixtureInput binds synthetic credentials and the exact test CA to one disposable registry authority.
// RU: fixtureInput привязывает synthetic credentials и точный test CA к одной disposable registry authority.
func fixtureInput(t *testing.T, server *httptest.Server, marker string) publisher.Input {
	t.Helper()
	archive, digest := fixtureArchive(t, marker)
	host := strings.TrimPrefix(server.URL, "https://")
	directory := t.TempDir()
	auth, _ := json.Marshal(map[string]any{"auths": map[string]any{host: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte("synthetic-writer:synthetic-password-v1"))}}})
	config := filepath.Join(directory, "config.json")
	if err := os.WriteFile(config, auth, 0o600); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(directory, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return publisher.Input{Archive: archive, DockerConfigFile: config, Destination: host + "/synthetic/image:candidate", ExpectedDigest: digest, CAFile: ca, ChunkBytes: 64 << 10}
}

// TestLargeLayerUsesBoundedChunksAndRepeatOnlyReads proves successful upload below the proxy limit and no repeated external writes.
// RU: TestLargeLayerUsesBoundedChunksAndRepeatOnlyReads проверяет upload ниже proxy limit и отсутствие повторных external writes.
func TestLargeLayerUsesBoundedChunksAndRepeatOnlyReads(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic-layer")
	result, err := publisher.Publish(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != input.ExpectedDigest || result.Reused || stats.oversized.Load() != 0 || stats.patches.Load() < 2 {
		t.Fatal("bounded chunk publication contract failed")
	}
	uploads := stats.uploads.Load()
	writes := stats.manifestWrites.Load()
	result, err = publisher.Publish(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reused || stats.uploads.Load() != uploads || stats.manifestWrites.Load() != writes {
		t.Fatal("repeat created another external write")
	}
}

// TestExistingDifferentTagIsNotReplaced checks the immutable digest conflict before any new upload session.
// RU: TestExistingDifferentTagIsNotReplaced проверяет digest conflict до нового upload session.
func TestExistingDifferentTagIsNotReplaced(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "first")
	if _, err := publisher.Publish(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.Archive, input.ExpectedDigest = fixtureArchive(t, "second")
	uploads := stats.uploads.Load()
	writes := stats.manifestWrites.Load()
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrConflict) || stats.uploads.Load() != uploads || stats.manifestWrites.Load() != writes {
		t.Fatal("existing destination was not protected")
	}
}

// TestWrongMetadataDigestFailsBeforeNetwork prevents a caller from publishing an archive under an unrelated metadata identity.
// RU: TestWrongMetadataDigestFailsBeforeNetwork запрещает публикацию archive с чужой metadata identity до сети.
func TestWrongMetadataDigestFailsBeforeNetwork(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic")
	input.ExpectedDigest = "sha256:" + strings.Repeat("0", 64)
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrInvalid) || stats.requests.Load() != 0 {
		t.Fatal("unbound archive reached the registry")
	}
}

// TestRejectedCredentialCannotStartUpload checks final authentication refusal without account fallback.
// RU: TestRejectedCredentialCannotStartUpload проверяет authentication refusal без account fallback и upload.
func TestRejectedCredentialCannotStartUpload(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 401, false)
	input := fixtureInput(t, server, "synthetic")
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrUnauthorized) || stats.uploads.Load() != 0 {
		t.Fatal("rejected credential started an upload")
	}
}

// TestForbiddenRegistryCannotStartUpload closes insufficient permissions before publication.
// RU: TestForbiddenRegistryCannotStartUpload закрывает недостаточные права до публикации.
func TestForbiddenRegistryCannotStartUpload(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 403, false)
	input := fixtureInput(t, server, "synthetic")
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrUnauthorized) || stats.uploads.Load() != 0 {
		t.Fatal("forbidden destination started an upload")
	}
}

// TestLostManifestResponseReconcilesExactDigest confirms the committed result rather than starting a second whole import.
// RU: TestLostManifestResponseReconcilesExactDigest подтверждает committed digest вместо повторного полного import.
func TestLostManifestResponseReconcilesExactDigest(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, true)
	input := fixtureInput(t, server, "synthetic")
	result, err := publisher.Publish(t.Context(), input)
	if err != nil || result.Digest != input.ExpectedDigest || !result.Reconciled || stats.oversized.Load() != 0 {
		t.Fatal("committed manifest was not reconciled")
	}
}

// TestUntrustedCAIsRejectedBeforeAuthenticatedHTTP preserves HTTPS verification in the optional transport.
// RU: TestUntrustedCAIsRejectedBeforeAuthenticatedHTTP сохраняет HTTPS verification в optional transport.
func TestUntrustedCAIsRejectedBeforeAuthenticatedHTTP(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic")
	input.CAFile = ""
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrUnavailable) || stats.requests.Load() != 0 {
		t.Fatal("untrusted TLS connection reached authenticated HTTP")
	}
}

// TestCorruptedBlobFailsBeforeNetwork verifies blob content identity before any registry operation.
// RU: TestCorruptedBlobFailsBeforeNetwork проверяет identity blob bytes до registry operation.
func TestCorruptedBlobFailsBeforeNetwork(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "byte-fixture")
	raw, err := os.ReadFile(input.Archive)
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.Index(raw, []byte("byte-fixturebyte-fixture"))
	if index < 0 {
		t.Fatal("fixture payload absent")
	}
	raw[index] ^= 1
	if err := os.WriteFile(input.Archive, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrInvalid) || stats.requests.Load() != 0 {
		t.Fatal("corrupted blob reached registry")
	}
}

// TestCredentialSymlinkIsRejected prevents a file reference from crossing the selected secret boundary.
// RU: TestCredentialSymlinkIsRejected запрещает symlink за границу выбранного secret file.
func TestCredentialSymlinkIsRejected(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic")
	alias := filepath.Join(t.TempDir(), "auth-alias.json")
	if err := os.Symlink(input.DockerConfigFile, alias); err != nil {
		t.Fatal(err)
	}
	input.DockerConfigFile = alias
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrInvalid) || stats.requests.Load() != 0 {
		t.Fatal("credential symlink reached registry")
	}
}

// TestForeignRedirectCannotReceiveAnyRequest enforces the exact HTTPS authority even when both certificates are trusted.
// RU: TestForeignRedirectCannotReceiveAnyRequest сохраняет exact authority даже при доверии обоим сертификатам.
func TestForeignRedirectCannotReceiveAnyRequest(t *testing.T) {
	t.Parallel()
	var foreignRequests atomic.Int64
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignRequests.Add(1); w.WriteHeader(200) }))
	defer foreign.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", foreign.URL+r.URL.Path)
		w.WriteHeader(307)
	}))
	defer source.Close()
	input := fixtureInput(t, source, "synthetic")
	ca, err := os.OpenFile(input.CAFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: foreign.Certificate().Raw})); err != nil {
		t.Fatal(err)
	}
	ca.Close()
	_, err = publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrUnavailable) || foreignRequests.Load() != 0 {
		t.Fatal("redirect crossed the registry authority")
	}
}

// TestArchiveBudgetRejectsSparseOversizedInput checks the size bound without reading or allocating the sparse payload.
// RU: TestArchiveBudgetRejectsSparseOversizedInput проверяет лимит без чтения и выделения sparse payload.
func TestArchiveBudgetRejectsSparseOversizedInput(t *testing.T) {
	t.Parallel()
	server, stats := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic")
	if err := os.Truncate(input.Archive, publisher.MaxArchiveBytes+1); err != nil {
		t.Fatal(err)
	}
	_, err := publisher.Publish(t.Context(), input)
	if !errors.Is(err, publisher.ErrInvalid) || stats.requests.Load() != 0 {
		t.Fatal("oversized archive reached registry")
	}
}

// TestNestedIndexPreservesAttestationBytes verifies that publishing retains the complete OCI graph and immutable root identity.
// RU: TestNestedIndexPreservesAttestationBytes проверяет сохранение полного OCI graph и immutable root identity.
func TestNestedIndexPreservesAttestationBytes(t *testing.T) {
	t.Parallel()
	server, _ := fixtureRegistry(t, 0, false)
	input := fixtureInput(t, server, "synthetic-index")
	original, err := os.Open(input.Archive)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	reader := tar.NewReader(original)
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[header.Name] = data
	}
	original.Close()
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return "sha256:" + hex.EncodeToString(sum[:]) }
	descriptor := func(media string, raw []byte) map[string]any {
		return map[string]any{"mediaType": media, "digest": digest(raw), "size": len(raw)}
	}
	config := []byte(`{}`)
	statement := []byte(`{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://example.invalid/synthetic/provenance","predicate":{"synthetic":true}}`)
	attestation, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": descriptor("application/vnd.oci.image.config.v1+json", config), "layers": []any{descriptor("application/vnd.in-toto+json", statement)}})
	if err != nil {
		t.Fatal(err)
	}
	image := entries["blobs/sha256/"+strings.TrimPrefix(input.ExpectedDigest, "sha256:")]
	attestationDescriptor := descriptor("application/vnd.oci.image.manifest.v1+json", attestation)
	attestationDescriptor["platform"] = map[string]string{"architecture": "unknown", "os": "unknown"}
	attestationDescriptor["annotations"] = map[string]string{"vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": input.ExpectedDigest}
	nested, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []any{descriptor("application/vnd.oci.image.manifest.v1+json", image), attestationDescriptor}})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := json.Marshal(map[string]any{"schemaVersion": 2, "manifests": []any{descriptor("application/vnd.oci.image.index.v1+json", nested)}})
	if err != nil {
		t.Fatal(err)
	}
	entries["index.json"] = layout
	for _, raw := range [][]byte{config, statement, attestation, nested} {
		entries["blobs/sha256/"+strings.TrimPrefix(digest(raw), "sha256:")] = raw
	}
	replacement, err := os.OpenFile(input.Archive, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(replacement)
	for name, raw := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o444, Size: int64(len(raw)), ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	input.ExpectedDigest = digest(nested)
	result, err := publisher.Publish(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != input.ExpectedDigest {
		t.Fatal("nested root identity changed")
	}
	for _, check := range []struct {
		path string
		raw  []byte
	}{{"manifests/candidate", nested}, {"manifests/" + digest(attestation), attestation}, {"blobs/" + digest(statement), statement}} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v2/synthetic/image/"+check.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.SetBasicAuth("synthetic-writer", "synthetic-password-v1")
		request.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Equal(raw, check.raw) {
			t.Fatal("OCI graph bytes were not preserved")
		}
	}
}
