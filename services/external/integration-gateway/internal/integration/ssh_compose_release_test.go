package integration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// TestSSHComposeReleaseRequiresCompletePinnedSource rejects partial trees and immutable-source substitution.
// TestSSHComposeReleaseRequiresCompletePinnedSource отклоняет неполные деревья и подмену immutable source.
func TestSSHComposeReleaseRequiresCompletePinnedSource(t *testing.T) {
	for _, scenario := range []string{"valid", "template-141", "limit-256", "overflow-257", "truncated", "symlink", "omitted", "bytes", "bundle-digest", "mutable-image"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "ssh-source-fixture-token")
			host := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
			host = append(host, 0, 0, 0, 32)
			host = append(host, bytes.Repeat([]byte{1}, 32)...)
			image := "docker.io/library/python@sha256:" + strings.Repeat("a", 64)
			manifest, err := json.Marshal(sshcompose.Manifest{Image: image, Command: []string{"python", "server.py"}, ContainerPort: 8080, HealthPath: "/health"})
			if err != nil {
				t.Fatal(err)
			}
			files := []sshcompose.File{}
			contents := map[string][]byte{}
			for path, raw := range map[string][]byte{"kodex-deploy.json": manifest, "server.py": []byte("print('fixture')\n")} {
				sha := sshcompose.GitObjectID("blob", raw)
				files = append(files, sshcompose.File{Path: path, Mode: "100644", BlobSHA: sha, Content: base64.StdEncoding.EncodeToString(raw)})
				contents[sha] = raw
			}
			count := map[string]int{"template-141": 141, "limit-256": 256, "overflow-257": 257}[scenario]
			for len(files) < count {
				raw := bytes.Repeat([]byte("x"), 3300)
				sha := sshcompose.GitObjectID("blob", raw)
				files = append(files, sshcompose.File{Path: fmt.Sprintf("template/file-%03d.txt", len(files)), Mode: "100644", BlobSHA: sha, Content: base64.StdEncoding.EncodeToString(raw)})
				contents[sha] = raw
			}
			bundle := sshcompose.Bundle{Version: sshcompose.ProtocolVersion, Repository: "example/project", MergedSHA: strings.Repeat("b", 40), TreeSHA: sshcompose.TreeID(files), Files: sshcompose.SortedFiles(files)}
			canonical, _, err := bundle.Canonical()
			if err != nil && scenario != "overflow-257" {
				t.Fatal(err)
			}
			snapshot := sshcompose.Snapshot{Target: sshcompose.Target{Ref: "target_fixture", ProjectRef: "project_fixture", Repository: bundle.Repository, Address: "192.0.2.10", Port: 22, User: "deployer", HostKey: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(host), PublishedPort: 8080, RuntimeImage: image}, MergedSHA: bundle.MergedSHA, TreeSHA: bundle.TreeSHA, BundleSHA256: sshcompose.Digest(canonical), Source: &sshcompose.SourceBinding{CredentialRef: credential.Ref, CredentialRevision: credential.Revision, SecretRef: credential.SecretRef, SecretUID: credential.SecretUID, SecretResourceVersion: credential.SecretResourceVersion, CredentialSHA256: credential.ContentSHA256}}
			if scenario == "bundle-digest" {
				snapshot.BundleSHA256 = strings.Repeat("c", 64)
			}
			if scenario == "mutable-image" {
				snapshot.Target.RuntimeImage = "python:latest"
			}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.Host != "api.github.com" || request.Header.Get("Authorization") != "Bearer ssh-source-fixture-token" {
					t.Fatal("source authority or destination changed")
				}
				var value any
				switch request.URL.Path {
				case "/repos/example/project/git/commits/" + bundle.MergedSHA:
					value = map[string]any{"sha": bundle.MergedSHA, "tree": map[string]string{"sha": bundle.TreeSHA}}
				case "/repos/example/project/git/trees/" + bundle.TreeSHA:
					if request.URL.Query().Get("recursive") != "1" {
						t.Fatal("incomplete tree request")
					}
					entries := []map[string]any{}
					for index, file := range bundle.Files {
						if scenario == "omitted" && index == 1 {
							continue
						}
						mode := file.Mode
						if scenario == "symlink" {
							mode = "120000"
						}
						entries = append(entries, map[string]any{"path": file.Path, "mode": mode, "type": "blob", "sha": file.BlobSHA, "size": len(contents[file.BlobSHA])})
					}
					value = map[string]any{"sha": bundle.TreeSHA, "truncated": scenario == "truncated", "tree": entries}
				default:
					sha := strings.TrimPrefix(request.URL.Path, "/repos/example/project/git/blobs/")
					raw, found := contents[sha]
					if !found {
						t.Fatal("unexpected source request")
					}
					encoded := base64.StdEncoding.EncodeToString(raw)
					if scenario == "bytes" {
						encoded = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'x'}, len(raw)))
					}
					value = map[string]any{"sha": sha, "size": len(raw), "encoding": "base64", "content": encoded}
				}
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
			})}
			adapter.githubHTTPClient = client
			_, err = adapter.ReadSSHComposeRelease(t.Context(), snapshot)
			if (err == nil) != (scenario == "valid" || scenario == "template-141" || scenario == "limit-256") {
				t.Fatalf("release admission mismatch: %v", err)
			}
		})
	}
}
