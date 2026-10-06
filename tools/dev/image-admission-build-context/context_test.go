package buildcontext

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// Используется Docker/Moby matcher, а не приближённая Git/glob-семантика.
func TestAdmissionDockerContextExcludesPrivateInputs(t *testing.T) {
	f, err := os.Open("../Dockerfile.local-image-supply-chain.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rules, err := ignorefile.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	matcher, err := patternmatcher.New(rules)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	allowed := []string{"libs/go/fixture.go", "services/jobs/role-image-builder/fixture.go", "tools/render-image-admission-job.sh"}
	private := []string{".env", ".env.local", ".kodex-env", "unrelated.go", "libs/go/.env", "libs/go/private.key", "libs/go/private.pem", "libs/go/secrets/value", "libs/go/credentials.json", "services/jobs/role-image-builder/.env", "services/jobs/role-image-builder/.env.local", "services/jobs/role-image-builder/kubeconfig", "services/staff/control-center/.env"}
	for _, path := range append(append([]string{}, allowed...), private...) {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var payload bytes.Buffer
	writer := tar.NewWriter(&payload)
	reads := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		excluded, err := matcher.MatchesOrParentMatches(relative)
		if err != nil || excluded {
			return err
		}
		reads[relative] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writer.WriteHeader(&tar.Header{Name: relative, Mode: 0o600, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(bytes.NewReader(payload.Bytes()))
	transmitted := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		transmitted[header.Name] = true
	}
	for _, path := range allowed {
		if !transmitted[path] {
			t.Fatalf("required COPY input missing: %s", path)
		}
	}
	for _, path := range private {
		if reads[path] || transmitted[path] {
			t.Fatal("private fixture read or transferred")
		}
	}
	if len(transmitted) != len(allowed) {
		t.Fatal("context contains unexpected inputs")
	}
}
